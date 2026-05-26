package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

const tablePadding = 3

type TableFormatter struct {
	header []string
	rows   [][]string
}

func NewTableFormatter(header []string, rows ...[]string) *TableFormatter {
	h := make([]string, len(header))
	for i, cell := range header {
		h[i] = strings.ToUpper(cell)
	}
	return &TableFormatter{
		header: h,
		rows:   rows,
	}
}

func (t *TableFormatter) AddRows(rows ...[]string) {
	t.rows = append(t.rows, rows...)
}

func (t *TableFormatter) Write(out io.Writer) error {
	w := tabwriter.NewWriter(out, 0, 0, tablePadding, ' ', 0)
	if err := t.writeRow(w, t.header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	for i, row := range t.rows {
		if err := t.writeRow(w, row); err != nil {
			return fmt.Errorf("failed to write row %d: %w", i, err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("failed to flush table output: %w", err)
	}
	return nil
}

func (t *TableFormatter) writeRow(w *tabwriter.Writer, row []string) error {
	for i, cell := range row {
		_, err := w.Write([]byte(cell))
		if err != nil {
			return fmt.Errorf("failed to write cell %d: %w", i, err)
		}
		if i+1 < len(row) {
			_, err := w.Write([]byte("\t"))
			if err != nil {
				return fmt.Errorf("failed to write spacer for cell %d: %w", i, err)
			}
		}
	}
	w.Write([]byte("\n"))
	return nil
}
