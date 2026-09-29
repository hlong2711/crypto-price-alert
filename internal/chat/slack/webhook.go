package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/domain"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// TargetRepository contains the target lookups needed by Slack authorization.
type TargetRepository interface {
	FindOrCreateTarget(context.Context, domain.AlertTarget) (domain.AlertTarget, error)
	GetTarget(context.Context, domain.ChatProvider, string, string) (domain.AlertTarget, error)
}

// EventRepository provides idempotent inbound event handling.
type EventRepository interface {
	ClaimInboundEvent(context.Context, domain.InboundEvent) (bool, error)
	MarkInboundEventProcessed(context.Context, domain.ChatProvider, string, time.Time) error
	MarkInboundEventFailed(context.Context, domain.ChatProvider, string, string) error
}

// CommandService is the platform-independent command surface used by Slack.
type CommandService interface {
	Handle(context.Context, chat.Command) (string, error)
	UpdateSession(context.Context, chat.Command, []string, []domain.Interval) error
}

// Adapter authenticates Slack requests and translates them to generic chat commands.
type Adapter struct {
	client         *APIClient
	targets        TargetRepository
	events         EventRepository
	commands       CommandService
	sessions       chat.SessionStore
	signingSecret  string
	tenantID       string
	expectedAppID  string
	expectedTeamID string
	symbols        []string
	intervals      []domain.Interval
	replayWindow   time.Duration
	now            func() time.Time
}

// NewAdapter creates a Slack chat adapter with request replay protection.
func NewAdapter(client *APIClient, targets TargetRepository, events EventRepository, commands CommandService, sessions chat.SessionStore, signingSecret, tenantID, expectedAppID, expectedTeamID string, symbols []string, intervals []domain.Interval) (*Adapter, error) {
	if client == nil || targets == nil || events == nil || commands == nil || sessions == nil || strings.TrimSpace(signingSecret) == "" || strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("invalid Slack adapter settings")
	}
	return &Adapter{
		client:         client,
		targets:        targets,
		events:         events,
		commands:       commands,
		sessions:       sessions,
		signingSecret:  signingSecret,
		tenantID:       tenantID,
		expectedAppID:  expectedAppID,
		expectedTeamID: expectedTeamID,
		symbols:        append([]string(nil), symbols...),
		intervals:      append([]domain.Interval(nil), intervals...),
		replayWindow:   5 * time.Minute,
		now:            time.Now,
	}, nil
}

// SlashCommandWebhook acknowledges valid slash commands immediately and processes them asynchronously.
func (a *Adapter) SlashCommandWebhook(c echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil || VerifySignature(a.signingSecret, c.Request().Header.Get("X-Slack-Request-Timestamp"), c.Request().Header.Get("X-Slack-Signature"), body, a.now(), a.replayWindow) != nil {
		return c.NoContent(http.StatusUnauthorized)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	payload := SlashCommand{
		Token:       values.Get("token"),
		TeamID:      values.Get("team_id"),
		APIAppID:    values.Get("api_app_id"),
		ChannelID:   values.Get("channel_id"),
		ChannelName: values.Get("channel_name"),
		UserID:      values.Get("user_id"),
		UserName:    values.Get("user_name"),
		Command:     values.Get("command"),
		Text:        values.Get("text"),
		ResponseURL: values.Get("response_url"),
		TriggerID:   values.Get("trigger_id"),
	}
	if err := a.validateContext(payload.APIAppID, payload.TeamID); err != nil || payload.ChannelID == "" || payload.UserID == "" {
		return c.NoContent(http.StatusForbidden)
	}
	if payload.TriggerID == "" {
		payload.TriggerID = uuid.NewString()
	}
	if err := a.claimAndProcess(c.Request().Context(), payload.TriggerID, slashMessage(payload), func(ctx context.Context) error { return a.processSlash(ctx, payload) }); err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, Response{})
}

// InteractionWebhook acknowledges valid Block Kit interactions immediately and processes them asynchronously.
func (a *Adapter) InteractionWebhook(c echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil || VerifySignature(a.signingSecret, c.Request().Header.Get("X-Slack-Request-Timestamp"), c.Request().Header.Get("X-Slack-Signature"), body, a.now(), a.replayWindow) != nil {
		return c.NoContent(http.StatusUnauthorized)
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	var payload Interaction
	if err := json.Unmarshal([]byte(values.Get("payload")), &payload); err != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	if err := a.validateContext(payload.APIAppID, payload.Team.ID); err != nil || payload.Channel.ID == "" || payload.User.ID == "" || payload.ResponseURL == "" {
		return c.NoContent(http.StatusForbidden)
	}
	eventID := payload.TriggerID
	if eventID == "" {
		eventID = uuid.NewString()
	}
	if err := a.claimAndProcess(c.Request().Context(), eventID, interactionMessage(payload), func(ctx context.Context) error { return a.processInteraction(ctx, payload) }); err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, Response{})
}

func (a *Adapter) claimAndProcess(ctx context.Context, externalID, message string, process func(context.Context) error) error {
	event := domain.InboundEvent{
		ID:              uuid.NewString(),
		Provider:        domain.ChatProviderSlack,
		ExternalEventID: externalID,
		Message:         message,
		ReceivedAt:      a.now().UTC(),
		Status:          "received",
	}
	claimed, err := a.events.ClaimInboundEvent(ctx, event)
	if err != nil || !claimed {
		return err
	}
	go func() {
		background := context.Background()
		if err := process(background); err != nil {
			_ = a.events.MarkInboundEventFailed(background, domain.ChatProviderSlack, externalID, err.Error())
			return
		}
		_ = a.events.MarkInboundEventProcessed(background, domain.ChatProviderSlack, externalID, a.now().UTC())
	}()
	return nil
}

func slashMessage(payload SlashCommand) string {
	message := strings.TrimSpace(payload.Command)
	if payload.Text != "" {
		message += " " + strings.TrimSpace(payload.Text)
	}
	return strings.TrimSpace(message)
}

func interactionMessage(payload Interaction) string {
	if len(payload.Actions) == 0 {
		return ""
	}
	return payload.Actions[0].Value
}

func (a *Adapter) processSlash(ctx context.Context, payload SlashCommand) error {
	target, err := a.targetForChannel(ctx, payload.ChannelID, payload.ChannelName, payload.TeamID)
	if err != nil {
		return a.respondError(ctx, payload.ResponseURL, err)
	}
	action, args, err := chat.ParseCommand(payload.Text)
	if err != nil {
		return a.client.Respond(ctx, payload.ResponseURL, Response{ResponseType: "ephemeral", Text: "Unknown command. Use /crypto-alert help."})
	}

	response, err := a.commands.Handle(ctx, chat.Command{
		Target:      target,
		ActorUserID: payload.UserID,
		Action:      action,
		Arguments:   args,
		EventID:     payload.TriggerID,
	})
	if err != nil {
		return a.respondError(ctx, payload.ResponseURL, err)
	}
	result := Response{ResponseType: "ephemeral", Text: response}
	if action == chat.ActionHelp || action == chat.ActionConfigure {
		result.Text = response
	}
	if action == chat.ActionConfigure {
		result.Text = "Choose configuration values:"
		result.Blocks = ConfigurationBlocks(response, a.symbols, a.intervals)
	}
	return a.client.Respond(ctx, payload.ResponseURL, result)
}

func (a *Adapter) processInteraction(ctx context.Context, payload Interaction) error {
	target, err := a.targets.GetTarget(ctx, domain.ChatProviderSlack, a.tenantID, payload.Channel.ID)
	if err != nil {
		return a.respondError(ctx, payload.ResponseURL, err)
	}
	info, err := a.client.GetChannelInfo(ctx, payload.Channel.ID)
	if err != nil || info.Creator != payload.User.ID {
		return a.respondError(ctx, payload.ResponseURL, fmt.Errorf("only the channel creator can use this configuration control"))
	}
	if len(payload.Actions) != 1 {
		return a.respondError(ctx, payload.ResponseURL, fmt.Errorf("invalid Slack interaction"))
	}
	action := payload.Actions[0]
	command := chat.Command{
		Target:      target,
		ActorUserID: payload.User.ID,
		Action:      chat.ActionSave,
		EventID:     payload.TriggerID,
	}

	sessionID := action.Value
	if strings.Contains(action.Value, "|") {
		parts := strings.SplitN(action.Value, "|", 3)
		if len(parts) != 3 {
			return a.respondError(ctx, payload.ResponseURL, fmt.Errorf("invalid configuration control"))
		}
		sessionID = parts[0]
		session, getErr := a.sessions.Get(ctx, sessionID)
		if getErr != nil || session.TargetID != target.ID || session.ActorUserID != payload.User.ID {
			return a.respondError(ctx, payload.ResponseURL, fmt.Errorf("invalid configuration session"))
		}
		command.Arguments = []string{sessionID}
		switch parts[1] {
		case "symbol":
			session.SelectedSymbols = toggleString(session.SelectedSymbols, parts[2])

		case "interval":
			session.SelectedIntervals = toggleInterval(session.SelectedIntervals, domain.Interval(parts[2]))

		default:
			return a.respondError(ctx, payload.ResponseURL, fmt.Errorf("unsupported configuration control"))
		}

		if err := a.commands.UpdateSession(ctx, command, session.SelectedSymbols, session.SelectedIntervals); err != nil {
			return a.respondError(ctx, payload.ResponseURL, err)
		}
		return a.client.Respond(ctx, payload.ResponseURL, Response{
			ResponseType: "ephemeral",
			Text:         "Selection updated.",
			Blocks:       ConfigurationBlocks(sessionID, a.symbols, a.intervals),
		})
	}
	command.Arguments = []string{sessionID}
	switch action.ActionID {
	case "crypto_alert_save":
		if _, err := a.commands.Handle(ctx, command); err != nil {
			return a.respondError(ctx, payload.ResponseURL, err)
		}
		return a.client.Respond(ctx, payload.ResponseURL, Response{
			ResponseType: "ephemeral",
			Text:         "Configuration saved.",
		})

	case "crypto_alert_cancel":
		command.Action = chat.ActionCancel
		if _, err := a.commands.Handle(ctx, command); err != nil {
			return a.respondError(ctx, payload.ResponseURL, err)
		}
		return a.client.Respond(ctx, payload.ResponseURL, Response{
			ResponseType: "ephemeral",
			Text:         "Configuration cancelled.",
		})

	default:
		return a.respondError(ctx, payload.ResponseURL, fmt.Errorf("unsupported configuration control"))
	}
}

func (a *Adapter) targetForChannel(ctx context.Context, channelID, name, teamID string) (domain.AlertTarget, error) {
	info, err := a.client.GetChannelInfo(ctx, channelID)
	if err != nil {
		return domain.AlertTarget{}, err
	}
	return a.targets.FindOrCreateTarget(ctx, domain.AlertTarget{
		ID:             uuid.NewString(),
		Provider:       domain.ChatProviderSlack,
		TenantID:       a.tenantID,
		ExternalChatID: channelID,
		DisplayName:    name,
		CreatorUserID:  info.Creator,
		Enabled:        true,
	})
}

func (a *Adapter) validateContext(appID, teamID string) error {
	if a.expectedAppID != "" && appID != a.expectedAppID {
		return fmt.Errorf("unexpected Slack app")
	}
	if a.expectedTeamID != "" && teamID != a.expectedTeamID {
		return fmt.Errorf("unexpected Slack team")
	}
	return nil
}

func (a *Adapter) respondError(ctx context.Context, responseURL string, err error) error {
	return a.client.Respond(ctx, responseURL, Response{ResponseType: "ephemeral", Text: "The request could not be completed."})
}

func toggleString(values []string, value string) []string {
	for i, current := range values {
		if current == value {
			return append(append([]string(nil), values[:i]...), values[i+1:]...)
		}
	}
	return append(append([]string(nil), values...), value)
}

func toggleInterval(values []domain.Interval, value domain.Interval) []domain.Interval {
	for i, current := range values {
		if current == value {
			return append(append([]domain.Interval(nil), values[:i]...), values[i+1:]...)
		}
	}
	return append(append([]domain.Interval(nil), values...), value)
}
