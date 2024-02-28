// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package oci contains functions for interacting with artifacts stored in OCI registries.
package oci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory" // used for docker test registry
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
)

type OCISuite struct {
	suite.Suite
	*require.Assertions
	remote      *OrasRemote
	registryURL string
}

func (suite *OCISuite) SetupSuite() {
	suite.Assertions = require.New(suite.T())
	suite.StartRegistry()

	platform := PlatformForArch("fake-package-so-does-not-matter")
	var err error
	suite.remote, err = NewOrasRemote(suite.registryURL, platform, WithPlainHTTP(true))
	suite.NoError(err)

}

func (suite *OCISuite) StartRegistry() {
	// Registry config
	ctx := context.TODO()
	config := &configuration.Configuration{}
	port, err := freeport.GetFreePort()
	suite.NoError(err)

	config.HTTP.Addr = fmt.Sprintf(":%d", port)
	config.HTTP.DrainTimeout = 10 * time.Second
	config.Storage = map[string]configuration.Parameters{"inmemory": map[string]interface{}{}}

	ref, err := registry.NewRegistry(ctx, config)
	suite.NoError(err)

	go ref.ListenAndServe()

	suite.registryURL = fmt.Sprintf("oci://localhost:%d/package:1.0.1", port)
}

func (suite *OCISuite) TestBadRemote() {
	suite.T().Log("Here")
	_, err := NewOrasRemote("nonsense", PlatformForArch("fake-package-so-does-not-matter"))
	suite.Error(err)
}

func (suite *OCISuite) TestPublishFailNoTitle() {
	suite.T().Log("")

	ctx := context.TODO()
	annotations := map[string]string{
		ocispec.AnnotationDescription: "No title",
	}
	_, err := suite.remote.CreateAndPushManifestConfig(ctx, annotations, ocispec.MediaTypeImageConfig)
	suite.Error(err)
}

func (suite *OCISuite) TestPublishSuccess() {
	suite.T().Log("")

	ctx := context.TODO()
	annotations := map[string]string{
		ocispec.AnnotationTitle:       "name",
		ocispec.AnnotationDescription: "description",
	}

	_, err := suite.remote.CreateAndPushManifestConfig(ctx, annotations, ocispec.MediaTypeImageConfig)
	suite.NoError(err)

}

func (suite *OCISuite) TestPublishForReal() {
	suite.T().Log("")

	ctx := context.TODO()

	annotations := map[string]string{
		ocispec.AnnotationTitle:       "name",
		ocispec.AnnotationDescription: "description",
	}

	manifestConfigDesc, err := suite.remote.CreateAndPushManifestConfig(ctx, annotations, ocispec.MediaTypeLayoutHeader)
	suite.NoError(err)

	fileContents := "here's what I'm putting in the file"

	tempDir := suite.T().TempDir()
	regularFileName := "i-am-what-i-am"
	regularFilePath := filepath.Join(tempDir, regularFileName)
	os.WriteFile(regularFilePath, []byte(fileContents), 0644)
	src, err := file.New(tempDir)
	suite.NoError(err)

	// I want to test that I am able to get a file by it's oci file name and see the contents

	ociFileName := "small-file"
	desc, err := src.Add(ctx, ociFileName, ocispec.MediaTypeEmptyJSON, regularFilePath)
	suite.NoError(err)
	descs := []ocispec.Descriptor{desc}
	manifestDesc, err := suite.remote.PackAndTagManifest(ctx, src, descs, manifestConfigDesc, annotations)
	suite.NoError(err)
	publishedDesc, err := oras.Copy(ctx, src, manifestDesc.Digest.String(), suite.remote.Repo(), "", suite.remote.GetDefaultCopyOpts())
	suite.NoError(err)
	fmt.Printf("manifest descriptor %s", publishedDesc.Digest.String())

	err = suite.remote.UpdateIndex(ctx, "0.0.1", manifestDesc)
	suite.NoError(err)

	otherTempDir := suite.T().TempDir()
	dst, err := file.New(otherTempDir)

	suite.NoError(err)
	err = suite.remote.CopyToTarget(ctx, descs, dst, suite.remote.GetDefaultCopyOpts())
	suite.NoError(err)
	ociFile := filepath.Join(otherTempDir, ociFileName)
	b, _ := os.ReadFile(ociFile)
	contents := string(b)
	suite.Equal(contents, fileContents)
}

func TestOCI(t *testing.T) {
	suite.Run(t, new(OCISuite))
}
