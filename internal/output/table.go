package output

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func Section(w io.Writer, title string) {
	fmt.Fprintf(w, "\n==== %s ====\n", title)
}

func Table(w io.Writer, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range rows {
		for i := range headers {
			if i < len(row) && displayWidth(row[i]) > widths[i] {
				widths[i] = displayWidth(row[i])
			}
		}
	}
	writeRow(w, headers, widths)
	sep := make([]string, len(headers))
	for i, width := range widths {
		sep[i] = strings.Repeat("-", width)
	}
	writeRow(w, sep, widths)
	for _, row := range rows {
		padded := make([]string, len(headers))
		copy(padded, row)
		writeRow(w, padded, widths)
	}
}

func writeRow(w io.Writer, row []string, widths []int) {
	parts := make([]string, len(widths))
	for i := range widths {
		cell := ""
		if i < len(row) {
			cell = sanitize(row[i])
		}
		parts[i] = cell + strings.Repeat(" ", widths[i]-displayWidth(cell))
	}
	fmt.Fprintln(w, strings.Join(parts, "  "))
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	if utf8.RuneCountInString(s) > 120 {
		runes := []rune(s)
		return string(runes[:117]) + "..."
	}
	return s
}

func displayWidth(s string) int {
	width := 0
	for _, r := range sanitize(s) {
		if r > 127 {
			width += 2
		} else {
			width++
		}
	}
	return width
}
