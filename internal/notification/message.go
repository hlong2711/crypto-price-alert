package notification

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-price-alert/internal/domain"
)

type PriceResult struct {
	Change      domain.PriceChange
	Unavailable bool
}

func BuildMessage(period domain.Period, results []PriceResult, location *time.Location) (domain.Message, error) {
	if err := period.Validate(); err != nil {
		return domain.Message{}, fmt.Errorf("validate message period: %w", err)
	}
	if location == nil {
		return domain.Message{}, fmt.Errorf("message location is required")
	}
	if len(results) == 0 {
		return domain.Message{}, fmt.Errorf("message results must not be empty")
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Change.Symbol < results[j].Change.Symbol
	})

	lines := make([]string, 0, len(results))
	for _, result := range results {
		if result.Change.Symbol == "" {
			return domain.Message{}, fmt.Errorf("message symbol is required")
		}
		if result.Unavailable {
			lines = append(lines, fmt.Sprintf("%s   unavailable ⚠️", result.Change.Symbol))
			continue
		}
		icon := "🟢"
		if result.Change.ChangePct < 0 {
			icon = "🔴"
		}
		lines = append(lines, fmt.Sprintf(
			"%-8s $%s %+.2f%% %s Vol: %s",
			result.Change.Symbol,
			formatPrice(result.Change.Close),
			result.Change.ChangePct,
			icon,
			formatVolume(result.Change.Volume),
		))
	}

	title := fmt.Sprintf("📊 Crypto %s Update", period.Interval)
	periodText := fmt.Sprintf(
		"%s → %s %s",
		period.Start.In(location).Format("15:04"),
		period.End.In(location).Format("15:04"),
		location.String(),
	)
	return domain.Message{
		Title:  title,
		Period: period,
		Lines:  append([]string{periodText}, lines...)}, nil
}

func RenderMessage(message domain.Message) string {
	return strings.Join(append([]string{message.Title}, message.Lines...), "\n")
}

func formatPrice(value float64) string {
	if value >= 1000 {
		return fmt.Sprintf("%.2f", value)
	}
	if value >= 1 {
		return fmt.Sprintf("%.4f", value)
	}
	return fmt.Sprintf("%.8f", value)
}

func formatVolume(value float64) string {
	formatted := strconv.FormatFloat(value, 'f', 5, 64)
	formatted = strings.TrimRight(formatted, "0")
	return strings.TrimRight(formatted, ".")
}
