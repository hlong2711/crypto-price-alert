package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"crypto-price-alert/internal/config"
)

func TestFundingStartupIsLazyAndIndependent(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer server.Close()
	cfg := config.Config{Funding: config.DefaultFundingConfig(), Retry: config.RetryConfig{MaxAttempts: 1, InitialBackoff: time.Millisecond}}
	cfg.Funding.BaseURL = server.URL
	s, err := newFundingService(cfg, nil)
	if err != nil || s == nil {
		t.Fatal(s, err)
	}
	s.Close()
	if calls.Load() != 0 {
		t.Fatal("startup contacted unavailable upstream")
	}
	cfg.Funding.Enabled = false
	s, err = newFundingService(cfg, nil)
	if err != nil || s != nil || calls.Load() != 0 {
		t.Fatal(s, err, calls.Load())
	}
}
