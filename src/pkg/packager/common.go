// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package packager contains functions for interacting with, managing and deploying Zarf packages.
package packager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/defenseunicorns/zarf/src/config/lang"
	"github.com/defenseunicorns/zarf/src/internal/cluster"
	"github.com/defenseunicorns/zarf/src/internal/packager/template"
	"github.com/defenseunicorns/zarf/src/types"
	"github.com/mholt/archiver/v3"

	"github.com/defenseunicorns/zarf/src/config"
	"github.com/defenseunicorns/zarf/src/pkg/interactive"
	"github.com/defenseunicorns/zarf/src/pkg/message"
	"github.com/defenseunicorns/zarf/src/pkg/oci"
	"github.com/defenseunicorns/zarf/src/pkg/utils"
)

// Packager is the main struct for managing packages.
type Packager struct {
	cfg            *types.PackagerConfig
	cluster        *cluster.Cluster
	remote         *oci.OrasRemote
	tmp            types.TempPaths
	arch           string
	warnings       []string
	valueTemplate  *template.Values
	hpaModified    bool
	connectStrings types.ConnectStrings
	provider       types.PackageProvider
}

// Zarf Packager Variables.
var (
	// Find zarf-packages on the local system (https://regex101.com/r/TUUftK/1)
	ZarfPackagePattern = regexp.MustCompile(`zarf-package[^\s\\\/]*\.tar(\.zst)?$`)

	// Find zarf-init packages on the local system
	ZarfInitPattern = regexp.MustCompile(GetInitPackageName("") + "$")
)

/*
New creates a new package instance with the provided config.

Note: This function creates a tmp directory that should be cleaned up with p.ClearTempPaths().
*/
func New(cfg *types.PackagerConfig) (*Packager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no config provided")
	}

	if cfg.SetVariableMap == nil {
		cfg.SetVariableMap = make(map[string]*types.ZarfSetVariable)
	}

	if cfg.Pkg.Build.OCIImportedComponents == nil {
		cfg.Pkg.Build.OCIImportedComponents = make(map[string]string)
	}

	var (
		err       error
		pkgConfig = &Packager{
			cfg: cfg,
		}
	)

	if config.CommonOptions.TempDirectory != "" {
		// If the cache directory is within the temp directory, warn the user
		if strings.HasPrefix(config.CommonOptions.CachePath, config.CommonOptions.TempDirectory) {
			message.Warnf("The cache directory (%q) is within the temp directory (%q) and will be removed when the temp directory is cleaned up", config.CommonOptions.CachePath, config.CommonOptions.TempDirectory)
		}
	}

	// Create a temp directory for the package
	if err = pkgConfig.SetTempDirectory(config.CommonOptions.TempDirectory); err != nil {
		return nil, fmt.Errorf("unable to create package temp paths: %w", err)
	}

	return pkgConfig, nil
}

/*
NewOrDie creates a new package instance with the provided config or throws a fatal error.

Note: This function creates a tmp directory that should be cleaned up with p.ClearTempPaths().
*/
func NewOrDie(config *types.PackagerConfig) *Packager {
	var (
		err       error
		pkgConfig *Packager
	)

	if pkgConfig, err = New(config); err != nil {
		message.Fatalf(err, "Unable to setup the package config: %s", err.Error())
	}

	return pkgConfig
}

// SetTempDirectory sets the temp directory for the packager.
func (p *Packager) SetTempDirectory(path string) error {
	if p.tmp.Base != "" {
		p.ClearTempPaths()
	}

	tmp, err := createPaths(path)
	if err != nil {
		return fmt.Errorf("unable to create package temp paths: %w", err)
	}
	p.tmp = tmp
	return nil
}

func (p *Packager) WithProvider(provider types.PackageProvider) *Packager {
	p.provider = provider
	return p
}

// GetInitPackageName returns the formatted name of the init package.
func GetInitPackageName(arch string) string {
	if arch == "" {
		// No package has been loaded yet so lookup GetArch() with no package info
		arch = config.GetArch()
	}
	return fmt.Sprintf("zarf-init-%s-%s.tar.zst", arch, config.CLIVersion)
}

// GetPackageName returns the formatted name of the package.
func (p *Packager) GetPackageName() string {
	if p.cfg.Pkg.Kind == types.ZarfInitConfig {
		return GetInitPackageName(p.arch)
	}

	packageName := p.cfg.Pkg.Metadata.Name
	suffix := "tar.zst"
	if p.cfg.Pkg.Metadata.Uncompressed {
		suffix = "tar"
	}

	packageFileName := fmt.Sprintf("%s%s-%s", config.ZarfPackagePrefix, packageName, p.arch)
	if p.cfg.Pkg.Build.Differential {
		packageFileName = fmt.Sprintf("%s-%s-differential-%s", packageFileName, p.cfg.CreateOpts.DifferentialData.DifferentialPackageVersion, p.cfg.Pkg.Metadata.Version)
	} else if p.cfg.Pkg.Metadata.Version != "" {
		packageFileName = fmt.Sprintf("%s-%s", packageFileName, p.cfg.Pkg.Metadata.Version)
	}

	return fmt.Sprintf("%s.%s", packageFileName, suffix)
}

// GetInitPackageRemote returns the URL for a remote init package for the given architecture
func GetInitPackageRemote(arch string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", config.GithubProject, config.CLIVersion, GetInitPackageName(arch))
}

// ClearTempPaths removes the temp directory and any files within it.
func (p *Packager) ClearTempPaths() {
	// Remove the temp directory, but don't throw an error if it fails
	_ = os.RemoveAll(p.tmp.Base)
	_ = os.RemoveAll(config.ZarfSBOMDir)
}

func (p *Packager) createOrGetComponentPaths(component types.ZarfComponent) (paths types.ComponentPaths, err error) {
	base := filepath.Join(p.tmp.Components, component.Name)

	err = utils.CreateDirectory(base, 0700)
	if err != nil {
		return paths, err
	}

	paths = types.ComponentPaths{
		Base:           base,
		Temp:           filepath.Join(base, types.TempFolder),
		Files:          filepath.Join(base, types.FilesFolder),
		Charts:         filepath.Join(base, types.ChartsFolder),
		Repos:          filepath.Join(base, types.ReposFolder),
		Manifests:      filepath.Join(base, types.ManifestsFolder),
		DataInjections: filepath.Join(base, types.DataInjectionsFolder),
		Values:         filepath.Join(base, types.ValuesFolder),
	}

	if len(component.Files) > 0 {
		err = utils.CreateDirectory(paths.Files, 0700)
		if err != nil {
			return paths, err
		}
	}

	if len(component.Charts) > 0 {
		err = utils.CreateDirectory(paths.Charts, 0700)
		if err != nil {
			return paths, err
		}
		for _, chart := range component.Charts {
			if len(chart.ValuesFiles) > 0 {
				err = utils.CreateDirectory(paths.Values, 0700)
				if err != nil {
					return paths, err
				}
				break
			}
		}
	}

	if len(component.Repos) > 0 {
		err = utils.CreateDirectory(paths.Repos, 0700)
		if err != nil {
			return paths, err
		}
	}

	if len(component.Manifests) > 0 {
		err = utils.CreateDirectory(paths.Manifests, 0700)
		if err != nil {
			return paths, err
		}
	}

	if len(component.DataInjections) > 0 {
		err = utils.CreateDirectory(paths.DataInjections, 0700)
		if err != nil {
			return paths, err
		}
	}

	return paths, err
}

func isValidFileExtension(filename string) bool {
	for _, extension := range config.GetValidPackageExtensions() {
		if strings.HasSuffix(filename, extension) {
			return true
		}
	}

	return false
}

func createPaths(basePath string) (paths types.TempPaths, err error) {
	if basePath == "" {
		basePath, err = utils.MakeTempDir()
		if err != nil {
			return paths, err
		}
	} else {
		if err := utils.CreateDirectory(basePath, 0700); err != nil {
			return paths, fmt.Errorf("unable to create temp directory: %w", err)
		}
	}
	message.Debug("Using temporary directory:", basePath)
	paths = types.TempPaths{
		Base: basePath,

		InjectBinary: filepath.Join(basePath, "zarf-injector"),
		SeedImages:   filepath.Join(basePath, "seed-images"),
		Images:       filepath.Join(basePath, "images"),
		Components:   filepath.Join(basePath, config.ZarfComponentsDir),
		Sboms:        filepath.Join(basePath, "sboms"),
		MetadataPaths: types.MetadataPaths{
			Checksums: filepath.Join(basePath, config.ZarfChecksumsTxt),
			ZarfYaml:  filepath.Join(basePath, config.ZarfYAML),
			ZarfSig:   filepath.Join(basePath, config.ZarfYAMLSignature),
			SbomTar:   filepath.Join(basePath, config.ZarfSBOMTar),
		},
	}

	return paths, err
}

func getRequestedComponentList(requestedComponents string) []string {
	if requestedComponents != "" {
		split := strings.Split(requestedComponents, ",")
		for idx, component := range split {
			split[idx] = strings.ToLower(strings.TrimSpace(component))
		}
		return split
	}

	return []string{}
}

// validatePackageArchitecture validates that the package architecture matches the target cluster architecture.
func (p *Packager) validatePackageArchitecture() (err error) {
	// Ignore this check if the package architecture is explicitly "multi"
	if p.arch == "multi" {
		return nil
	}

	// Fetch cluster architecture only if we're already connected to a cluster.
	if p.cluster != nil {
		clusterArch, err := p.cluster.GetArchitecture()
		if err != nil {
			return lang.ErrUnableToCheckArch
		}

		// Check if the package architecture and the cluster architecture are the same.
		if p.arch != clusterArch {
			return fmt.Errorf(lang.CmdPackageDeployValidateArchitectureErr, p.arch, clusterArch)
		}
	}

	return nil
}

// validateLastNonBreakingVersion compares the Zarf CLI version against a package's LastNonBreakingVersion.
// It will return an error if there is an error parsing either of the two versions,
// and will throw a warning if the CLI version is less than the LastNonBreakingVersion.
func (p *Packager) validateLastNonBreakingVersion() (err error) {
	cliVersion := config.CLIVersion
	lastNonBreakingVersion := p.cfg.Pkg.Build.LastNonBreakingVersion

	if lastNonBreakingVersion == "" || cliVersion == "UnknownVersion" {
		return nil
	}

	lastNonBreakingSemVer, err := semver.NewVersion(lastNonBreakingVersion)
	if err != nil {
		return fmt.Errorf("unable to parse lastNonBreakingVersion '%s' from Zarf package build data : %w", lastNonBreakingVersion, err)
	}

	cliSemVer, err := semver.NewVersion(cliVersion)
	if err != nil {
		return fmt.Errorf("unable to parse Zarf CLI version '%s' : %w", cliVersion, err)
	}

	if cliSemVer.LessThan(lastNonBreakingSemVer) {
		warning := fmt.Sprintf(
			lang.CmdPackageDeployValidateLastNonBreakingVersionWarn,
			cliVersion,
			lastNonBreakingVersion,
			lastNonBreakingVersion,
		)
		p.warnings = append(p.warnings, warning)
	}

	return nil
}

var (
	// ErrPkgKeyButNoSig is returned when a key was provided but the package is not signed
	ErrPkgKeyButNoSig = errors.New("a key was provided but the package is not signed - remove the --key flag and run the command again")
	// ErrPkgSigButNoKey is returned when a package is signed but no key was provided
	ErrPkgSigButNoKey = errors.New("package is signed but no key was provided - add a key with the --key flag or use the --insecure flag and run the command again")
)

// ValidatePackageSignature validates the signature of a package
func ValidatePackageSignature(directory string, publicKeyPath string) error {
	// If the insecure flag was provided ignore the signature validation
	if config.CommonOptions.Insecure {
		return nil
	}

	// Handle situations where there is no signature within the package
	sigExist := !utils.InvalidPath(filepath.Join(directory, config.ZarfYAMLSignature))
	if !sigExist && publicKeyPath == "" {
		// Nobody was expecting a signature, so we can just return
		return nil
	} else if sigExist && publicKeyPath == "" {
		// The package is signed but no key was provided
		return ErrPkgSigButNoKey
	} else if !sigExist && publicKeyPath != "" {
		// A key was provided but there is no signature
		return ErrPkgKeyButNoSig
	}

	// Validate the signature with the key we were provided
	if err := utils.CosignVerifyBlob(filepath.Join(directory, config.ZarfYAML), filepath.Join(directory, config.ZarfYAMLSignature), publicKeyPath); err != nil {
		return fmt.Errorf("package signature did not match the provided key: %w", err)
	}

	return nil
}

func (p *Packager) getSigCreatePassword(_ bool) ([]byte, error) {
	// CLI flags take priority (also loads from viper configs)
	if p.cfg.CreateOpts.SigningKeyPassword != "" {
		return []byte(p.cfg.CreateOpts.SigningKeyPassword), nil
	}

	return interactive.PromptSigPassword()
}

func (p *Packager) getSigPublishPassword(_ bool) ([]byte, error) {
	// CLI flags take priority (also loads from viper configs)
	if p.cfg.CreateOpts.SigningKeyPassword != "" {
		return []byte(p.cfg.CreateOpts.SigningKeyPassword), nil
	}

	return interactive.PromptSigPassword()
}

func (p *Packager) archiveComponent(component types.ZarfComponent) error {
	componentPath := filepath.Join(p.tmp.Components, component.Name)
	size, err := utils.GetDirSize(componentPath)
	if err != nil {
		return err
	}
	if size > 0 {
		tar := fmt.Sprintf("%s.tar", componentPath)
		message.Debugf("Archiving %s to '%s'", component.Name, tar)
		err := archiver.Archive([]string{componentPath}, tar)
		if err != nil {
			return err
		}
	} else {
		message.Debugf("Component %s is empty, skipping archiving", component.Name)
	}
	return os.RemoveAll(componentPath)
}

func (p *Packager) archivePackage(sourceDir string, destinationTarball string) error {
	spinner := message.NewProgressSpinner("Writing %s to %s", sourceDir, destinationTarball)
	defer spinner.Stop()
	// Make the archive
	archiveSrc := []string{sourceDir + string(os.PathSeparator)}
	if err := archiver.Archive(archiveSrc, destinationTarball); err != nil {
		return fmt.Errorf("unable to create package: %w", err)
	}
	spinner.Updatef("Wrote %s to %s", sourceDir, destinationTarball)

	f, err := os.Stat(destinationTarball)
	if err != nil {
		return fmt.Errorf("unable to read the package archive: %w", err)
	}

	// Convert Megabytes to bytes.
	chunkSize := p.cfg.CreateOpts.MaxPackageSizeMB * 1000 * 1000

	// If a chunk size was specified and the package is larger than the chunk size, split it into chunks.
	if p.cfg.CreateOpts.MaxPackageSizeMB > 0 && f.Size() > int64(chunkSize) {
		spinner.Updatef("Package is larger than %dMB, splitting into multiple files", p.cfg.CreateOpts.MaxPackageSizeMB)
		chunks, sha256sum, err := utils.SplitFile(destinationTarball, chunkSize)
		if err != nil {
			return fmt.Errorf("unable to split the package archive into multiple files: %w", err)
		}
		if len(chunks) > 999 {
			return fmt.Errorf("unable to split the package archive into multiple files: must be less than 1,000 files")
		}

		status := fmt.Sprintf("Package split into %d files, original sha256sum is %s", len(chunks)+1, sha256sum)
		spinner.Updatef(status)
		message.Debug(status)
		_ = os.RemoveAll(destinationTarball)

		// Marshal the data into a json file.
		jsonData, err := json.Marshal(types.ZarfPartialPackageData{
			Count:     len(chunks),
			Bytes:     f.Size(),
			Sha256Sum: sha256sum,
		})
		if err != nil {
			return fmt.Errorf("unable to marshal the partial package data: %w", err)
		}

		// Prepend the json data to the first chunk.
		chunks = append([][]byte{jsonData}, chunks...)

		for idx, chunk := range chunks {
			path := fmt.Sprintf("%s.part%03d", destinationTarball, idx)
			status := fmt.Sprintf("Writing %s", path)
			spinner.Updatef(status)
			message.Debug(status)
			if err := os.WriteFile(path, chunk, 0644); err != nil {
				return fmt.Errorf("unable to write the file %s: %w", path, err)
			}
		}
	}
	spinner.Successf("Package tarball successfully written")
	return nil
}

// SetOCIRemote sets the remote OCI client for the package.
func (p *Packager) SetOCIRemote(url string) error {
	remote, err := oci.NewOrasRemote(url)
	if err != nil {
		return err
	}
	p.remote = remote
	return nil
}
