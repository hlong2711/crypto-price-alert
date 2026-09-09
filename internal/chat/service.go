package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/service/configuration"
)

// ConfigurationService exposes the configuration operations required by chat commands.
type ConfigurationService interface {
	GetConfig(context.Context, string) (domain.AlertConfig, error)
	ReplaceConfig(context.Context, string, []string, []domain.Interval, bool, string, int64) (domain.AlertConfig, error)
	Enable(context.Context, string, string, int64) (domain.AlertConfig, error)
	Pause(context.Context, string, string, int64) (domain.AlertConfig, error)
}

// Service dispatches normalized chat commands and coordinates authorization,
// configuration updates, and interactive sessions.
type Service struct {
	configuration ConfigurationService
	authorizer    Authorizer
	sessions      SessionStore
	sessionTTL    time.Duration
}

// NewService creates a shared chat command service.
func NewService(configuration ConfigurationService, authorizer Authorizer, sessions SessionStore, sessionTTL time.Duration) (*Service, error) {
	if configuration == nil || authorizer == nil || sessions == nil || sessionTTL <= 0 {
		return nil, fmt.Errorf("invalid chat service settings")
	}
	return &Service{configuration: configuration, authorizer: authorizer, sessions: sessions, sessionTTL: sessionTTL}, nil
}

// Handle authorizes and executes a normalized command.
func (s *Service) Handle(ctx context.Context, command Command) (string, error) {
	if strings.TrimSpace(command.ActorUserID) == "" {
		return "", fmt.Errorf("actor identity is required")
	}
	if command.Action.IsMutation() {
		if err := s.authorizer.Authorize(ctx, AuthorizationRequest{Target: command.Target, ActorUserID: command.ActorUserID, Action: command.Action}); err != nil {
			return "", err
		}
	}
	switch command.Action {
	case ActionHelp:
		return "help, configure, show, enable, pause, test", nil
	case ActionShow:
		config, err := s.configuration.GetConfig(ctx, command.Target.ID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("enabled=%t symbols=%s intervals=%s version=%d", config.Enabled, strings.Join(config.Symbols, ","), joinIntervals(config.Intervals), config.Version), nil
	case ActionConfigure:
		config, err := s.configuration.GetConfig(ctx, command.Target.ID)
		if err != nil {
			return "", err
		}
		session, err := NewConfigSession(command.Target.ID, command.ActorUserID, config, s.sessionTTL)
		if err != nil {
			return "", err
		}
		if err := s.sessions.Create(ctx, session); err != nil {
			return "", err
		}
		return session.SessionID, nil
	case ActionEnable, ActionPause:
		config, err := s.configuration.GetConfig(ctx, command.Target.ID)
		if err != nil {
			return "", err
		}
		var updated domain.AlertConfig
		if command.Action == ActionEnable {
			updated, err = s.configuration.Enable(ctx, command.Target.ID, command.ActorUserID, config.Version)
		} else {
			updated, err = s.configuration.Pause(ctx, command.Target.ID, command.ActorUserID, config.Version)
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("enabled=%t version=%d", updated.Enabled, updated.Version), nil
	case ActionSave:
		return "", s.saveSession(ctx, command)
	case ActionCancel:
		if len(command.Arguments) != 1 {
			return "", fmt.Errorf("cancel requires a session ID")
		}
		if err := s.deleteOwnedSession(ctx, command.Arguments[0], command); err != nil {
			return "", err
		}
		return "cancelled", nil
	case ActionTest:
		return "test alert requested", nil
	default:
		return "", fmt.Errorf("unsupported action %q", command.Action)
	}
}

// UpdateSession replaces the selections in an actor-owned configuration session.
func (s *Service) UpdateSession(ctx context.Context, command Command, symbols []string, intervals []domain.Interval) error {
	if err := s.authorizer.Authorize(ctx, AuthorizationRequest{Target: command.Target, ActorUserID: command.ActorUserID, Action: ActionConfigure}); err != nil {
		return err
	}
	if len(command.Arguments) != 1 {
		return fmt.Errorf("session update requires a session ID")
	}
	session, err := s.sessions.Get(ctx, command.Arguments[0])
	if err != nil {
		return err
	}
	if session.TargetID != command.Target.ID || session.ActorUserID != command.ActorUserID {
		return fmt.Errorf("session does not belong to actor and target")
	}
	session.SelectedSymbols = append([]string(nil), symbols...)
	session.SelectedIntervals = append([]domain.Interval(nil), intervals...)
	return s.sessions.Update(ctx, session)
}

func (s *Service) saveSession(ctx context.Context, command Command) error {
	if len(command.Arguments) != 1 {
		return fmt.Errorf("save requires a session ID")
	}
	session, err := s.sessions.Get(ctx, command.Arguments[0])
	if err != nil {
		return err
	}
	if session.TargetID != command.Target.ID || session.ActorUserID != command.ActorUserID {
		return fmt.Errorf("session does not belong to actor and target")
	}
	config, err := s.configuration.GetConfig(ctx, session.TargetID)
	if err != nil {
		return err
	}
	if config.Version != session.BaseConfigVersion {
		return fmt.Errorf("configuration changed while session was open")
	}
	if _, err := s.configuration.ReplaceConfig(ctx, session.TargetID, session.SelectedSymbols, session.SelectedIntervals, config.Enabled, command.ActorUserID, session.BaseConfigVersion); err != nil {
		return err
	}
	return s.sessions.Delete(ctx, session.SessionID)
}

func (s *Service) deleteOwnedSession(ctx context.Context, sessionID string, command Command) error {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.TargetID != command.Target.ID || session.ActorUserID != command.ActorUserID {
		return fmt.Errorf("session does not belong to actor and target")
	}
	return s.sessions.Delete(ctx, sessionID)
}

func joinIntervals(intervals []domain.Interval) string {
	values := make([]string, 0, len(intervals))
	for _, interval := range intervals {
		values = append(values, string(interval))
	}
	return strings.Join(values, ",")
}

var _ ConfigurationService = (*configuration.Service)(nil)
