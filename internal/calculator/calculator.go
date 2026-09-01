package calculator

import (
	"fmt"
	"math"
)

func CalculateChange(open, close float64) (float64, error) {
	if !isFinite(open) || open <= 0 {
		return 0, fmt.Errorf("open price must be finite and positive")
	}
	if !isFinite(close) || close < 0 {
		return 0, fmt.Errorf("close price must be finite and non-negative")
	}
	return (close - open) / open * 100, nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
