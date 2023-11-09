// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package packager contains functions for interacting with, managing and deploying Zarf packages.
package packager

import (
	"os"
	"testing"

	"github.com/defenseunicorns/zarf/src/types"
	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestValidateSchema(t *testing.T) {
	readFileFailFatally := func(t *testing.T, path string) []byte {
		file, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("error reading file: %s", err)
		}
		return file
	}

	readSchema := func(t *testing.T) []byte {
		t.Helper()
		return readFileFailFatally(t, "../../../zarf.schema.json")
	}

	readAndUnmarshalYaml := func(t *testing.T, path string) interface{} {
		t.Helper()
		var unmarshalledYaml interface{}
		file := readFileFailFatally(t, path)
		err := goyaml.Unmarshal(file, &unmarshalledYaml)
		if err != nil {
			t.Errorf("error unmarshalling yaml %s", err)
		}
		return unmarshalledYaml
	}

	readAndUnmarshallZarfPackage := func(t *testing.T, path string) types.ZarfPackage {
		t.Helper()
		var unmarshalledYaml types.ZarfPackage
		file := readFileFailFatally(t, path)
		err := goyaml.Unmarshal(file, &unmarshalledYaml)
		if err != nil {
			t.Errorf("error unmarshalling yaml %s", err)
		}
		return unmarshalledYaml
	}

	t.Run("validate schema success", func(t *testing.T) {
		unmarshalledYaml := readAndUnmarshalYaml(t, "../../../zarf.yaml")
		zarfSchema := readSchema(t)
		err := validateSchema(unmarshalledYaml, zarfSchema)
		require.NoError(t, err)
	})

	t.Run("validate schema fail", func(t *testing.T) {
		unmarshalledYaml := readAndUnmarshalYaml(t, "../../test/packages/12-lint/zarf.yaml")
		zarfSchema := readSchema(t)
		err := validateSchema(unmarshalledYaml, zarfSchema)
		errorMessage := zarfInvalidPrefix + `
 - /components/0/import: additionalProperties 'not-path' not allowed
 - /components/1/import/path: expected string, but got number`
		require.EqualError(t, err, errorMessage)
	})

	t.Run("Template in component import success", func(t *testing.T) {
		unmarshalledYaml := readAndUnmarshallZarfPackage(t, "../../../zarf.yaml")
		err := checkForVarInComponentImport(unmarshalledYaml)
		require.NoError(t, err)
	})

	t.Run("Template in component import failure", func(t *testing.T) {
		unmarshalledYaml := readAndUnmarshallZarfPackage(t, "../../test/packages/12-lint/zarf.yaml")
		err := checkForVarInComponentImport(unmarshalledYaml)
		errorMessage := zarfWarningPrefix + " component/2/import/path will not resolve ZARF_PKG_TMPL_* variables. " +
			"component/3/import/url will not resolve ZARF_PKG_TMPL_* variables."
		require.EqualError(t, err, errorMessage)
	})
}
