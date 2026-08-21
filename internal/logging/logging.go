package logging

import (
	"io"
	"log/slog"
)

// New returns a logger whose text output is stable logfmt. Command-line logs
// omit timestamps because the surrounding process already provides ordering.
func New(output io.Writer) *slog.Logger {
	handler := slog.NewTextHandler(output, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	})
	return slog.New(handler)
}
