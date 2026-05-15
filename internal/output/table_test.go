package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableSanitizesLongMultilineValues(t *testing.T) {
	var buf bytes.Buffer
	Table(&buf, []string{"字段"}, [][]string{{strings.Repeat("a", 130) + "\nnext"}})
	out := buf.String()
	if strings.Contains(out, "\nnext") {
		t.Fatalf("expected embedded newline to be escaped: %q", out)
	}
	if !strings.Contains(out, "...") {
		t.Fatalf("expected long value truncation: %q", out)
	}
}

func TestOneLineCollapsesWhitespace(t *testing.T) {
	got := OneLine("Microsoft SQL Server\n\tApr 29 2016\r\nEnterprise")
	want := "Microsoft SQL Server Apr 29 2016 Enterprise"
	if got != want {
		t.Fatalf("OneLine() = %q, want %q", got, want)
	}
}
