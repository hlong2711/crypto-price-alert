package notification

import (
	"context"
	"crypto-price-alert/internal/domain"
)

type Notifier interface {
	Send(ctx context.Context, message domain.Message) error
}

// TargetNotifier delivers a message to the destination carried by an alert target.
type TargetNotifier interface {
	SendToTarget(ctx context.Context, target domain.AlertTarget, message domain.Message) error
}

// TargetNotifierProvider identifies which chat provider a target notifier serves.
type TargetNotifierProvider interface {
	TargetProvider() domain.ChatProvider
}

// SendToTarget uses target-aware delivery when available and preserves legacy notifier behavior otherwise.
func SendToTarget(ctx context.Context, notifier Notifier, target domain.AlertTarget, message domain.Message) error {
	if provider, ok := notifier.(TargetNotifierProvider); ok && provider.TargetProvider() != target.Provider {
		return nil
	}
	if targetNotifier, ok := notifier.(TargetNotifier); ok {
		return targetNotifier.SendToTarget(ctx, target, message)
	}
	return notifier.Send(ctx, message)
}
