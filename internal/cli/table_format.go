package cli

import "strings"

const defaultErrorPreviewLen = 48

// formatAlignedTable renders a fixed-width, terminal-friendly table.
func formatAlignedTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}

	for _, row := range rows {
		for i := 0; i < len(headers) && i < len(row); i++ {
			if len(row[i]) > widths[i] {
				widths[i] = len(row[i])
			}
		}
	}

	var b strings.Builder
	b.WriteString(formatAlignedRow(headers, widths))
	b.WriteByte('\n')

	for _, row := range rows {
		cells := make([]string, len(headers))
		copy(cells, row)
		b.WriteString(formatAlignedRow(cells, widths))
		b.WriteByte('\n')
	}

	return b.String()
}

func formatAlignedRow(row []string, widths []int) string {
	var b strings.Builder
	for i := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		b.WriteString(cell)
		if pad := widths[i] - len(cell); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		if i < len(widths)-1 {
			b.WriteString("  ")
		}
	}
	return b.String()
}

func truncatePreview(input string, maxLen int) string {
	if maxLen <= 0 || len(input) <= maxLen {
		return input
	}
	if maxLen <= 3 {
		return input[:maxLen]
	}
	return input[:maxLen-3] + "..."
}

func formatRestoreHistoryRow(cells ...string) string {
	return strings.Join(cells, " | ")
}
