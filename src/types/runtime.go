// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package types contains all the types used by Zarf.
package types

import (
	"time"
)

// ZarfCommonOptions tracks the user-defined preferences used across commands.
type ZarfCommonOptions struct {
	// Verify that Zarf should perform an action
	Confirm bool
	// Allow insecure connections for remote packages
	Insecure bool
	// Disable checking the server TLS certificate for validity
	InsecureSkipTLSVerify bool
	// Force connections to be over http instead of https
	PlainHTTP bool
	// Path to use to cache images and git repos on package create
	CachePath string
	// Location Zarf should use as a staging ground when managing files and images for package creation and deployment
	TempDirectory string
	// Number of concurrent layer operations to perform when interacting with a remote package
	OCIConcurrency int
}

// ZarfPackageOptions tracks the user-defined preferences during common package operations.
type ZarfPackageOptions struct {
	// The SHA256 checksum of the package
	Shasum string
	// Location where a Zarf package can be found
	PackageSource string
	// Comma separated list of optional components
	OptionalComponents string
	// Location where the public key component of a cosign key-pair can be found
	SGetKeyPath string
	// Key-Value map of variable names and their corresponding values that will be used to template manifests and files in the Zarf package
	SetVariables map[string]string
	// Location where the public key component of a cosign key-pair can be found
	PublicKeyPath string
	// The number of retries to perform for Zarf deploy operations like image pushes or Helm installs
	Retries int
	// Skip validating the signature of the Zarf package
	SkipSignatureValidation bool
}

// ZarfInspectOptions tracks the user-defined preferences during a package inspection.
type ZarfInspectOptions struct {
	// View SBOM contents while inspecting the package
	ViewSBOM bool
	// Location to output an SBOM into after package inspection
	SBOMOutputDir string
	// ListImages will list the images in the package
	ListImages bool
}

// ZarfFindImagesOptions tracks the user-defined preferences during a prepare find-images search.
type ZarfFindImagesOptions struct {
	// Path to the helm chart directory
	RepoHelmChartPath string
	// Kubernetes version to use for the helm chart
	KubeVersionOverride string
	// Manual override for ###ZARF_REGISTRY###
	RegistryURL string
	// Find the location of the image given as an argument and print it to the console
	Why string
	// Optionally skip lookup of cosign artifacts when finding images
	SkipCosign bool
	// Update the images in zarf.yaml in place
	UpdatePackageDefinition bool
}

// ZarfDeployOptions tracks the user-defined preferences during a package deploy.
type ZarfDeployOptions struct {
	// Whether to adopt any pre-existing K8s resources into the Helm charts managed by Zarf
	AdoptExistingResources bool
	// Timeout for performing Helm operations
	Timeout time.Duration
	// [Library Only] A map of component names to chart names containing Helm Chart values to override values on deploy
	ValuesOverridesMap map[string]map[string]map[string]interface{}
	// [Dev Deploy Only] Manual override for ###ZARF_REGISTRY###
	RegistryURL string
}

// ZarfMirrorOptions tracks the user-defined preferences during a package mirror.
type ZarfMirrorOptions struct {
	// Whether to skip adding a Zarf checksum to image references
	NoImgChecksum bool
}

// ZarfPublishOptions tracks the user-defined preferences during a package publish.
type ZarfPublishOptions struct {
	// Location where the Zarf package will be published to
	PackageDestination string
	// Password to the private key signature file that will be used to sign the published package
	SigningKeyPassword string
	// Location where the private key component of a cosign key-pair can be found
	SigningKeyPath string
}

// ZarfPullOptions tracks the user-defined preferences during a package pull.
type ZarfPullOptions struct {
	// Location where the pulled Zarf package will be placed
	OutputDirectory string
}

// ZarfGenerateOptions tracks the user-defined options during package generation.
type ZarfGenerateOptions struct {
	// Name of the package being generated
	Name string
	// URL to the source git repository
	URL string
	// Version of the chart to use
	Version string
	// Relative path to the chart in the git repository
	GitPath string
	// Location where the finalized zarf.yaml will be placed
	Output string
}

// ZarfInitOptions tracks the user-defined options during cluster initialization.
type ZarfInitOptions struct {
	// Indicates if Zarf was initialized while deploying its own k8s cluster
	ApplianceMode bool
	// Information about the repository Zarf is going to be using
	GitServer GitServerInfo
	// Information about the container registry Zarf is going to be using
	RegistryInfo RegistryInfo
	// Information about the artifact registry Zarf is going to be using
	ArtifactServer ArtifactServerInfo
	// StorageClass of the k8s cluster Zarf is initializing
	StorageClass string
}

// ZarfCreateOptions tracks the user-defined options used to create the package.
type ZarfCreateOptions struct {
	// Disable the generation of SBOM materials during package creation
	SkipSBOM bool
	// Location where the Zarf package will be created from
	BaseDir string
	// Location where the finalized Zarf package will be placed
	Output string
	// Whether to pause to allow for viewing the SBOM post-creation
	ViewSBOM bool
	// Location to output an SBOM into after package creation
	SBOMOutputDir string
	// Key-Value map of variable names and their corresponding values that will be used to template against the Zarf package being used
	SetVariables map[string]string
	// Size of chunks to use when splitting a zarf package into multiple files in megabytes
	MaxPackageSizeMB int
	// Location where the private key component of a cosign key-pair can be found
	SigningKeyPath string
	// Password to the private key signature file that will be used to sigh the created package
	SigningKeyPassword string
	// Path to a previously built package used as the basis for creating a differential package
	DifferentialPackagePath string
	// A map of domains to override on package create when pulling images
	RegistryOverrides map[string]string
	// An optional variant that controls which components will be included in a package
	Flavor string
	// Whether to create a skeleton package
	IsSkeleton bool
	// Whether to create a YOLO package
	NoYOLO bool
}

// ZarfSplitPackageData contains info about a split package.
type ZarfSplitPackageData struct {
	// The sha256sum of the package
	Sha256Sum string
	// The size of the package in bytes
	Bytes int64
	// The number of parts the package is split into
	Count int
}

// DifferentialData contains image and repository information about the package a Differential Package is Based on.
type DifferentialData struct {
	DifferentialImages         map[string]bool
	DifferentialRepos          map[string]bool
	DifferentialPackageVersion string
}
