package chat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// PublicIPReader reports the application's public outbound address.
type PublicIPReader interface {
	PublicIP(context.Context) (netip.Addr, error)
}

type publicIPResolver struct {
	mu        sync.Mutex
	client    *http.Client
	endpoint  string
	address   netip.Addr
	err       error
	expiresAt time.Time
}

// NewPublicIPResolver uses ipify with a two-second timeout. Successful lookups
// are cached for five minutes; failures for thirty seconds.
func NewPublicIPResolver() PublicIPReader {
	return &publicIPResolver{
		client:   &http.Client{Timeout: 2 * time.Second},
		endpoint: "https://api.ipify.org",
	}
}

func (r *publicIPResolver) PublicIP(ctx context.Context) (netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return netip.Addr{}, err
	}
	if time.Now().Before(r.expiresAt) {
		return r.address, r.err
	}
	address, err := r.lookup(ctx)
	ttl := 5 * time.Minute
	if err != nil {
		ttl = 30 * time.Second // cache failures for a shorter time
	}
	r.address, r.err, r.expiresAt = address, err, time.Now().Add(ttl)
	return address, err
}

func (r *publicIPResolver) lookup(ctx context.Context) (netip.Addr, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint, nil)
	if err != nil {
		return netip.Addr{}, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("public IP lookup status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65))
	if err != nil {
		return netip.Addr{}, err
	}

	address, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	address = address.Unmap()
	if err != nil || !address.IsGlobalUnicast() || address.IsPrivate() {
		return netip.Addr{}, fmt.Errorf("invalid public IP address")
	}
	return address, nil
}
