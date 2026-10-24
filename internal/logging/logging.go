package logging

import (
	"io"
	"log/slog"
	"os"

	clog "github.com/containerd/log"
)

const (
	FormatText = "text"
	FormatJSON = "json"
)

func NewHandler(w io.Writer, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if format == FormatJSON {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

func Setup(format string) {
	slog.SetDefault(slog.New(NewHandler(os.Stderr, format)))
	clog.UseSlog()
}
