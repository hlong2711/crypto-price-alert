package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/domain"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type Adapter struct {
	client           *APIClient
	targets          TargetRepository
	events           EventRepository
	commands         CommandService
	sessions         chat.SessionStore
	webhookSecret    string
	tenantID         string
	allowedSymbols   []string
	allowedIntervals []domain.Interval
}

// RegisterCommands publishes the bot command menu through Telegram.
func (a *Adapter) RegisterCommands(ctx context.Context) error {
	return a.client.RegisterDefaultCommands(ctx)
}

// RegisterWebhook configures Telegram to deliver updates to the public application URL.
func (a *Adapter) RegisterWebhook(ctx context.Context, webhookURL string) error {
	return a.client.SetWebhook(ctx, webhookURL, a.webhookSecret)
}

func NewAdapter(client *APIClient, targets TargetRepository, events EventRepository, commands CommandService, sessions chat.SessionStore, webhookSecret, tenantID string, symbols []string, intervals []domain.Interval) (*Adapter, error) {
	if client == nil || targets == nil || events == nil || commands == nil || sessions == nil || strings.TrimSpace(webhookSecret) == "" || strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("invalid Telegram adapter settings")
	}
	return &Adapter{
		client:           client,
		targets:          targets,
		events:           events,
		commands:         commands,
		sessions:         sessions,
		webhookSecret:    webhookSecret,
		tenantID:         tenantID,
		allowedSymbols:   append([]string(nil), symbols...),
		allowedIntervals: append([]domain.Interval(nil), intervals...),
	}, nil
}

// Webhook handles authenticated Telegram updates and acknowledges duplicates.
func (a *Adapter) Webhook(c echo.Context) error {
	if c.Request().Header.Get("X-Telegram-Bot-Api-Secret-Token") != a.webhookSecret {
		return c.NoContent(http.StatusUnauthorized)
	}
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	var update Update
	if err := json.Unmarshal(body, &update); err != nil || update.UpdateID == 0 {
		return c.NoContent(http.StatusBadRequest)
	}
	externalID := fmt.Sprintf("%d", update.UpdateID)
	event := domain.InboundEvent{
		ID:              uuid.NewString(),
		Provider:        domain.ChatProviderTelegram,
		ExternalEventID: externalID,
		ReceivedAt:      time.Now().UTC(),
		Status:          "received",
	}
	claimed, err := a.events.ClaimInboundEvent(c.Request().Context(), event)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	if !claimed {
		return c.NoContent(http.StatusOK)
	}
	if err := a.process(c, update); err != nil {
		_ = a.events.MarkInboundEventFailed(c.Request().Context(), domain.ChatProviderTelegram, externalID, err.Error())
		return c.NoContent(http.StatusOK)
	}

	_ = a.events.MarkInboundEventProcessed(c.Request().Context(), domain.ChatProviderTelegram, externalID, time.Now().UTC())

	return c.NoContent(http.StatusOK)
}

func (a *Adapter) process(c echo.Context, update Update) error {
	ctx := c.Request().Context()
	if update.Message != nil {
		if update.Message.From == nil || strings.TrimSpace(update.Message.Text) == "" {
			return nil
		}
		target, err := a.targetForChat(ctx, update.Message.Chat)
		if err != nil {
			return err
		}

		action, args, err := chat.ParseCommand(update.Message.Text)
		if err != nil {
			return a.client.SendMessage(ctx, update.Message.Chat.ID, "Unknown command. Use /help.", nil)
		}

		response, err := a.commands.Handle(ctx,
			chat.Command{
				Target:      target,
				ActorUserID: userIDString(update.Message.From.ID),
				Action:      action,
				Arguments:   args,
				EventID:     fmt.Sprintf("%d", update.UpdateID),
			})
		if err != nil {
			c.Logger().Errorf("Processing error %v", err)
			return a.client.SendMessage(ctx, update.Message.Chat.ID, safeError(err), nil)
		}

		var keyboard *InlineKeyboardMarkup
		if action == chat.ActionConfigure {
			keyboard = a.configurationKeyboard(response)
		}
		return a.client.SendMessage(ctx, update.Message.Chat.ID, response, keyboard)
	}

	if update.CallbackQuery != nil {
		return a.processCallback(ctx, update.CallbackQuery)
	}
	return nil
}

func (a *Adapter) processCallback(ctx context.Context, callback *CallbackQuery) error {
	if callback.Message == nil {
		return fmt.Errorf("callback has no message")
	}

	target, err := a.targets.GetTarget(ctx, domain.ChatProviderTelegram, a.tenantID, chatIDString(callback.Message.Chat.ID))
	if err != nil {
		return err
	}

	admins, err := a.client.GetChatAdministrators(ctx, callback.Message.Chat.ID)
	if err != nil {
		return err
	}

	creator := ""
	for _, admin := range admins {
		if admin.Status == "creator" {
			creator = userIDString(admin.User.ID)
			break
		}
	}

	if creator == "" || creator != userIDString(callback.From.ID) {
		return fmt.Errorf("only the chat creator can use this configuration control")
	}
	sessionID, action, err := parseCallback(callback.Data)
	if err != nil {
		return err
	}

	command := chat.Command{
		Target:      target,
		ActorUserID: userIDString(callback.From.ID),
		Action:      chat.ActionSave,
		Arguments:   []string{sessionID},
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil || session.TargetID != target.ID {
		return fmt.Errorf("invalid configuration session")
	}
	switch {
	case strings.HasPrefix(action, "symbol:"):
		session.SelectedSymbols = toggleString(session.SelectedSymbols, strings.TrimPrefix(action, "symbol:"))
		if err := a.commands.UpdateSession(ctx, command, session.SelectedSymbols, session.SelectedIntervals); err != nil {
			return err
		}

	case strings.HasPrefix(action, "interval:"):
		value := domain.Interval(strings.TrimPrefix(action, "interval:"))
		session.SelectedIntervals = toggleInterval(session.SelectedIntervals, value)
		if err := a.commands.UpdateSession(ctx, command, session.SelectedSymbols, session.SelectedIntervals); err != nil {
			return err
		}

	case action == "save":
		if _, err := a.commands.Handle(ctx, command); err != nil {
			return err
		}

	case action == "cancel":
		command.Action = chat.ActionCancel
		if _, err := a.commands.Handle(ctx, command); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsupported callback action")
	}
	return a.client.AnswerCallbackQuery(ctx, callback.ID)
}

func (a *Adapter) targetForChat(ctx context.Context, telegramChat Chat) (domain.AlertTarget, error) {
	chatID := chatIDString(telegramChat.ID)
	admins, err := a.client.GetChatAdministrators(ctx, telegramChat.ID)
	if err != nil {
		return domain.AlertTarget{}, err
	}

	var creator *ChatMember
	for i := range admins {
		if admins[i].Status == "creator" {
			creator = &admins[i]
			break
		}
	}
	if creator == nil {
		return domain.AlertTarget{}, fmt.Errorf("chat creator not found")
	}

	return a.targets.FindOrCreateTarget(ctx, domain.AlertTarget{
		ID:             uuid.NewString(),
		Provider:       domain.ChatProviderTelegram,
		TenantID:       a.tenantID,
		ExternalChatID: chatID,
		DisplayName:    telegramChat.Title,
		CreatorUserID:  userIDString(creator.User.ID),
		Enabled:        true,
	})
}

func (a *Adapter) configurationKeyboard(sessionID string) *InlineKeyboardMarkup {
	keyboard := &InlineKeyboardMarkup{}

	for _, symbol := range a.allowedSymbols {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []InlineKeyboardButton{{
			Text:         symbol,
			CallbackData: callbackData(sessionID, "symbol:"+symbol),
		}})
	}
	for _, interval := range a.allowedIntervals {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []InlineKeyboardButton{{
			Text:         string(interval),
			CallbackData: callbackData(sessionID, "interval:"+string(interval)),
		}})
	}
	keyboard.InlineKeyboard = append(
		keyboard.InlineKeyboard,
		[]InlineKeyboardButton{
			{Text: "Save", CallbackData: callbackData(sessionID, "save")},
			{Text: "Cancel", CallbackData: callbackData(sessionID, "cancel")}},
	)
	return keyboard
}

func toggleString(values []string, value string) []string {
	for i, current := range values {
		if current == value {
			return append(values[:i], values[i+1:]...)
		}
	}
	return append(values, value)
}

func toggleInterval(values []domain.Interval, value domain.Interval) []domain.Interval {
	for i, current := range values {
		if current == value {
			return append(values[:i], values[i+1:]...)
		}
	}
	return append(values, value)
}

func safeError(error) string { return "Unable to process the request." }
