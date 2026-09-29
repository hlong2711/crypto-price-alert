package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"crypto-price-alert/internal/domain"

	"github.com/google/uuid"
)

// ConfigSession stores temporary configuration selections for one actor and target.
type ConfigSession struct {
	SessionID         string
	TargetID          string
	ActorUserID       string
	BaseConfigVersion int64
	SelectedSymbols   []string
	SelectedIntervals []domain.Interval
	ExpiresAt         time.Time
}

func (s ConfigSession) Validate(now time.Time) error {
	if s.SessionID == "" || s.TargetID == "" || s.ActorUserID == "" || s.ExpiresAt.IsZero() {
		return fmt.Errorf("invalid configuration session")
	}
	if !now.Before(s.ExpiresAt) {
		return fmt.Errorf("configuration session expired")
	}
	return nil
}

// SessionStore persists short-lived configuration sessions.
type SessionStore interface {
	Create(context.Context, ConfigSession) error
	Get(context.Context, string) (ConfigSession, error)
	Update(context.Context, ConfigSession) error
	Delete(context.Context, string) error
}

// MemorySessionStore stores sessions in process memory for a single-instance deployment.
type MemorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]ConfigSession
	now      func() time.Time
}

// NewMemorySessionStore creates an empty in-memory session store.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: make(map[string]ConfigSession), now: time.Now}
}

func (s *MemorySessionStore) Create(_ context.Context, session ConfigSession) error {
	if err := session.Validate(s.now()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[session.SessionID]; exists {
		return fmt.Errorf("session already exists")
	}
	s.sessions[session.SessionID] = cloneSession(session)
	return nil
}

func (s *MemorySessionStore) Get(_ context.Context, sessionID string) (ConfigSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return ConfigSession{}, fmt.Errorf("session not found")
	}
	if err := session.Validate(s.now()); err != nil {
		delete(s.sessions, sessionID)
		return ConfigSession{}, err
	}
	return cloneSession(session), nil
}

func (s *MemorySessionStore) Update(_ context.Context, session ConfigSession) error {
	if err := session.Validate(s.now()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[session.SessionID]; !ok {
		return fmt.Errorf("session not found")
	}
	s.sessions[session.SessionID] = cloneSession(session)
	return nil
}

func (s *MemorySessionStore) Delete(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return fmt.Errorf("session not found")
	}
	delete(s.sessions, sessionID)
	return nil
}

func cloneSession(session ConfigSession) ConfigSession {
	session.SelectedSymbols = append([]string(nil), session.SelectedSymbols...)
	session.SelectedIntervals = append([]domain.Interval(nil), session.SelectedIntervals...)
	return session
}

// NewConfigSession creates an expiring session initialized from the current configuration.
func NewConfigSession(targetID, actorUserID string, config domain.AlertConfig, ttl time.Duration) (ConfigSession, error) {
	if ttl <= 0 {
		return ConfigSession{}, fmt.Errorf("session ttl must be positive")
	}
	return ConfigSession{
		SessionID:         uuid.NewString(),
		TargetID:          targetID,
		ActorUserID:       actorUserID,
		BaseConfigVersion: config.Version,
		SelectedSymbols:   append([]string(nil), config.Symbols...),
		SelectedIntervals: append([]domain.Interval(nil), config.Intervals...),
		ExpiresAt:         time.Now().Add(ttl),
	}, nil
}
