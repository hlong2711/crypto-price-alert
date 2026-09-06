package notification

import (
	"context"
	"log/slog"

	"crypto-price-alert/internal/domain"
)

// DryRunNotifier logs messages instead of forwarding them to an external channel.
type DryRunNotifier struct {
	name   string
	logger *slog.Logger
}

func NewDryRunNotifier(name string, logger *slog.Logger) *DryRunNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &DryRunNotifier{name: name, logger: logger}
}

func (n *DryRunNotifier) Send(_ context.Context, message domain.Message) error {
	n.logger.Info("notification dry run", "notifier", n.name, "message", RenderMessage(message))
	return nil
}
