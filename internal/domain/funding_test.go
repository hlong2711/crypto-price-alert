package domain

import (
	"strings"
	"testing"
	"time"
)

func TestFundingDecimals(t *testing.T) {
	for _, value := range []string{"NaN", "Inf", "1/2", "1e-3", "", " 0.1", ".1", "1.", strings.Repeat("1", 65)} {
		if _, err := ParseFundingDecimal(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	a, _ := ParseFundingDecimal("0.00000000000000000002")
	b, _ := ParseFundingDecimal("0.00000000000000000001")
	if a.Cmp(b) <= 0 {
		t.Fatal("lost exact ordering")
	}
	c, _ := ParseFundingDecimal("0.10")
	d, _ := ParseFundingDecimal("0.1")
	if c.Cmp(d) != 0 {
		t.Fatal("unequal equivalent decimals")
	}
	for value, want := range map[string]string{"0.0005": "+0.05%", "0": "0%", "-0.001": "-0.1%", "0.00000000000000000001": "+1e-18%"} {
		got, err := FundingPercent(value)
		if err != nil || got != want {
			t.Errorf("%s: got %s (%v), want %s", value, got, err, want)
		}
	}
}

func TestFundingValidation(t *testing.T) {
	now := time.Now().UTC()
	o := FundingObservation{Symbol: "BTCUSDT", Rate: "0.001", ObservedAt: now, NextFundingTime: now.Add(time.Hour)}
	if err := o.Validate(now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*FundingObservation){
		func(o *FundingObservation) { o.ObservedAt = now.Add(-3 * time.Minute) },
		func(o *FundingObservation) { o.ObservedAt = now.Add(6 * time.Second) },
		func(o *FundingObservation) { o.NextFundingTime = now },
		func(o *FundingObservation) { o.Symbol = "<bad>" },
	} {
		bad := o
		mutate(&bad)
		if bad.Validate(now) == nil {
			t.Fatal("accepted invalid observation")
		}
	}
	for _, v := range []string{"", "4", "21", "5.0", "-5", " 5", "+5", "005"} {
		if _, err := ParseFundingLimit(v); err == nil {
			t.Errorf("accepted %q", v)
		}
	}
	for _, v := range []string{"5", "10", "20"} {
		if _, err := ParseFundingLimit(v); err != nil {
			t.Fatal(err)
		}
	}
	if (FundingContract{Symbol: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT"}).Validate() != nil {
		t.Fatal("valid contract rejected")
	}
	if (FundingContract{Symbol: "币安人生USDT", BaseAsset: "币安人生", QuoteAsset: "USDT"}).Validate() != nil {
		t.Fatal("valid Unicode Binance contract rejected")
	}
	if (FundingContract{Symbol: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "BTC"}).Validate() == nil {
		t.Fatal("unsupported quote accepted")
	}
}
