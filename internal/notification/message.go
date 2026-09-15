package notification

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

	rows := make([][]string, 0, len(results))
	for _, result := range results {
		if result.Change.Symbol == "" {
			return domain.Message{}, fmt.Errorf("message symbol is required")
		}
		if result.Unavailable {
			rows = append(rows, []string{result.Change.Symbol, "-", "unavailable ⚠️", "-"})
			continue
		}
		icon := "🟢"
		if result.Change.ChangePct < 0 {
			icon = "🔴"
		}
		rows = append(rows, []string{
			result.Change.Symbol,
			"$" + formatPrice(result.Change.Close),
			fmt.Sprintf("%+.2f%% %s", result.Change.ChangePct, icon),
			formatVolume(result.Change.Volume),
		})
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
		Lines:  append([]string{periodText, ""}, formatTable(rows)...)}, nil
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

func formatTable(rows [][]string) []string {
	headers := []string{"Symbol", "Price", "Change", "Volume"}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if width := utf8.RuneCountInString(cell); width > widths[i] {
				widths[i] = width
			}
		}
	}

	lines := []string{
		formatTableRow(headers, widths),
		formatTableSeparator(widths),
	}
	for _, row := range rows {
		lines = append(lines, formatTableRow(row, widths))
	}
	return lines
}

func formatTableRow(cells []string, widths []int) string {
	padded := make([]string, len(cells))
	for i, cell := range cells {
		padded[i] = cell + strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell))
	}
	return strings.Join(padded, "  ")
}

func formatTableSeparator(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = strings.Repeat("-", width)
	}
	return strings.Join(parts, "  ")
}
