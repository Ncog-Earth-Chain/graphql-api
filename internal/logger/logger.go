package logger

import (
	"fmt"
	"io"
	"ncogearthchain-api-graphql/internal/config"
	"os"
	"strings"

	"github.com/op/go-logging"
)

// ApiLogger defines extended logger with generic no-level logging option
type ApiLogger struct {
	logging.Logger
}

// Printf implements default non-leveled output.
// We assume the information is low in importance if passed to this function so we relay it to Debug level.
func (a ApiLogger) Printf(format string, args ...interface{}) {
	a.Debugf(format, args...)
}

// New provides pre-configured Logger with stderr output and leveled filtering.
// Modules are not supported at the moment, but may be added in the future to make the logging setup more granular.
func New(cfg *config.Config) Logger {
	// Prep the backend for exporting the log records, sending them to the sink chosen by
	// configuration: "stderr" (default) or "stdout", otherwise a filesystem path to append to.
	// A path that cannot be opened degrades to stderr with a one-line notice rather than
	// stopping the server from starting -- a misconfigured log destination is not fatal.
	backend := logging.NewLogBackend(logOutput(cfg.Log.Output), "", 0)

	// Parse log format from configuration and apply it to the backend
	format := logging.MustStringFormatter(cfg.Log.Format)
	fmtBackend := logging.NewBackendFormatter(backend, format)

	// Parse and apply the configured level on which the recording will be emitted
	level, err := logging.LogLevel(cfg.Log.Level)
	if err != nil {
		level = logging.INFO
	}
	lvlBackend := logging.AddModuleLevel(fmtBackend)
	lvlBackend.SetLevel(level, "")

	// assign the backend and return the new logger
	logging.SetBackend(lvlBackend)
	l := logging.MustGetLogger(cfg.AppName)

	return &ApiLogger{*l}
}

// logOutput resolves the configured log sink to a writer.
//
// "stderr" (and the empty default) and "stdout" map to the standard streams; anything else is
// treated as a filesystem path opened for append. If the path cannot be opened the log falls back
// to stderr with a single notice, so a bad log destination never prevents the server from starting.
// The file handle is intentionally not closed: the logger lives for the whole process and the OS
// reclaims it on exit.
func logOutput(out string) io.Writer {
	switch strings.ToLower(strings.TrimSpace(out)) {
	case "", "stderr":
		return os.Stderr
	case "stdout":
		return os.Stdout
	default:
		f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "can not open log output %q: %s; falling back to stderr\n", out, err)
			return os.Stderr
		}
		return f
	}
}
