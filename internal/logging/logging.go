// Package logging configures the process-wide logger.
package logging

import (
	"io"
	"log/slog"
	"os"

	clog "github.com/containerd/log"
)

// Formats accepted by [NewHandler] and [Setup].
const (
	FormatText = "text"
	FormatJSON = "json"
)

// NewHandler returns the slog handler shared by pici and its log adapters, so
// every log line in the container uses the same format.
func NewHandler(w io.Writer, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if format == FormatJSON {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// Setup installs the default slog logger and forwards the logs of libraries
// that still use logrus (Docker and containerd clients) to it.
func Setup(format string) {
	slog.SetDefault(slog.New(NewHandler(os.Stderr, format)))
	clog.UseSlog()
}
