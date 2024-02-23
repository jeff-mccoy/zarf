// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package test provides e2e tests for Zarf.
package test

import (
	"testing"

	"github.com/defenseunicorns/zarf/src/pkg/utils"
	"github.com/defenseunicorns/zarf/src/types"
	"github.com/stretchr/testify/require"
)

func TestZarfDevGenerate(t *testing.T) {
	t.Log("E2E: Zarf Dev Generate")

	t.Run("Test arguments and flags", func(t *testing.T) {
		stdOut, stdErr, err := e2e.Zarf("dev", "generate")
		require.Error(t, err, stdOut, stdErr)

		stdOut, stdErr, err = e2e.Zarf("dev", "generate", "podinfo")
		require.Error(t, err, stdOut, stdErr)

		stdOut, stdErr, err = e2e.Zarf("dev", "generate", "podinfo", "--url", "https://zarf.dev")
		require.Error(t, err, stdOut, stdErr)
	})

	t.Run("Test generate podinfo", func(t *testing.T) {
		tmpDir := t.TempDir()

		url := "https://github.com/stefanprodan/podinfo.git"
		version := "6.4.0"
		gitPath := "charts/podinfo"

		stdOut, stdErr, err := e2e.Zarf("dev", "generate", "podinfo", "--url", url, "--version", version, "--gitPath", gitPath, "--output-directory", tmpDir)
		require.NoError(t, err, stdOut, stdErr)

		zarfPackage := types.ZarfPackage{}
		err = utils.ReadYaml(tmpDir+"/zarf.yaml", &zarfPackage)
		require.NoError(t, err)
		require.Equal(t, zarfPackage.Components[0].Charts[0].URL, url)
		require.Equal(t, zarfPackage.Components[0].Charts[0].Version, version)
		require.Equal(t, zarfPackage.Components[0].Charts[0].GitPath, gitPath)
	})
}
