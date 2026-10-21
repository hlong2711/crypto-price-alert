package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

func TestPublicIPResolver(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"IPv4", "203.0.113.10\n", "203.0.113.10", 200},
		{"IPv6", "2001:db8::1", "2001:db8::1", 200},
		{"mapped private", "::ffff:192.168.1.1", "", 200},
		{"private", "172.17.0.2", "", 200},
		{"loopback", "127.0.0.1", "", 200},
		{"link local", "169.254.1.1", "", 200},
		{"invalid", "not an IP", "", 200},
		{"oversized", strings.Repeat("x", 1000), "", 200},
		{"upstream error", "203.0.113.10", "", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			resolver := &publicIPResolver{client: server.Client(), endpoint: server.URL}
			var wg sync.WaitGroup
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					address, err := resolver.PublicIP(context.Background())
					if tc.want == "" {
						if err == nil || address.IsValid() {
							t.Errorf("expected failure, got %v, %v", address, err)
						}
					} else if err != nil || address.String() != tc.want {
						t.Errorf("got %v, %v; want %s", address, err, tc.want)
					}
				}()
			}
			wg.Wait()
			if calls.Load() != 1 {
				t.Fatalf("cached lookup made %d calls", calls.Load())
			}
			resolver.expiresAt = time.Time{}
			resolver.PublicIP(context.Background())
			if calls.Load() != 2 {
				t.Fatal("expired cache was not refreshed")
			}
		})
	}
}

func TestPublicIPResolverTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	resolver := &publicIPResolver{client: &http.Client{Timeout: 20 * time.Millisecond}, endpoint: server.URL}
	if _, err := resolver.PublicIP(context.Background()); err == nil {
		t.Fatal("expected timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.PublicIP(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

type chatPublicIPFunc func(context.Context) (netip.Addr, error)

func (f chatPublicIPFunc) PublicIP(ctx context.Context) (netip.Addr, error) {
	return f(ctx)
}

func TestFundingPublicIPFooter(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		s, configuration, sessions, target := newChatService(t)
		target.Provider = domain.ChatProviderTelegram
		reader := chatFundingFunc(func(context.Context, int) (domain.FundingRateResult, error) {
			return domain.FundingRateResult{RequestedLimit: 5}, nil
		})
		ipReader := chatPublicIPFunc(func(context.Context) (netip.Addr, error) {
			if unavailable {
				return netip.Addr{}, errors.New("sensitive upstream details")
			}
			return netip.MustParseAddr("203.0.113.10"), nil
		})
		var err error
		s, err = NewService(configuration, CreatorAuthorizer{}, sessions, time.Minute, WithFundingReader(reader), WithPublicIPReader(ipReader))
		if err != nil {
			t.Fatal(err)
		}
		message, err := s.Handle(context.Background(), Command{Target: target, ActorUserID: "member", Action: ActionFunding})
		want := "Public IP: 203.0.113.10"
		if unavailable {
			want = "Public IP: unavailable"
		}
		if err != nil || !strings.HasSuffix(message, want) || !strings.Contains(message, "No eligible positive-rate pairs") || strings.Contains(message, "sensitive") {
			t.Fatal(message, err)
		}
	}
}
