// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package logger implements a log/slog based logger in Zarf.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	"github.com/phsym/console-slog"

	"github.com/golang-cz/devslog"
)

var defaultLogger atomic.Pointer[slog.Logger]

// init sets a logger with default config when the package is initialized.
func init() {
	l, _ := New(ConfigDefault()) //nolint:errcheck
	SetDefault(l)
}

// Level declares each supported log level. These are 1:1 what log/slog supports by default. Info is the default level.
type Level int

// Store names for Levels
var (
	Debug = Level(slog.LevelDebug) // -4
	Info  = Level(slog.LevelInfo)  // 0
	Warn  = Level(slog.LevelWarn)  // 4
	Error = Level(slog.LevelError) // 8
)

// String returns the string representation of the Level.
func (l Level) String() string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// validLevels is a set that provides an ergonomic way to check if a level is a member of the set.
var validLevels = map[Level]bool{
	Debug: true,
	Info:  true,
	Warn:  true,
	Error: true,
}

// strLevels maps a string to its Level.
var strLevels = map[string]Level{
	// NOTE(mkcp): Map trace to debug for backwards compatibility.
	"trace": Debug,
	"debug": Debug,
	"info":  Info,
	"warn":  Warn,
	"error": Error,
}

// ParseLevel takes a string representation of a Level, ensure it exists, and then converts it into a Level.
func ParseLevel(s string) (Level, error) {
	k := strings.ToLower(s)
	l, ok := strLevels[k]
	if !ok {
		return 0, fmt.Errorf("invalid log level: %s", k)
	}
	return l, nil
}

// Format declares the kind of logging handler to use.
// NOTE(mkcp): An empty Format defaults to "none" while logger is being worked on, but this is intended to use "text"
// on release.
type Format string

// ToLower takes a Format string and converts it to lowercase for case-agnostic validation. Users shouldn't have to care
// about "json" vs. "JSON" for example - they should both work.
func (f Format) ToLower() Format {
	return Format(strings.ToLower(string(f)))
}

var (
	// FormatJSON uses the standard slog JSONHandler
	FormatJSON Format = "json"
	// FormatConsole uses console-slog to provide prettier colorful messages
	FormatConsole Format = "console"
	// FormatDev uses a verbose and pretty printing devslog handler
	FormatDev Format = "dev"
	// FormatLegacy indicates that the user is continuing to use the legacy logger. The same as FormatNone.
	// This option will be removed in a coming release
	FormatLegacy Format = "legacy"
	// FormatNone sends log writes to DestinationNone / io.Discard
	FormatNone Format = "none"
)

// Destination declares an io.Writer to send logs to.
type Destination io.Writer

var (
	// DestinationDefault points to Stderr
	DestinationDefault Destination = os.Stderr
	// DestinationNone discards logs as they are received
	DestinationNone Destination = io.Discard
)

// can't define method on Destination type
func destinationString(d Destination) string {
	switch {
	case d == DestinationDefault:
		return "os.Stderr"
	case d == DestinationNone:
		return "io.Discard"
	default:
		return "unknown"
	}
}

// Config is configuration for a logger.
type Config struct {
	// Level sets the log level. An empty value corresponds to Info aka 0.
	Level
	Format
	Destination
	Color
}

// Color is a type that represents whether or not to use color in the logger.
type Color bool

// LogValue of config
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("level", c.Level.String()),
		slog.Any("format", c.Format),
		slog.Any("destination", destinationString(c.Destination)),
		slog.Bool("color", bool(c.Color)),
	)
}

// ConfigDefault returns a Config with defaults like Text formatting at Info level writing to Stderr.
func ConfigDefault() Config {
	return Config{
		Level:       Info,
		Format:      FormatConsole,
		Destination: DestinationDefault, // Stderr
		Color:       true,
	}
}

// New takes a Config and returns a validated logger.
func New(cfg Config) (*slog.Logger, error) {
	// Use default destination if none
	if cfg.Destination == nil {
		cfg.Destination = DestinationDefault
	}

	// Check that we have a valid log level.
	if !validLevels[cfg.Level] {
		return nil, fmt.Errorf("unsupported log level: %d", cfg.Level)
	}

	opts := slog.HandlerOptions{
		Level: slog.Level(cfg.Level),
	}

	var handler slog.Handler
	switch cfg.Format.ToLower() {
	case FormatConsole:
		handler = console.NewHandler(cfg.Destination, &console.HandlerOptions{
			Level:   slog.Level(cfg.Level),
			NoColor: !bool(cfg.Color),
		})
	case FormatJSON:
		handler = slog.NewJSONHandler(cfg.Destination, &opts)
	case FormatConsole:
		handler = console.NewHandler(cfg.Destination, &console.HandlerOptions{
			Level: slog.Level(cfg.Level),
		})
	case FormatDev:
		opts.AddSource = true
		handler = devslog.NewHandler(cfg.Destination, &devslog.Options{
			HandlerOptions:  &opts,
			NewLineAfterLog: true,
			NoColor:         !bool(cfg.Color),
		})
	// Use discard handler if no format provided
	case "", FormatNone, FormatLegacy:
		handler = slog.NewTextHandler(DestinationNone, &slog.HandlerOptions{})
	// Format not found, let's error out
	default:
		return nil, fmt.Errorf("unsupported log format: %s", cfg.Format)
	}

	return slog.New(handler), nil
}

// ctxKey provides a location to store a logger in a context.
type ctxKey struct{}

// defaultCtxKey provides a key instance to get a logger from context
var defaultCtxKey = ctxKey{}

// WithContext takes a context.Context and a *slog.Logger, storing it on the key
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, defaultCtxKey, logger)
}

// TODO (@austinabro321) once we switch over to the new logger completely the enabled key & logic should be deleted
type ctxKeyEnabled struct{}

var defaultCtxKeyEnabled = ctxKeyEnabled{}

// WithLoggingEnabled allows stores a value to determine whether or not slog logging is enabled
func WithLoggingEnabled(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, defaultCtxKeyEnabled, enabled)
}

// Enabled returns true if slog logging is enabled
func Enabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled := ctx.Value(defaultCtxKeyEnabled)
	switch v := enabled.(type) {
	case bool:
		return v
	default:
		return false
	}
}

// From takes a context and reads out a *slog.Logger. If From does not find a value it will return a discarding logger
// similar to log-format "none".
func From(ctx context.Context) *slog.Logger {
	// Check that we have a ctx
	if ctx == nil {
		return newDiscard()
	}
	// Grab value from key
	log := ctx.Value(defaultCtxKey)
	if log == nil {
		return newDiscard()
	}

	// Ensure our value is a *slog.Logger before we cast.
	switch l := log.(type) {
	case *slog.Logger:
		return l
	default:
		// Not reached
		panic(fmt.Sprintf("unexpected value type on context key: %T", log))
	}
}

// newDiscard returns a logger without any settings that goes to io.Discard
func newDiscard() *slog.Logger {
	h := slog.NewTextHandler(DestinationNone, &slog.HandlerOptions{})
	return slog.New(h)
}

// Default retrieves a logger from the package default. This is intended as a fallback when a logger cannot easily be
// passed in as a dependency, like when developing a new function. Use it like you would use context.TODO().
func Default() *slog.Logger {
	return defaultLogger.Load()
}

// SetDefault takes a logger and atomically stores it as the package default. This is intended to be called when the
// application starts to override the default config with application-specific config. See Default() for more usage
// details.
func SetDefault(l *slog.Logger) {
	defaultLogger.Store(l)
}
