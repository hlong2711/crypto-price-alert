package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/domain"
)

// create command list for bot from generic chat command definition
func botCommands() []BotCommand {
	definitions := chat.PublicCommandDefinitions()
	commands := make([]BotCommand, 0, len(definitions))
	for _, definition := range definitions {
		commands = append(commands, BotCommand{Command: definition.Command, Description: definition.Description})
	}
	return commands
}

type CommandService interface {
	Handle(context.Context, chat.Command) (string, error)
	UpdateSession(context.Context, chat.Command, []string, []domain.Interval) error
}

type EventRepository interface {
	ClaimInboundEvent(context.Context, domain.InboundEvent) (bool, error)
	MarkInboundEventProcessed(context.Context, domain.ChatProvider, string, time.Time) error
	MarkInboundEventFailed(context.Context, domain.ChatProvider, string, string) error
}

type TargetRepository interface {
	FindOrCreateTarget(context.Context, domain.AlertTarget) (domain.AlertTarget, error)
	GetTarget(context.Context, domain.ChatProvider, string, string) (domain.AlertTarget, error)
}

func parseCallback(data string) (string, string, error) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != "crypto-alert" || parts[1] == "" || parts[2] == "" {
		return "", "", fmt.Errorf("invalid callback payload")
	}
	return parts[1], parts[2], nil
}

func callbackData(sessionID, action string) string {
	return "crypto-alert:" + sessionID + ":" + action
}

func chatIDString(id int64) string { return strconv.FormatInt(id, 10) }

func userIDString(id int64) string { return strconv.FormatInt(id, 10) }
