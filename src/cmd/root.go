// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package cmd contains the CLI commands for Zarf.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/zarf-dev/zarf/src/cmd/say"
	"github.com/zarf-dev/zarf/src/pkg/logger"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/zarf-dev/zarf/src/cmd/common"
	"github.com/zarf-dev/zarf/src/cmd/tools"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/message"
	"github.com/zarf-dev/zarf/src/types"
)

var (
	// Default global config for the packager
	pkgConfig = types.PackagerConfig{}
	// LogLevelCLI holds the log level as input from a command
	LogLevelCLI string
	// LogFormat holds the log format as input from a command
	LogFormat string
	// SkipLogFile is a flag to skip logging to a file
	SkipLogFile bool
	// NoColor is a flag to disable colors in output
	NoColor bool
)

var rootCmd = &cobra.Command{
	Use:          "zarf COMMAND",
	Short:        lang.RootCmdShort,
	Long:         lang.RootCmdLong,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	// TODO(mkcp): Do we actually want to silence errors here?
	SilenceErrors:     true,
	PersistentPreRunE: preRun,
	Run:               run,
}

func preRun(cmd *cobra.Command, _ []string) error {
	// If --insecure was provided, set --insecure-skip-tls-verify and --plain-http to match
	if config.CommonOptions.Insecure {
		config.CommonOptions.InsecureSkipTLSVerify = true
		config.CommonOptions.PlainHTTP = true
	}

	// Skip for vendor only commands
	if common.CheckVendorOnlyFromPath(cmd) {
		return nil
	}

	// Setup message
	skipLogFile := SkipLogFile

	// Don't write tool commands to file.
	comps := strings.Split(cmd.CommandPath(), " ")
	if len(comps) > 1 && comps[1] == "tools" {
		skipLogFile = true
	}
	if len(comps) > 1 && comps[1] == "version" {
		skipLogFile = true
	}

	// Dont write help command to file.
	if cmd.Parent() == nil {
		skipLogFile = true
	}

	// Configure logger and add it to cmd context.
	l, err := setupLogger(LogLevelCLI, LogFormat)
	if err != nil {
		return err
	}
	ctx := logger.WithContext(cmd.Context(), l)
	cmd.SetContext(ctx)

	// Configure the global message instance.
	var disableMessage bool
	if LogFormat != "" {
		disableMessage = true
		ctx := logger.WithLoggingEnabled(ctx, true)
		cmd.SetContext(ctx)
	}
	err = SetupMessage(MessageCfg{
		Level:           LogLevelCLI,
		SkipLogFile:     skipLogFile,
		NoColor:         NoColor,
		FeatureDisabled: disableMessage,
	})
	if err != nil {
		return err
	}

	// Print out config location
	err = common.PrintViperConfigUsed(cmd.Context())
	if err != nil {
		return err
	}
	return nil
}

func run(cmd *cobra.Command, _ []string) {
	err := cmd.Help()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}
}

// Execute is the entrypoint for the CLI.
func Execute(ctx context.Context) {
	// Add `zarf say`
	rootCmd.AddCommand(say.Command())

	cmd, err := rootCmd.ExecuteContextC(ctx)
	if err == nil {
		return
	}

	// Check if we need to use the default err printer
	defaultPrintCmds := []string{"helm", "yq", "kubectl"}
	comps := strings.Split(cmd.CommandPath(), " ")
	if len(comps) > 1 && comps[1] == "tools" && slices.Contains(defaultPrintCmds, comps[2]) {
		cmd.PrintErrln(cmd.ErrPrefix(), err.Error())
		os.Exit(1)
	}

	// TODO(mkcp): Remove message on logger release
	errParagraph := message.Paragraph(err.Error())
	pterm.Error.Println(errParagraph)

	// NOTE(mkcp): The default logger is set with user flags downstream in rootCmd's preRun func, so we don't have
	// access to it on Execute's ctx.
	logger.Default().Error(err.Error())
	os.Exit(1)
}

func init() {
	// Add the tools commands
	tools.Include(rootCmd)

	// Skip for vendor-only commands
	if common.CheckVendorOnlyFromArgs() {
		return
	}

	v := common.InitViper()

	// Logs
	rootCmd.PersistentFlags().StringVarP(&LogLevelCLI, "log-level", "l", v.GetString(common.VLogLevel), lang.RootCmdFlagLogLevel)
	rootCmd.PersistentFlags().StringVar(&LogFormat, "log-format", v.GetString(common.VLogFormat), "Select a logging format. Defaults to 'text'. Valid options are: 'text', 'json'")
	rootCmd.PersistentFlags().BoolVar(&SkipLogFile, "no-log-file", v.GetBool(common.VNoLogFile), lang.RootCmdFlagSkipLogFile)
	rootCmd.PersistentFlags().BoolVar(&message.NoProgress, "no-progress", v.GetBool(common.VNoProgress), lang.RootCmdFlagNoProgress)
	rootCmd.PersistentFlags().BoolVar(&NoColor, "no-color", v.GetBool(common.VNoColor), lang.RootCmdFlagNoColor)

	rootCmd.PersistentFlags().StringVarP(&config.CLIArch, "architecture", "a", v.GetString(common.VArchitecture), lang.RootCmdFlagArch)
	rootCmd.PersistentFlags().StringVar(&config.CommonOptions.CachePath, "zarf-cache", v.GetString(common.VZarfCache), lang.RootCmdFlagCachePath)
	rootCmd.PersistentFlags().StringVar(&config.CommonOptions.TempDirectory, "tmpdir", v.GetString(common.VTmpDir), lang.RootCmdFlagTempDir)

	// Security
	rootCmd.PersistentFlags().BoolVar(&config.CommonOptions.Insecure, "insecure", v.GetBool(common.VInsecure), lang.RootCmdFlagInsecure)
	rootCmd.PersistentFlags().MarkDeprecated("insecure", "please use --plain-http, --insecure-skip-tls-verify, or --skip-signature-validation instead.")
	rootCmd.PersistentFlags().BoolVar(&config.CommonOptions.PlainHTTP, "plain-http", v.GetBool(common.VPlainHTTP), lang.RootCmdFlagPlainHTTP)
	rootCmd.PersistentFlags().BoolVar(&config.CommonOptions.InsecureSkipTLSVerify, "insecure-skip-tls-verify", v.GetBool(common.VInsecureSkipTLSVerify), lang.RootCmdFlagInsecureSkipTLSVerify)

	// HACK(mkcp): This is a workaround for us testing that help output matches to the byte. Undo this and update tests
	// before release.
	rootCmd.PersistentFlags().MarkHidden("log-format")
}

// setup Logger handles creating a logger and setting it as the global default.
func setupLogger(level, format string) (*slog.Logger, error) {
	// If we didn't get a level from config, fallback to "info"
	if level == "" {
		level = "info"
	}
	sLevel, err := logger.ParseLevel(level)
	if err != nil {
		return nil, err
	}
	cfg := logger.Config{
		Level:       sLevel,
		Format:      logger.Format(format),
		Destination: logger.DestinationDefault,
	}
	l, err := logger.New(cfg)
	if err != nil {
		return nil, err
	}
	logger.SetDefault(l)
	l.Debug("logger successfully initialized", "cfg", cfg)
	return l, nil
}

// MessageCfg is used to configure the Message package output options.
type MessageCfg struct {
	Level       string
	SkipLogFile bool
	NoColor     bool
	// FeatureDisabled is a feature flag that disables it
	FeatureDisabled bool
}

// SetupMessage configures message while we migrate over to logger.
func SetupMessage(cfg MessageCfg) error {
	// HACK(mkcp): Discard message logs if feature is disabled. message calls InitializePTerm once in its init() fn so
	//             this ends up being a messy solution.
	if cfg.FeatureDisabled {
		// Discard all* PTerm messages. *see below
		message.InitializePTerm(io.Discard)
		// Disable all progress bars and spinners
		message.NoProgress = true
		return nil
	}

	if cfg.NoColor {
		message.DisableColor()
	}

	level := cfg.Level
	if cfg.Level != "" {
		match := map[string]message.LogLevel{
			// NOTE(mkcp): Add error for forwards compatibility with logger
			"error": message.WarnLevel,
			"warn":  message.WarnLevel,
			"info":  message.InfoLevel,
			"debug": message.DebugLevel,
			"trace": message.TraceLevel,
		}
		lvl, ok := match[level]
		if !ok {
			return errors.New("invalid log level, valid options are warn, info, debug, error, and trace")
		}
		message.SetLogLevel(lvl)
		message.Debug("Log level set to " + level)
	}

	// Disable progress bars for CI envs
	if os.Getenv("CI") == "true" {
		message.Debug("CI environment detected, disabling progress bars")
		message.NoProgress = true
	}

	if !cfg.SkipLogFile {
		ts := time.Now().Format("2006-01-02-15-04-05")
		f, err := os.CreateTemp("", fmt.Sprintf("zarf-%s-*.log", ts))
		if err != nil {
			return fmt.Errorf("could not create a log file in a the temporary directory: %w", err)
		}
		logFile, err := message.UseLogFile(f)
		if err != nil {
			return fmt.Errorf("could not save a log file to the temporary directory: %w", err)
		}
		pterm.SetDefaultOutput(io.MultiWriter(os.Stderr, logFile))
		message.Notef("Saving log file to %s", f.Name())
	}
	return nil
}
