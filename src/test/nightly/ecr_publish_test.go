// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package test provides e2e tests for Zarf.
package test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/test"
)

var (
	e2e test.ZarfE2ETest //nolint:gochecknoglobals
)

// TestECRPublishing tests pushing, inspecting, and pulling signed Zarf packages to/from ECR.
/*
This test assumes the following:
  1. The user running the test has a current valid credential to the public.ecr.aws/t8y5r5z5/zarf-nightly repository in their docker config.json
  2. The zarf CLI has been built and is available in the build directory
*/
func TestECRPublishing(t *testing.T) {
	t.Log("E2E: Testing component actions")

	// Work from the root directory of the project
	err := os.Chdir("../../../")
	require.NoError(t, err)

	// Create a tmpDir for us to use during this test
	tmpDir := t.TempDir()

	// Set up the e2e configs
	e2e.Arch = config.GetArch()
	e2e.ZarfBinPath = path.Join("build", test.GetCLIName())

	// Set up variables for common names/locations
	testPackageName := "helm-charts"
	testPackageVersion := "0.0.1"
	testPackageFileName := fmt.Sprintf("zarf-package-%s-%s-%s.tar.zst", testPackageName, e2e.Arch, testPackageVersion)
	testPackageLocation := filepath.Join(tmpDir, testPackageFileName)
	registryURL := "oci://public.ecr.aws/k2w9d2q6/zarf-nightly"
	upstreamPackageURL := fmt.Sprintf("%s/%s:%s", registryURL, testPackageName, testPackageVersion)
	keyFlag := fmt.Sprintf("--key=%s", "./src/test/packages/zarf-test.pub")

	// Build the package with our test signature
	stdOut, stdErr, err := e2e.Zarf(t, "package", "create", "examples/helm-charts", "--signing-key=./src/test/packages/zarf-test.prv-key", "--confirm", fmt.Sprintf("-o=%s", tmpDir))
	require.NoError(t, err, stdOut, stdErr)
	require.FileExists(t, testPackageLocation)

	// Validate that we can publish the package to ECR without an issue
	stdOut, stdErr, err = e2e.Zarf(t, "package", "publish", testPackageLocation, registryURL, keyFlag)
	require.NoError(t, err, stdOut, stdErr)

	// Ensure we get a warning when trying to inspect the online published package
	stdOut, stdErr, err = e2e.Zarf(t, "package", "inspect", upstreamPackageURL, keyFlag, "--sbom-out", tmpDir, "--skip-signature-validation")
	require.NoError(t, err, stdOut, stdErr)

	// Validate that we can pull the package down from ECR
	pullTempDir := t.TempDir()
	stdOut, stdErr, err = e2e.Zarf(t, "package", "pull", upstreamPackageURL, keyFlag, fmt.Sprintf("-o=%s", pullTempDir))
	require.NoError(t, err, stdOut, stdErr)

	pulledPackagePath := filepath.Join(pullTempDir, testPackageFileName)

	stdOut, stdErr, err = e2e.Zarf(t, "package", "inspect", pulledPackagePath, "--skip-signature-validation")
	require.NoError(t, err, stdOut, stdErr)

	stdOut, stdErr, err = e2e.Zarf(t, "package", "inspect", pulledPackagePath, keyFlag)
	require.NoError(t, err, stdOut, stdErr)
}
