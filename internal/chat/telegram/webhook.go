package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"

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
	providers        market.ProviderResolver
}

// RegisterCommands publishes the bot command menu through Telegram.
func (a *Adapter) RegisterCommands(ctx context.Context) error {
	return a.client.RegisterDefaultCommands(ctx)
}

// RegisterWebhook configures Telegram to deliver updates to the public application URL.
func (a *Adapter) RegisterWebhook(ctx context.Context, webhookURL string) error {
	return a.client.SetWebhook(ctx, webhookURL, a.webhookSecret)
}

func NewAdapter(client *APIClient, targets TargetRepository, events EventRepository, commands CommandService, sessions chat.SessionStore, webhookSecret, tenantID string, symbols []string, intervals []domain.Interval, providers market.ProviderResolver) (*Adapter, error) {
	if client == nil || targets == nil || events == nil || commands == nil || sessions == nil || providers == nil || strings.TrimSpace(webhookSecret) == "" || strings.TrimSpace(tenantID) == "" {
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
		providers:        providers,
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
	if update.Message != nil && !strings.HasPrefix(update.Message.Text, "/") {
		return c.JSON(http.StatusOK, map[string]string{
			"result": "message ignored",
		})
	}
	externalID := fmt.Sprintf("%d", update.UpdateID)
	event := domain.InboundEvent{
		ID:              uuid.NewString(),
		Provider:        domain.ChatProviderTelegram,
		ExternalEventID: externalID,
		Message:         updateMessage(update),
		ReceivedAt:      time.Now().UTC(),
		Status:          "received",
	}
	claimed, err := a.events.ClaimInboundEvent(c.Request().Context(), event)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}
	if !claimed {
		return c.JSON(http.StatusOK, map[string]string{
			"result": "event recorded",
		})
	}
	if err := a.process(c, update); err != nil {
		_ = a.events.MarkInboundEventFailed(c.Request().Context(), domain.ChatProviderTelegram, externalID, err.Error())
		return c.JSON(http.StatusOK, map[string]string{
			"result": "event failed",
		})
	}

	_ = a.events.MarkInboundEventProcessed(c.Request().Context(), domain.ChatProviderTelegram, externalID, time.Now().UTC())

	return c.JSON(http.StatusOK, map[string]string{
		"result": "event processed",
	})
}

func updateMessage(update Update) string {
	if update.Message != nil {
		return update.Message.Text
	}
	if update.CallbackQuery != nil {
		return update.CallbackQuery.Data
	}
	return ""
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
		text := response
		if action == chat.ActionConfigure {
			session, err := a.sessions.Get(ctx, response)
			if err != nil {
				return err
			}

			if provider, onlyOne := a.singleProvider(); onlyOne {
				session.SelectedProvider = provider
				if err := a.sessions.Update(ctx, session); err != nil {
					return err
				}
				text = symbolSelectionText(session)
				keyboard = a.symbolKeyboard(session)
			} else {
				text = providerSelectionText(session)
				keyboard = a.providerKeyboard(session)
			}
		}
		return a.client.SendMessage(ctx, update.Message.Chat.ID, text, keyboard)
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
	if err != nil || session.TargetID != target.ID || session.ActorUserID != userIDString(callback.From.ID) {
		return fmt.Errorf("invalid configuration session")
	}

	var resp string = ""
	switch {
	case strings.HasPrefix(action, "provider:"):
		provider := domain.MarketProvider(strings.TrimPrefix(action, "provider:"))
		if !a.providerEnabled(provider) {
			return fmt.Errorf("market provider %q is not enabled", provider)
		}
		session.SelectedProvider = provider
		session.SelectedSymbols = a.supportedSymbols(session)
		session.SelectedIntervals = a.supportedIntervals(session)
		if err := a.commands.UpdateSession(ctx, command, session.SelectedProvider, session.SelectedSymbols, session.SelectedIntervals); err != nil {
			return err
		}
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, providerSelectionText(session), a.providerKeyboard(session)); err != nil {
			return err
		}

	case strings.HasPrefix(action, "symbol:"):
		session.SelectedSymbols = toggleString(session.SelectedSymbols, strings.TrimPrefix(action, "symbol:"))
		if err := a.commands.UpdateSession(ctx, command, session.SelectedProvider, session.SelectedSymbols, session.SelectedIntervals); err != nil {
			return err
		}
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, symbolSelectionText(session), a.symbolKeyboard(session)); err != nil {
			return err
		}

	case strings.HasPrefix(action, "interval:"):
		value := domain.Interval(strings.TrimPrefix(action, "interval:"))
		session.SelectedIntervals = toggleInterval(session.SelectedIntervals, value)
		if err := a.commands.UpdateSession(ctx, command, session.SelectedProvider, session.SelectedSymbols, session.SelectedIntervals); err != nil {
			return err
		}
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, intervalSelectionText(session), a.intervalKeyboard(session)); err != nil {
			return err
		}

	case action == "next:intervals":
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, intervalSelectionText(session), a.intervalKeyboard(session)); err != nil {
			return err
		}

	case action == "next:symbols":
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, symbolSelectionText(session), a.symbolKeyboard(session)); err != nil {
			return err
		}

	case action == "back:providers":
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, providerSelectionText(session), a.providerKeyboard(session)); err != nil {
			return err
		}

	case action == "back:symbols":
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, symbolSelectionText(session), a.symbolKeyboard(session)); err != nil {
			return err
		}

	case action == "save":
		if resp, err = a.commands.Handle(ctx, command); err != nil {
			return err
		}
		finalText := fmt.Sprintf("%s\nSymbols: %s\nIntervals: %s", resp, joinSelectedStrings(session.SelectedSymbols), joinSelectedIntervals(session.SelectedIntervals))
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, finalText, emptyInlineKeyboard()); err != nil {
			return err
		}

	case action == "cancel":
		command.Action = chat.ActionCancel
		if resp, err = a.commands.Handle(ctx, command); err != nil {
			return err
		}
		if err := a.client.EditMessageText(ctx, callback.Message.Chat.ID, callback.Message.MessageID, resp, emptyInlineKeyboard()); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsupported callback action")
	}

	return a.client.AnswerCallbackQuery(ctx, callback.ID, resp)
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

func (a *Adapter) symbolKeyboard(session chat.ConfigSession) *InlineKeyboardMarkup {
	keyboard := &InlineKeyboardMarkup{}

	for _, symbol := range a.availableSymbols(session.SelectedProvider) {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []InlineKeyboardButton{{
			Text:         selectedText(slices.Contains(session.SelectedSymbols, symbol), symbol),
			CallbackData: callbackData(session.SessionID, "symbol:"+symbol),
		}})
	}
	controls := []InlineKeyboardButton{}
	if _, onlyOne := a.singleProvider(); !onlyOne {
		controls = append(controls, InlineKeyboardButton{
			Text:         "Back",
			CallbackData: callbackData(session.SessionID, "back:providers"),
		})
	}
	controls = append(controls,
		InlineKeyboardButton{
			Text:         "Next",
			CallbackData: callbackData(session.SessionID, "next:intervals"),
		},
		InlineKeyboardButton{
			Text:         "Cancel",
			CallbackData: callbackData(session.SessionID, "cancel"),
		})

	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, controls)
	return keyboard
}

func (a *Adapter) intervalKeyboard(session chat.ConfigSession) *InlineKeyboardMarkup {
	keyboard := &InlineKeyboardMarkup{}

	for _, interval := range a.availableIntervals(session.SelectedProvider) {
		value := string(interval)
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []InlineKeyboardButton{{
			Text:         selectedText(slices.Contains(session.SelectedIntervals, interval), value),
			CallbackData: callbackData(session.SessionID, "interval:"+value),
		}})
	}
	keyboard.InlineKeyboard = append(
		keyboard.InlineKeyboard,
		[]InlineKeyboardButton{
			{Text: "Back", CallbackData: callbackData(session.SessionID, "back:symbols")},
			{Text: "Save", CallbackData: callbackData(session.SessionID, "save")},
			{Text: "Cancel", CallbackData: callbackData(session.SessionID, "cancel")},
		},
	)
	return keyboard
}

func (a *Adapter) providerKeyboard(session chat.ConfigSession) *InlineKeyboardMarkup {
	keyboard := &InlineKeyboardMarkup{}
	for _, provider := range a.providers.Enabled() {
		value := string(provider)
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []InlineKeyboardButton{{
			Text:         selectedText(session.SelectedProvider == provider, value),
			CallbackData: callbackData(session.SessionID, "provider:"+value),
		}})
	}
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []InlineKeyboardButton{
		{Text: "Next", CallbackData: callbackData(session.SessionID, "next:symbols")},
		{Text: "Cancel", CallbackData: callbackData(session.SessionID, "cancel")}})
	return keyboard
}

func (a *Adapter) providerEnabled(provider domain.MarketProvider) bool {
	return slices.Contains(a.providers.Enabled(), provider)
}

func (a *Adapter) singleProvider() (domain.MarketProvider, bool) {
	providers := a.providers.Enabled()
	if len(providers) != 1 {
		return "", false
	}
	return providers[0], true
}

func (a *Adapter) availableSymbols(provider domain.MarketProvider) []string {
	result := make([]string, 0, len(a.allowedSymbols))
	for _, symbol := range a.allowedSymbols {
		for _, interval := range a.allowedIntervals {
			if a.providers.Supports(provider, symbol, interval) {
				result = append(result, symbol)
				break
			}
		}
	}
	return result
}

func (a *Adapter) availableIntervals(provider domain.MarketProvider) []domain.Interval {
	result := make([]domain.Interval, 0, len(a.allowedIntervals))
	for _, interval := range a.allowedIntervals {
		for _, symbol := range a.allowedSymbols {
			if a.providers.Supports(provider, symbol, interval) {
				result = append(result, interval)
				break
			}
		}
	}
	return result
}

func (a *Adapter) supportedSymbols(session chat.ConfigSession) []string {
	return filterStrings(session.SelectedSymbols, a.availableSymbols(session.SelectedProvider))
}

func (a *Adapter) supportedIntervals(session chat.ConfigSession) []domain.Interval {
	return filterIntervals(session.SelectedIntervals, a.availableIntervals(session.SelectedProvider))
}

func filterStrings(values, allowed []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if slices.Contains(allowed, value) {
			result = append(result, value)
		}
	}
	return result
}

func filterIntervals(values, allowed []domain.Interval) []domain.Interval {
	result := make([]domain.Interval, 0, len(values))
	for _, value := range values {
		if slices.Contains(allowed, value) {
			result = append(result, value)
		}
	}
	return result
}

// emptyInlineKeyboard clears the inline keyboard on an edited message.
// Telegram removes buttons when editMessageText carries an empty inline_keyboard.
func emptyInlineKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
}

func symbolSelectionText(session chat.ConfigSession) string {
	return fmt.Sprintf("Select symbols for alerts.\nSelected: %s", joinSelectedStrings(session.SelectedSymbols))
}

func providerSelectionText(session chat.ConfigSession) string {
	return fmt.Sprintf("Select a market provider.\nSelected: %s", session.SelectedProvider)
}

func intervalSelectionText(session chat.ConfigSession) string {
	return fmt.Sprintf("Select intervals for alerts.\nSelected: %s", joinSelectedIntervals(session.SelectedIntervals))
}

func selectedText(selected bool, text string) string {
	if selected {
		return "✓ " + text
	}
	return text
}

func joinSelectedStrings(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func joinSelectedIntervals(values []domain.Interval) string {
	if len(values) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, ", ")
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
