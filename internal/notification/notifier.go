package notification

import (
	"context"
	"crypto-price-alert/internal/domain"
)

type Notifier interface {
	Send(ctx context.Context, message domain.Message) error
}
