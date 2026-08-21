package logging

import (
	"bytes"
	"testing"
)

func TestNewWritesLogfmt(t *testing.T) {
	var output bytes.Buffer
	New(&output).Warn("download failed", "path", "one two.png")
	want := `level=WARN msg="download failed" path="one two.png"` + "\n"
	if output.String() != want {
		t.Fatalf("log output = %q, want %q", output.String(), want)
	}
}
