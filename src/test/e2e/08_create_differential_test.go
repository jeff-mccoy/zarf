// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package test provides e2e tests for Zarf.
package test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mholt/archiver/v3"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/api/v1alpha1"
	"github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/layout"
	"github.com/zarf-dev/zarf/src/pkg/utils"
)

// TestCreateDifferential creates several differential packages and ensures the reference package images and repos are not included in the new package.
func TestCreateDifferential(t *testing.T) {
	t.Log("E2E: Test Differential Package Behavior")
	tmpdir := t.TempDir()

	packagePath := "src/test/packages/08-differential-package"
	packageName := fmt.Sprintf("zarf-package-differential-package-%s-v0.25.0.tar.zst", e2e.Arch)
	differentialPackageName := fmt.Sprintf("zarf-package-differential-package-%s-v0.25.0-differential-v0.26.0.tar.zst", e2e.Arch)
	differentialFlag := fmt.Sprintf("--differential=%s", packageName)

	// Build the package a first time
	stdOut, stdErr, err := e2e.Zarf(t, "package", "create", packagePath, "--set=PACKAGE_VERSION=v0.25.0", "--confirm")
	require.NoError(t, err, stdOut, stdErr)
	defer e2e.CleanFiles(t, packageName)

	// Build the differential package without changing the version
	_, stdErr, err = e2e.Zarf(t, "package", "create", packagePath, "--set=PACKAGE_VERSION=v0.25.0", differentialFlag, "--confirm")
	require.Error(t, err, "zarf package create should have errored when a differential package was being created without updating the package version number")
	require.Contains(t, e2e.StripMessageFormatting(stdErr), lang.PkgCreateErrDifferentialSameVersion)

	// Build the differential package
	stdOut, stdErr, err = e2e.Zarf(t, "package", "create", packagePath, "--set=PACKAGE_VERSION=v0.26.0", differentialFlag, "--confirm")
	require.NoError(t, err, stdOut, stdErr)
	defer e2e.CleanFiles(t, differentialPackageName)

	// Extract the yaml of the differential package
	err = archiver.Extract(differentialPackageName, layout.ZarfYAML, tmpdir)
	require.NoError(t, err, "unable to extract zarf.yaml from the differential git package")

	// Load the extracted zarf.yaml specification
	var differentialZarfConfig v1alpha1.ZarfPackage
	err = utils.ReadYaml(filepath.Join(tmpdir, layout.ZarfYAML), &differentialZarfConfig)
	require.NoError(t, err, "unable to read zarf.yaml from the differential git package")

	// Get a list of all images and repos that are inside of the differential package
	actualGitRepos := []string{}
	actualImages := []string{}
	for _, component := range differentialZarfConfig.Components {
		actualGitRepos = append(actualGitRepos, component.Repos...)
		actualImages = append(actualImages, component.Images...)
	}

	/* Validate we have ONLY the git repos we expect to have */
	expectedGitRepos := []string{
		"https://github.com/stefanprodan/podinfo.git",
		"https://github.com/kelseyhightower/nocode.git",
		"https://github.com/zarf-dev/zarf.git@refs/tags/v0.26.0",
	}
	require.Len(t, actualGitRepos, 4, "zarf.yaml from the differential package does not contain the correct number of repos")
	for _, expectedRepo := range expectedGitRepos {
		require.Contains(t, actualGitRepos, expectedRepo, "unable to find expected repo %s", expectedRepo)
	}

	/* Validate we have ONLY the images we expect to have */
	expectedImages := []string{
		"ghcr.io/stefanprodan/podinfo:latest",
		"ghcr.io/zarf-dev/zarf/agent:v0.26.0",
	}
	require.Len(t, actualImages, 2, "zarf.yaml from the differential package does not contain the correct number of images")
	for _, expectedImage := range expectedImages {
		require.Contains(t, actualImages, expectedImage, "unable to find expected image %s", expectedImage)
	}
}
