package api

import (
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestHealth(t *testing.T) {
	e := echo.New()
	record := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/health", nil)
	ctx := e.NewContext(request, record)
	if err := (&Handler{}).Health(ctx); err != nil {
		t.Fatal(err)
	}
	if record.Code != 200 {
		t.Fatalf("status=%d, want 200", record.Code)
	}
	if record.Body.String() != `{"status":"ok"}
` {
		t.Fatalf("body=%q", record.Body.String())
	}
}

func TestNewServerRegistersHealthRoute(t *testing.T) {
	e := NewServer()
	record := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/health", nil)
	e.ServeHTTP(record, request)
	if record.Code != 200 {
		t.Fatalf("status=%d, want 200", record.Code)
	}
}
