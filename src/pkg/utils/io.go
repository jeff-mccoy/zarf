// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package utils provides generic helper functions.
package utils

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/defenseunicorns/pkg/helpers/v2"
	"github.com/zarf-dev/zarf/src/config"
)

const (
	tmpPathPrefix = "zarf-"
)

// MakeTempDir creates a temp directory with the zarf- prefix.
func MakeTempDir(basePath string) (string, error) {
	if basePath != "" {
		if err := helpers.CreateDirectory(basePath, helpers.ReadWriteExecuteUser); err != nil {
			return "", err
		}
	}
	tmp, err := os.MkdirTemp(basePath, tmpPathPrefix)
	if err != nil {
		return "", err
	}
	return tmp, nil
}

// GetFinalExecutablePath returns the absolute path to the current executable, following any symlinks along the way.
func GetFinalExecutablePath() (string, error) {
	binaryPath, err := os.Executable()
	if err != nil {
		return "", err
	}

	// In case the binary is symlinked somewhere else, get the final destination
	linkedPath, err := filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return "", err
	}
	return linkedPath, nil
}

// GetFinalExecutableCommand returns the final path to the Zarf executable including and library prefixes and overrides.
func GetFinalExecutableCommand() (string, error) {
	// In case the binary is symlinked somewhere else, get the final destination
	zarfCommand, err := GetFinalExecutablePath()
	if err != nil {
		return "", err
	}

	if config.ActionsCommandZarfPrefix != "" {
		zarfCommand = fmt.Sprintf("%s %s", zarfCommand, config.ActionsCommandZarfPrefix)
	}

	// If a library user has chosen to override config to use system Zarf instead, reset the binary path.
	if config.ActionsUseSystemZarf {
		zarfCommand = "zarf"
	}

	return zarfCommand, nil
}
