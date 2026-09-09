package chat

import (
	"context"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

func TestMemorySessionStoreEnforcesExpirationAndSingleUse(t *testing.T) {
	store := NewMemorySessionStore()
	session, err := NewConfigSession("target", "creator", domain.AlertConfig{Version: 3, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), session.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), session.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), session.SessionID); err == nil {
		t.Fatal("expected deleted session to be unavailable")
	}
}

func TestCreatorAuthorizer(t *testing.T) {
	authorizer := CreatorAuthorizer{}
	target := domain.AlertTarget{CreatorUserID: "creator"}
	if err := authorizer.Authorize(context.Background(), AuthorizationRequest{Target: target, ActorUserID: "creator", Action: ActionPause}); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.Authorize(context.Background(), AuthorizationRequest{Target: target, ActorUserID: "member", Action: ActionPause}); err == nil {
		t.Fatal("expected non-creator rejection")
	}
}
