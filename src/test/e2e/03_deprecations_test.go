// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package test provides e2e tests for Zarf.
package test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	goyaml "github.com/goccy/go-yaml"
	"github.com/otiai10/copy"
	"github.com/stretchr/testify/require"

	layout2 "github.com/zarf-dev/zarf/src/internal/packager2/layout"
)

// TestDeprecatedComponentScripts verifies that deprecated component scripts are still able to be executed after being internally migrated into zarf actions.
func TestDeprecatedComponentScripts(t *testing.T) {
	t.Parallel()

	deployArtifacts := []string{
		"test-deprecated-deploy-before-hook.txt",
		"test-deprecated-deploy-after-hook.txt",
	}

	packagePath := t.TempDir()
	err := copy.Copy("src/test/packages/03-deprecated-component-scripts", packagePath)
	require.NoError(t, err)

	workingDirPath := t.TempDir()
	tarName := fmt.Sprintf("zarf-package-deprecated-component-scripts-%s.tar.zst", e2e.Arch)

	// Try creating the package to test the create scripts
	_, _, err = e2e.ZarfInDir(t, workingDirPath, "package", "create", packagePath, "--confirm")
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(packagePath, "test-deprecated-prepare-hook.txt"))
	for _, artifact := range deployArtifacts {
		require.NoFileExists(t, filepath.Join(workingDirPath, artifact))
	}

	// Deploy the simple script that should pass
	_, _, err = e2e.ZarfInDir(t, workingDirPath, "package", "deploy", tarName, "--confirm", "--components=2-test-deprecated-deploy-scripts")
	require.NoError(t, err)

	for _, artifact := range deployArtifacts {
		require.FileExists(t, filepath.Join(workingDirPath, artifact))
	}

	// Deploy the simple script that should fail the timeout
	_, _, err = e2e.ZarfInDir(t, workingDirPath, "package", "deploy", tarName, "--confirm", "--components=3-test-deprecated-timeout-scripts")
	require.Error(t, err)
}

// TestDeprecatedSetAndPackageVariables verifies that deprecated setVariables and PKG_VARs still able to be set.
func TestDeprecatedSetAndPackageVariables(t *testing.T) {
	t.Parallel()

	// Note prepare script files will be created in the package directory, not CWD
	testPackageDirPath := "src/test/packages/03-deprecated-set-variable"

	outPath := t.TempDir()
	tarPath := filepath.Join(outPath, fmt.Sprintf("zarf-package-deprecated-set-variable-%s.tar.zst", e2e.Arch))

	// Check that the command still errors out
	_, _, err := e2e.Zarf(t, "package", "create", testPackageDirPath, "-o", outPath, "--confirm")
	require.Error(t, err)

	// // Check that the command displays a warning on create
	_, _, err = e2e.Zarf(t, "package", "create", testPackageDirPath, "-o", outPath, "--confirm", "--set", "ECHO=Zarf-The-Axolotl")
	require.NoError(t, err)

	pkgLayout, err := layout2.LoadFromTar(context.Background(), tarPath, layout2.PackageLayoutOptions{})
	require.NoError(t, err)
	b, err := goyaml.Marshal(pkgLayout.Pkg.Components)
	require.NoError(t, err)
	expectedYaml := `- name: 1-test-deprecated-set-variable
  actions:
    onDeploy:
      before:
      - cmd: echo "Hello Kitteh"
        setVariables:
        - name: HELLO_KITTEH
      - cmd: echo "Hello from ${ZARF_VAR_HELLO_KITTEH}"
- name: 2-test-deprecated-pkg-var
  actions:
    onDeploy:
      before:
      - cmd: echo "Zarf-The-Axolotl"
`
	require.YAMLEq(t, expectedYaml, string(b))
}
