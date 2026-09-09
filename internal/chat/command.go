package chat

import (
	"context"
	"fmt"
	"strings"

	"crypto-price-alert/internal/domain"
)

// CommandAction identifies a normalized chat operation.
type CommandAction string

const (
	ActionHelp      CommandAction = "help"
	ActionConfigure CommandAction = "configure"
	ActionShow      CommandAction = "show"
	ActionEnable    CommandAction = "enable"
	ActionPause     CommandAction = "pause"
	ActionTest      CommandAction = "test"
	ActionSave      CommandAction = "save"
	ActionCancel    CommandAction = "cancel"
)

func (a CommandAction) IsMutation() bool {
	return a == ActionConfigure || a == ActionEnable || a == ActionPause || a == ActionTest || a == ActionSave || a == ActionCancel
}

// Command is the platform-independent representation of a chat request.
type Command struct {
	Target      domain.AlertTarget
	ActorUserID string
	Action      CommandAction
	Arguments   []string
	EventID     string
}

// AuthorizationRequest contains the identity and target needed for authorization.
type AuthorizationRequest struct {
	Target      domain.AlertTarget
	ActorUserID string
	Action      CommandAction
}

// Authorizer verifies whether an actor may perform a command action.
type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) error
}

// CreatorAuthorizer permits actions only when the actor is the target creator.
type CreatorAuthorizer struct{}

func (CreatorAuthorizer) Authorize(_ context.Context, request AuthorizationRequest) error {
	if strings.TrimSpace(request.ActorUserID) == "" {
		return fmt.Errorf("actor identity is required")
	}
	if request.Target.CreatorUserID == "" || request.ActorUserID != request.Target.CreatorUserID {
		return fmt.Errorf("only the chat creator can perform %s", request.Action)
	}
	return nil
}
