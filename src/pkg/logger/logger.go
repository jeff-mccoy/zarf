// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package logger implements a log/slog based logger in Zarf.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Level declares each supported log level. These are 1:1 what log/slog supports by default. Info is the default level.
type Level int

// Store names for Levels
var (
	Debug = Level(slog.LevelDebug) // -4
	Info  = Level(slog.LevelInfo)  // 0
	Warn  = Level(slog.LevelWarn)  // 4
	Error = Level(slog.LevelError) // 8
)

// validLevels is a set that provides an ergonomic way to check if a level is a member of the set.
var validLevels = map[Level]bool{
	Debug: true,
	Info:  true,
	Warn:  true,
	Error: true,
}

// strLevels maps a string to its Level.
var strLevels = map[string]Level{
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

// Format declares the kind of logging handler to use. An empty Format defaults to text.
type Format string

// ToLower takes a Format string and converts it to lowercase for case-agnostic validation. Users shouldn't have to care
// about "json" vs. "JSON" for example - they should both work.
func (f Format) ToLower() Format {
	return Format(strings.ToLower(string(f)))
}

// TODO(mkcp): Add dev format
var (
	// FormatText uses the standard slog TextHandler
	FormatText Format = "text"
	// FormatJSON uses the standard slog JSONHandler
	FormatJSON Format = "json"
	// FormatNone sends log writes to DestinationNone / io.Discard
	FormatNone Format = "none"
)

// More printers would be great, like dev format https://github.com/golang-cz/devslog
// and a pretty console slog https://github.com/phsym/console-slog

// Destination declares an io.Writer to send logs to.
type Destination io.Writer

var (
	// DestinationDefault points to Stderr
	DestinationDefault Destination = os.Stderr
	// DestinationNone discards logs as they are received
	DestinationNone Destination = io.Discard
)

// Config is configuration for a logger.
type Config struct {
	// Level sets the log level. An empty value corresponds to Info aka 0.
	Level
	Format
	Destination
}

// ConfigDefault returns a Config with defaults like Text formatting at Info level writing to Stderr.
func ConfigDefault() Config {
	return Config{
		Level:       Info,
		Format:      FormatText,
		Destination: DestinationDefault, // Stderr
	}
}

// New takes a Config and returns a validated logger.
func New(cfg Config) (*slog.Logger, error) {
	var handler slog.Handler
	opts := slog.HandlerOptions{}

	// Use default destination if none
	if cfg.Destination == nil {
		cfg.Destination = DestinationDefault
	}

	// Check that we have a valid log level.
	if !validLevels[cfg.Level] {
		return nil, fmt.Errorf("unsupported log level: %d", cfg.Level)
	}
	opts.Level = slog.Level(cfg.Level)

	switch cfg.Format.ToLower() {
	// Use Text handler if no format provided
	case "", FormatText:
		handler = slog.NewTextHandler(cfg.Destination, &opts)
	case FormatJSON:
		handler = slog.NewJSONHandler(cfg.Destination, &opts)
	// TODO(mkcp): Add dev format
	// case FormatDev:
	// 	handler = slog.NewTextHandler(DestinationNone, &slog.HandlerOptions{
	//		AddSource: true,
	//	})
	case FormatNone:
		handler = slog.NewTextHandler(DestinationNone, &slog.HandlerOptions{})
	// Format not found, let's error out
	default:
		return nil, fmt.Errorf("unsupported log format: %s", cfg.Format)
	}

	log := slog.New(handler)
	return log, nil
}

// Default gets a logger from the atomic slog default.
func Default() *slog.Logger {
	return slog.Default()
}

// SetDefault takes a logger and sets it as the atomic slog default.
func SetDefault(l *slog.Logger) {
	slog.SetDefault(l)
}
