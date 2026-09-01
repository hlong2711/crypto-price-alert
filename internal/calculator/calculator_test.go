package calculator

import (
	"math"
	"testing"
)

func TestCalculateChange(t *testing.T) {
	tests := []struct {
		name        string
		open, close float64
		want        float64
	}{
		{"increase", 100, 110, 10},
		{"decrease", 100, 90, -10},
		{"unchanged", 100, 100, 0},
		{"fractional", 100, 100.5, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateChange(tt.open, tt.close)
			if err != nil {
				t.Fatalf("CalculateChange() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("CalculateChange() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateChangeRejectsInvalidPrices(t *testing.T) {
	tests := []struct {
		name        string
		open, close float64
	}{
		{"zero open", 0, 100},
		{"negative open", -1, 100},
		{"nan open", math.NaN(), 100},
		{"infinite open", math.Inf(1), 100},
		{"nan close", 100, math.NaN()},
		{"negative close", 100, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CalculateChange(tt.open, tt.close); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
