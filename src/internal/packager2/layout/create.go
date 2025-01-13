// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

package layout

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/defenseunicorns/pkg/helpers/v2"
	goyaml "github.com/goccy/go-yaml"
	"github.com/mholt/archiver/v3"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/sign"

	"github.com/zarf-dev/zarf/src/api/v1alpha1"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/internal/git"
	"github.com/zarf-dev/zarf/src/internal/packager/helm"
	"github.com/zarf-dev/zarf/src/internal/packager/images"
	"github.com/zarf-dev/zarf/src/internal/packager/kustomize"
	actions2 "github.com/zarf-dev/zarf/src/internal/packager2/actions"
	"github.com/zarf-dev/zarf/src/internal/packager2/filters"
	"github.com/zarf-dev/zarf/src/pkg/interactive"
	"github.com/zarf-dev/zarf/src/pkg/lint"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/message"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/pkg/zoci"
	"github.com/zarf-dev/zarf/src/types"
)

// CreateOptions are the options for creating a skeleton package.
type CreateOptions struct {
	Flavor                  string
	RegistryOverrides       map[string]string
	SigningKeyPath          string
	SigningKeyPassword      string
	SetVariables            map[string]string
	SkipSBOM                bool
	DifferentialPackagePath string
}

func CreatePackage(ctx context.Context, packagePath string, opt CreateOptions) (*PackageLayout, error) {
	l := logger.From(ctx)
	l.Info("creating package", "path", packagePath)

	buildPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}

	pkg, err := loadPackage(ctx, packagePath, opt.Flavor, opt.SetVariables)
	if err != nil {
		return nil, err
	}

	if opt.DifferentialPackagePath != "" {
		l.Debug("creating differential package", "differential", opt.DifferentialPackagePath)
		layoutOpt := PackageLayoutOptions{
			SkipSignatureValidation: true,
		}
		diffPkgLayout, err := LoadFromTar(ctx, opt.DifferentialPackagePath, layoutOpt)
		if err != nil {
			return nil, err
		}
		allIncludedImagesMap := map[string]bool{}
		allIncludedReposMap := map[string]bool{}
		for _, component := range diffPkgLayout.Pkg.Components {
			for _, image := range component.Images {
				allIncludedImagesMap[image] = true
			}
			for _, repo := range component.Repos {
				allIncludedReposMap[repo] = true
			}
		}

		pkg.Build.Differential = true
		pkg.Build.DifferentialPackageVersion = diffPkgLayout.Pkg.Metadata.Version

		versionsMatch := diffPkgLayout.Pkg.Metadata.Version == pkg.Metadata.Version
		if versionsMatch {
			return nil, errors.New(lang.PkgCreateErrDifferentialSameVersion)
		}
		noVersionSet := diffPkgLayout.Pkg.Metadata.Version == "" || pkg.Metadata.Version == ""
		if noVersionSet {
			return nil, errors.New(lang.PkgCreateErrDifferentialNoVersion)
		}
		filter := filters.ByDifferentialData(allIncludedImagesMap, allIncludedReposMap)
		pkg.Components, err = filter.Apply(pkg)
		if err != nil {
			return nil, err
		}
	}

	for _, component := range pkg.Components {
		err := assemblePackageComponent(ctx, component, packagePath, buildPath)
		if err != nil {
			return nil, err
		}
	}

	componentImages := []transform.Image{}
	for _, component := range pkg.Components {
		for _, src := range component.Images {
			refInfo, err := transform.ParseImageRef(src)
			if err != nil {
				return nil, fmt.Errorf("failed to create ref for image %s: %w", src, err)
			}
			if slices.Contains(componentImages, refInfo) {
				continue
			}
			componentImages = append(componentImages, refInfo)
		}
	}
	sbomImageList := []transform.Image{}
	if len(componentImages) > 0 {
		cachePath, err := config.GetAbsCachePath()
		if err != nil {
			return nil, err
		}
		pullCfg := images.PullConfig{
			DestinationDirectory: filepath.Join(buildPath, ImagesDir),
			ImageList:            componentImages,
			Arch:                 pkg.Metadata.Architecture,
			RegistryOverrides:    opt.RegistryOverrides,
			CacheDirectory:       filepath.Join(cachePath, ImagesDir),
		}
		pulled, err := images.Pull(ctx, pullCfg)
		if err != nil {
			return nil, err
		}
		for info, img := range pulled {
			ok, err := utils.OnlyHasImageLayers(img)
			if err != nil {
				return nil, fmt.Errorf("failed to validate %s is an image and not an artifact: %w", info, err)
			}
			if ok {
				sbomImageList = append(sbomImageList, info)
			}
		}

		// Sort images index to make build reproducible.
		err = utils.SortImagesIndex(filepath.Join(buildPath, ImagesDir))
		if err != nil {
			return nil, err
		}
	}

	l.Info("composed components successfully")

	if !opt.SkipSBOM {
		l.Info("generating SBOM")
		err = generateSBOM(ctx, pkg, buildPath, sbomImageList)
		if err != nil {
			return nil, err
		}
	}

	checksumContent, checksumSha, err := getChecksum(buildPath)
	if err != nil {
		return nil, err
	}
	checksumPath := filepath.Join(buildPath, Checksums)
	err = os.WriteFile(checksumPath, []byte(checksumContent), helpers.ReadWriteUser)
	if err != nil {
		return nil, err
	}
	pkg.Metadata.AggregateChecksum = checksumSha

	pkg = recordPackageMetadata(pkg, opt.Flavor, opt.RegistryOverrides)

	b, err := goyaml.Marshal(pkg)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(filepath.Join(buildPath, ZarfYAML), b, helpers.ReadWriteUser)
	if err != nil {
		return nil, err
	}

	err = signPackage(buildPath, opt.SigningKeyPath, opt.SigningKeyPassword)
	if err != nil {
		return nil, err
	}

	pkgLayout, err := LoadFromDir(ctx, buildPath, PackageLayoutOptions{SkipSignatureValidation: true})
	if err != nil {
		return nil, err
	}

	l.Info("package created")

	return pkgLayout, nil
}

// CreateSkeleton creates a skeleton package and returns the path to the created package.
func CreateSkeleton(ctx context.Context, packagePath string, opt CreateOptions) (string, error) {
	pkg, err := loadPackage(ctx, packagePath, opt.Flavor, nil)
	if err != nil {
		return "", err
	}
	pkg.Metadata.Architecture = zoci.SkeletonArch

	buildPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return "", err
	}

	for _, component := range pkg.Components {
		err := assembleSkeletonComponent(component, packagePath, buildPath)
		if err != nil {
			return "", err
		}
	}

	checksumContent, checksumSha, err := getChecksum(buildPath)
	if err != nil {
		return "", err
	}
	checksumPath := filepath.Join(buildPath, Checksums)
	err = os.WriteFile(checksumPath, []byte(checksumContent), helpers.ReadWriteUser)
	if err != nil {
		return "", err
	}
	pkg.Metadata.AggregateChecksum = checksumSha

	pkg = recordPackageMetadata(pkg, opt.Flavor, opt.RegistryOverrides)

	b, err := goyaml.Marshal(pkg)
	if err != nil {
		return "", err
	}
	err = os.WriteFile(filepath.Join(buildPath, ZarfYAML), b, helpers.ReadWriteUser)
	if err != nil {
		return "", err
	}

	err = signPackage(buildPath, opt.SigningKeyPath, opt.SigningKeyPassword)
	if err != nil {
		return "", err
	}

	return buildPath, nil
}

func loadPackage(ctx context.Context, packagePath, flavor string, setVariables map[string]string) (v1alpha1.ZarfPackage, error) {
	b, err := os.ReadFile(filepath.Join(packagePath, ZarfYAML))
	if err != nil {
		return v1alpha1.ZarfPackage{}, err
	}
	pkg, err := ParseZarfPackage(b)
	if err != nil {
		return v1alpha1.ZarfPackage{}, err
	}
	pkg.Metadata.Architecture = config.GetArch(pkg.Metadata.Architecture)
	pkg, err = resolveImports(ctx, pkg, packagePath, pkg.Metadata.Architecture, flavor, map[string]interface{}{})
	if err != nil {
		return v1alpha1.ZarfPackage{}, err
	}
	if setVariables != nil {
		pkg, _, err = fillActiveTemplate(ctx, pkg, setVariables)
		if err != nil {
			return v1alpha1.ZarfPackage{}, err
		}
	}
	err = validate(pkg, packagePath, setVariables)
	if err != nil {
		return v1alpha1.ZarfPackage{}, err
	}
	return pkg, nil
}

func validate(pkg v1alpha1.ZarfPackage, packagePath string, setVariables map[string]string) error {
	err := lint.ValidatePackage(pkg)
	if err != nil {
		return fmt.Errorf("package validation failed: %w", err)
	}
	findings, err := lint.ValidatePackageSchemaAtPath(packagePath, setVariables)
	if err != nil {
		return fmt.Errorf("unable to check schema: %w", err)
	}
	if len(findings) == 0 {
		return nil
	}
	return &lint.LintError{
		BaseDir:     packagePath,
		PackageName: pkg.Metadata.Name,
		Findings:    findings,
	}
}

func assemblePackageComponent(ctx context.Context, component v1alpha1.ZarfComponent, packagePath, buildPath string) error {
	tmpBuildPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpBuildPath)
	compBuildPath := filepath.Join(tmpBuildPath, component.Name)
	err = os.MkdirAll(compBuildPath, 0o700)
	if err != nil {
		return err
	}

	onCreate := component.Actions.OnCreate
	if err := actions2.Run(ctx, packagePath, onCreate.Defaults, onCreate.Before, nil); err != nil {
		return fmt.Errorf("unable to run component before action: %w", err)
	}

	// If any helm charts are defined, process them.
	for _, chart := range component.Charts {
		// TODO: Refactor helm builder
		if chart.LocalPath != "" {
			chart.LocalPath = filepath.Join(packagePath, chart.LocalPath)
		}
		oldValuesFiles := chart.ValuesFiles
		valuesFiles := []string{}
		for _, v := range chart.ValuesFiles {
			valuesFiles = append(valuesFiles, filepath.Join(packagePath, v))
		}
		chart.ValuesFiles = valuesFiles
		helmCfg := helm.New(chart, filepath.Join(compBuildPath, string(ChartsComponentDir)), filepath.Join(compBuildPath, string(ValuesComponentDir)))
		if err := helmCfg.PackageChart(ctx, filepath.Join(compBuildPath, string(ChartsComponentDir))); err != nil {
			return err
		}
		chart.ValuesFiles = oldValuesFiles
	}

	for filesIdx, file := range component.Files {
		rel := filepath.Join(string(FilesComponentDir), strconv.Itoa(filesIdx), filepath.Base(file.Target))
		dst := filepath.Join(compBuildPath, rel)
		destinationDir := filepath.Dir(dst)

		if helpers.IsURL(file.Source) {
			if file.ExtractPath != "" {
				// get the compressedFileName from the source
				compressedFileName, err := helpers.ExtractBasePathFromURL(file.Source)
				if err != nil {
					return fmt.Errorf(lang.ErrFileNameExtract, file.Source, err.Error())
				}
				tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)
				compressedFile := filepath.Join(tmpDir, compressedFileName)

				// If the file is an archive, download it to the componentPath.Temp
				if err := utils.DownloadToFile(ctx, file.Source, compressedFile, component.DeprecatedCosignKeyPath); err != nil {
					return fmt.Errorf(lang.ErrDownloading, file.Source, err.Error())
				}
				err = archiver.Extract(compressedFile, file.ExtractPath, destinationDir)
				if err != nil {
					return fmt.Errorf(lang.ErrFileExtract, file.ExtractPath, compressedFileName, err.Error())
				}
			} else {
				if err := utils.DownloadToFile(ctx, file.Source, dst, component.DeprecatedCosignKeyPath); err != nil {
					return fmt.Errorf(lang.ErrDownloading, file.Source, err.Error())
				}
			}
		} else {
			if file.ExtractPath != "" {
				if err := archiver.Extract(filepath.Join(packagePath, file.Source), file.ExtractPath, destinationDir); err != nil {
					return fmt.Errorf(lang.ErrFileExtract, file.ExtractPath, file.Source, err.Error())
				}
			} else {
				if err := helpers.CreatePathAndCopy(filepath.Join(packagePath, file.Source), dst); err != nil {
					return fmt.Errorf("unable to copy file %s: %w", file.Source, err)
				}
			}
		}

		if file.ExtractPath != "" {
			// Make sure dst reflects the actual file or directory.
			updatedExtractedFileOrDir := filepath.Join(destinationDir, file.ExtractPath)
			if updatedExtractedFileOrDir != dst {
				if err := os.Rename(updatedExtractedFileOrDir, dst); err != nil {
					return fmt.Errorf(lang.ErrWritingFile, dst, err)
				}
			}
		}

		// Abort packaging on invalid shasum (if one is specified).
		if file.Shasum != "" {
			if err := helpers.SHAsMatch(dst, file.Shasum); err != nil {
				return err
			}
		}

		if file.Executable || helpers.IsDir(dst) {
			err := os.Chmod(dst, helpers.ReadWriteExecuteUser)
			if err != nil {
				return err
			}
		} else {
			err := os.Chmod(dst, helpers.ReadWriteUser)
			if err != nil {
				return err
			}
		}
	}

	for dataIdx, data := range component.DataInjections {
		rel := filepath.Join(string(DataComponentDir), strconv.Itoa(dataIdx), filepath.Base(data.Target.Path))
		dst := filepath.Join(compBuildPath, rel)

		if helpers.IsURL(data.Source) {
			if err := utils.DownloadToFile(ctx, data.Source, dst, component.DeprecatedCosignKeyPath); err != nil {
				return fmt.Errorf(lang.ErrDownloading, data.Source, err.Error())
			}
		} else {
			if err := helpers.CreatePathAndCopy(filepath.Join(packagePath, data.Source), dst); err != nil {
				return fmt.Errorf("unable to copy data injection %s: %s", data.Source, err.Error())
			}
		}
	}

	// Iterate over all manifests.
	if len(component.Manifests) > 0 {
		err := os.MkdirAll(filepath.Join(compBuildPath, string(ManifestsComponentDir)), 0o700)
		if err != nil {
			return err
		}
	}
	for _, manifest := range component.Manifests {
		for fileIdx, path := range manifest.Files {
			rel := filepath.Join(string(ManifestsComponentDir), fmt.Sprintf("%s-%d.yaml", manifest.Name, fileIdx))
			dst := filepath.Join(compBuildPath, rel)

			// Copy manifests without any processing.
			if helpers.IsURL(path) {
				if err := utils.DownloadToFile(ctx, path, dst, component.DeprecatedCosignKeyPath); err != nil {
					return fmt.Errorf(lang.ErrDownloading, path, err.Error())
				}
			} else {
				if err := helpers.CreatePathAndCopy(filepath.Join(packagePath, path), dst); err != nil {
					return fmt.Errorf("unable to copy manifest %s: %w", path, err)
				}
			}
		}

		for kustomizeIdx, path := range manifest.Kustomizations {
			// Generate manifests from kustomizations and place in the package.
			kname := fmt.Sprintf("kustomization-%s-%d.yaml", manifest.Name, kustomizeIdx)
			rel := filepath.Join(string(ManifestsComponentDir), kname)
			dst := filepath.Join(compBuildPath, rel)

			if !helpers.IsURL(path) {
				path = filepath.Join(packagePath, path)
			}
			if err := kustomize.Build(path, dst, manifest.KustomizeAllowAnyDirectory); err != nil {
				return fmt.Errorf("unable to build kustomization %s: %w", path, err)
			}
		}
	}

	// Load all specified git repos.
	for _, url := range component.Repos {
		// Pull all the references if there is no `@` in the string.
		_, err := git.Clone(ctx, filepath.Join(compBuildPath, string(RepoComponentDir)), url, false)
		if err != nil {
			return fmt.Errorf("unable to pull git repo %s: %w", url, err)
		}
	}

	if err := actions2.Run(ctx, packagePath, onCreate.Defaults, onCreate.After, nil); err != nil {
		return fmt.Errorf("unable to run component after action: %w", err)
	}

	// Write the tar component.
	entries, err := os.ReadDir(compBuildPath)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	tarPath := filepath.Join(buildPath, "components", fmt.Sprintf("%s.tar", component.Name))
	err = os.MkdirAll(filepath.Join(buildPath, "components"), 0o700)
	if err != nil {
		return err
	}
	err = createReproducibleTarballFromDir(compBuildPath, component.Name, tarPath, false)
	if err != nil {
		return err
	}
	return nil
}

func assembleSkeletonComponent(component v1alpha1.ZarfComponent, packagePath, buildPath string) error {
	tmpBuildPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpBuildPath)
	compBuildPath := filepath.Join(tmpBuildPath, component.Name)
	err = os.MkdirAll(compBuildPath, 0o700)
	if err != nil {
		return err
	}

	for chartIdx, chart := range component.Charts {
		if chart.LocalPath != "" {
			rel := filepath.Join(string(ChartsComponentDir), fmt.Sprintf("%s-%d", chart.Name, chartIdx))
			dst := filepath.Join(compBuildPath, rel)

			err := helpers.CreatePathAndCopy(filepath.Join(packagePath, chart.LocalPath), dst)
			if err != nil {
				return err
			}

			component.Charts[chartIdx].LocalPath = rel
		}

		for valuesIdx, path := range chart.ValuesFiles {
			if helpers.IsURL(path) {
				continue
			}

			rel := fmt.Sprintf("%s-%d", helm.StandardName(string(ValuesComponentDir), chart), valuesIdx)
			component.Charts[chartIdx].ValuesFiles[valuesIdx] = rel

			if err := helpers.CreatePathAndCopy(filepath.Join(packagePath, path), filepath.Join(compBuildPath, rel)); err != nil {
				return fmt.Errorf("unable to copy chart values file %s: %w", path, err)
			}
		}
	}

	for filesIdx, file := range component.Files {
		if helpers.IsURL(file.Source) {
			continue
		}

		rel := filepath.Join(string(FilesComponentDir), strconv.Itoa(filesIdx), filepath.Base(file.Target))
		dst := filepath.Join(compBuildPath, rel)
		destinationDir := filepath.Dir(dst)

		if file.ExtractPath != "" {
			if err := archiver.Extract(filepath.Join(packagePath, file.Source), file.ExtractPath, destinationDir); err != nil {
				return fmt.Errorf(lang.ErrFileExtract, file.ExtractPath, file.Source, err.Error())
			}

			// Make sure dst reflects the actual file or directory.
			updatedExtractedFileOrDir := filepath.Join(destinationDir, file.ExtractPath)
			if updatedExtractedFileOrDir != dst {
				if err := os.Rename(updatedExtractedFileOrDir, dst); err != nil {
					return fmt.Errorf(lang.ErrWritingFile, dst, err)
				}
			}
		} else {
			if err := helpers.CreatePathAndCopy(filepath.Join(packagePath, file.Source), dst); err != nil {
				return fmt.Errorf("unable to copy file %s: %w", file.Source, err)
			}
		}

		// Change the source to the new relative source directory (any remote files will have been skipped above)
		component.Files[filesIdx].Source = rel

		// Remove the extractPath from a skeleton since it will already extract it
		component.Files[filesIdx].ExtractPath = ""

		// Abort packaging on invalid shasum (if one is specified).
		if file.Shasum != "" {
			if err := helpers.SHAsMatch(dst, file.Shasum); err != nil {
				return err
			}
		}

		if file.Executable || helpers.IsDir(dst) {
			err = os.Chmod(dst, helpers.ReadWriteExecuteUser)
			if err != nil {
				return err
			}
		} else {
			err = os.Chmod(dst, helpers.ReadWriteUser)
			if err != nil {
				return err
			}
		}
	}

	for dataIdx, data := range component.DataInjections {
		rel := filepath.Join(string(DataComponentDir), strconv.Itoa(dataIdx), filepath.Base(data.Target.Path))
		dst := filepath.Join(compBuildPath, rel)

		if err := helpers.CreatePathAndCopy(filepath.Join(packagePath, data.Source), dst); err != nil {
			return fmt.Errorf("unable to copy data injection %s: %s", data.Source, err.Error())
		}

		component.DataInjections[dataIdx].Source = rel
	}

	// Iterate over all manifests.
	for manifestIdx, manifest := range component.Manifests {
		for fileIdx, path := range manifest.Files {
			rel := filepath.Join(string(ManifestsComponentDir), fmt.Sprintf("%s-%d.yaml", manifest.Name, fileIdx))
			dst := filepath.Join(compBuildPath, rel)

			// Copy manifests without any processing.
			if err := helpers.CreatePathAndCopy(filepath.Join(packagePath, path), dst); err != nil {
				return fmt.Errorf("unable to copy manifest %s: %w", path, err)
			}

			component.Manifests[manifestIdx].Files[fileIdx] = rel
		}

		for kustomizeIdx, path := range manifest.Kustomizations {
			// Generate manifests from kustomizations and place in the package.
			kname := fmt.Sprintf("kustomization-%s-%d.yaml", manifest.Name, kustomizeIdx)
			rel := filepath.Join(string(ManifestsComponentDir), kname)
			dst := filepath.Join(compBuildPath, rel)

			if err := kustomize.Build(filepath.Join(packagePath, path), dst, manifest.KustomizeAllowAnyDirectory); err != nil {
				return fmt.Errorf("unable to build kustomization %s: %w", path, err)
			}
		}

		// remove kustomizations
		component.Manifests[manifestIdx].Kustomizations = nil
	}

	// Write the tar component.
	entries, err := os.ReadDir(compBuildPath)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	err = os.MkdirAll(filepath.Join(compBuildPath, "temp"), 0o700)
	if err != nil {
		return err
	}
	tarPath := filepath.Join(buildPath, "components", fmt.Sprintf("%s.tar", component.Name))
	err = os.MkdirAll(filepath.Join(buildPath, "components"), 0o700)
	if err != nil {
		return err
	}
	err = createReproducibleTarballFromDir(compBuildPath, component.Name, tarPath, true)
	if err != nil {
		return err
	}
	return nil
}

func recordPackageMetadata(pkg v1alpha1.ZarfPackage, flavor string, registryOverrides map[string]string) v1alpha1.ZarfPackage {
	now := time.Now()
	// Just use $USER env variable to avoid CGO issue.
	// https://groups.google.com/g/golang-dev/c/ZFDDX3ZiJ84.
	// Record the name of the user creating the package.
	if runtime.GOOS == "windows" {
		pkg.Build.User = os.Getenv("USERNAME")
	} else {
		pkg.Build.User = os.Getenv("USER")
	}

	// Record the hostname of the package creation terminal.
	// The error here is ignored because the hostname is not critical to the package creation.
	hostname, _ := os.Hostname()
	pkg.Build.Terminal = hostname

	if pkg.IsInitConfig() && pkg.Metadata.Version == "" {
		pkg.Metadata.Version = config.CLIVersion
	}

	pkg.Build.Architecture = pkg.Metadata.Architecture

	// Record the Zarf Version the CLI was built with.
	pkg.Build.Version = config.CLIVersion

	// Record the time of package creation.
	pkg.Build.Timestamp = now.Format(time.RFC1123Z)

	// Record the flavor of Zarf used to build this package (if any).
	pkg.Build.Flavor = flavor

	pkg.Build.RegistryOverrides = registryOverrides

	return pkg
}

func getChecksum(dirPath string) (string, string, error) {
	checksumData := []string{}
	err := filepath.Walk(dirPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		if rel == ZarfYAML || rel == Checksums {
			return nil
		}
		sum, err := helpers.GetSHA256OfFile(path)
		if err != nil {
			return err
		}
		checksumData = append(checksumData, fmt.Sprintf("%s %s", sum, filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		return "", "", err
	}
	slices.Sort(checksumData)

	checksumContent := strings.Join(checksumData, "\n") + "\n"
	sha := sha256.Sum256([]byte(checksumContent))
	return checksumContent, hex.EncodeToString(sha[:]), nil
}

func signPackage(dirPath, signingKeyPath, signingKeyPassword string) error {
	if signingKeyPath == "" {
		return nil
	}
	passFunc := func(_ bool) ([]byte, error) {
		return []byte(signingKeyPassword), nil
	}
	keyOpts := options.KeyOpts{
		KeyRef:   signingKeyPath,
		PassFunc: passFunc,
	}
	rootOpts := &options.RootOptions{
		Verbose: false,
		Timeout: options.DefaultTimeout,
	}
	_, err := sign.SignBlobCmd(
		rootOpts,
		keyOpts,
		filepath.Join(dirPath, ZarfYAML),
		true,
		filepath.Join(dirPath, Signature),
		"",
		false)
	if err != nil {
		return err
	}
	return nil
}

func createReproducibleTarballFromDir(dirPath, dirPrefix, tarballPath string, overrideMode bool) error {
	tb, err := os.Create(tarballPath)
	if err != nil {
		return fmt.Errorf("error creating tarball: %w", err)
	}
	defer tb.Close()

	tw := tar.NewWriter(tb)
	defer tw.Close()

	// Walk through the directory and process each file
	return filepath.Walk(dirPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		link := ""
		if info.Mode().Type() == os.ModeSymlink {
			link, err = os.Readlink(filePath)
			if err != nil {
				return fmt.Errorf("error reading symlink: %w", err)
			}
		}

		// Create a new header
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return fmt.Errorf("error creating tar header: %w", err)
		}

		// Strip non-deterministic header data
		header.ModTime = time.Time{}
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""

		// When run on windows the header mode will set all permission octals to the same value as the first octal.
		// A file created with 0o700 will return 0o777 when read back. This discrepancy causes differences between packages
		// created on Windows and Linux.
		// https://medium.com/@MichalPristas/go-and-file-perms-on-windows-3c944d55dd44
		// To mitigate this difference we zero all but the last permission octal when writing files to the tar. Making sure
		// that when unpackaged files from packages created on Windows and Linux will have the same permissions.
		// The &^ operator called AND NOT sets the bits to 0 in the left hand if the right hand bits are 1.
		// https://medium.com/learning-the-go-programming-language/bit-hacking-with-go-e0acee258827
		if overrideMode {
			header.Mode = header.Mode &^ 0o077
		}

		// Ensure the header's name is correctly set relative to the base directory
		name, err := filepath.Rel(dirPath, filePath)
		if err != nil {
			return fmt.Errorf("error getting relative path: %w", err)
		}
		name = filepath.Join(dirPrefix, name)
		name = filepath.ToSlash(name)
		header.Name = name

		// Write the header to the tarball
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("error writing header: %w", err)
		}

		// If it's a file, write its content
		if info.Mode().IsRegular() {
			file, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("error opening file: %w", err)
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return fmt.Errorf("error writing file to tarball: %w", err)
			}
		}

		return nil
	})
}

func fillActiveTemplate(ctx context.Context, pkg v1alpha1.ZarfPackage, setVariables map[string]string) (v1alpha1.ZarfPackage, []string, error) {
	templateMap := map[string]string{}
	warnings := []string{}

	promptAndSetTemplate := func(templatePrefix string, deprecated bool) error {
		yamlTemplates, err := utils.FindYamlTemplates(&pkg, templatePrefix, "###")
		if err != nil {
			return err
		}

		for key := range yamlTemplates {
			if deprecated {
				warnings = append(warnings, fmt.Sprintf(lang.PkgValidateTemplateDeprecation, key, key, key))
			}

			_, present := setVariables[key]
			if !present && !config.CommonOptions.Confirm {
				setVal, err := interactive.PromptVariable(ctx, v1alpha1.InteractiveVariable{
					Variable: v1alpha1.Variable{Name: key},
				})
				if err != nil {
					return err
				}
				setVariables[key] = setVal
			} else if !present {
				return fmt.Errorf("template %q must be '--set' when using the '--confirm' flag", key)
			}
		}

		for key, value := range setVariables {
			templateMap[fmt.Sprintf("%s%s###", templatePrefix, key)] = value
		}

		return nil
	}

	// update the component templates on the package
	if err := reloadComponentTemplatesInPackage(&pkg); err != nil {
		return v1alpha1.ZarfPackage{}, nil, err
	}

	if err := promptAndSetTemplate(v1alpha1.ZarfPackageTemplatePrefix, false); err != nil {
		return v1alpha1.ZarfPackage{}, nil, err
	}
	// [DEPRECATION] Set the Package Variable syntax as well for backward compatibility
	if err := promptAndSetTemplate(v1alpha1.ZarfPackageVariablePrefix, true); err != nil {
		return v1alpha1.ZarfPackage{}, nil, err
	}

	// Add special variable for the current package architecture
	templateMap[v1alpha1.ZarfPackageArch] = pkg.Metadata.Architecture

	if err := utils.ReloadYamlTemplate(&pkg, templateMap); err != nil {
		return v1alpha1.ZarfPackage{}, nil, err
	}

	return pkg, warnings, nil
}

// reloadComponentTemplate appends ###ZARF_COMPONENT_NAME### for the component, assigns value, and reloads
// Any instance of ###ZARF_COMPONENT_NAME### within a component will be replaced with that components name
func reloadComponentTemplate(component *v1alpha1.ZarfComponent) error {
	mappings := map[string]string{}
	mappings[v1alpha1.ZarfComponentName] = component.Name
	err := utils.ReloadYamlTemplate(component, mappings)
	if err != nil {
		return err
	}
	return nil
}

// reloadComponentTemplatesInPackage appends ###ZARF_COMPONENT_NAME###  for each component, assigns value, and reloads
func reloadComponentTemplatesInPackage(zarfPackage *v1alpha1.ZarfPackage) error {
	// iterate through components to and find all ###ZARF_COMPONENT_NAME, assign to component Name and value
	for i := range zarfPackage.Components {
		if err := reloadComponentTemplate(&zarfPackage.Components[i]); err != nil {
			return err
		}
	}
	return nil
}

func splitFile(srcPath string, chunkSize int) (err error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	// Ensure we close our sourcefile, even if we error out.
	defer func() {
		err2 := srcFile.Close()
		// Ignore if file is already closed
		if !errors.Is(err2, os.ErrClosed) {
			err = errors.Join(err, err2)
		}
	}()

	fi, err := srcFile.Stat()
	if err != nil {
		return err
	}

	title := fmt.Sprintf("[0/%d] MB bytes written", fi.Size()/1000/1000)
	progressBar := message.NewProgressBar(fi.Size(), title)
	defer func(progressBar *message.ProgressBar) {
		err2 := progressBar.Close()
		err = errors.Join(err, err2)
	}(progressBar)

	hash := sha256.New()
	fileCount := 0
	// TODO(mkcp): The inside of this loop should be wrapped in a closure so we can close the destination file each
	//   iteration as soon as we're done writing.
	for {
		path := fmt.Sprintf("%s.part%03d", srcPath, fileCount+1)
		dstFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, helpers.ReadAllWriteUser)
		if err != nil {
			return err
		}
		defer func(dstFile *os.File) {
			err2 := dstFile.Close()
			// Ignore if file is already closed
			if !errors.Is(err2, os.ErrClosed) {
				err = errors.Join(err, err2)
			}
		}(dstFile)

		written, copyErr := io.CopyN(dstFile, srcFile, int64(chunkSize))
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {
			return err
		}
		progressBar.Add(int(written))
		title := fmt.Sprintf("[%d/%d] MB bytes written", progressBar.GetCurrent()/1000/1000, fi.Size()/1000/1000)
		progressBar.Updatef(title)

		_, err = dstFile.Seek(0, io.SeekStart)
		if err != nil {
			return err
		}
		_, err = io.Copy(hash, dstFile)
		if err != nil {
			return err
		}

		// EOF error could be returned on 0 bytes written.
		if written == 0 {
			// NOTE(mkcp): We have to close the file before removing it or windows will break with a file-in-use err.
			err = dstFile.Close()
			if err != nil {
				return err
			}
			err = os.Remove(path)
			if err != nil {
				return err
			}
			break
		}

		fileCount++
		if errors.Is(copyErr, io.EOF) {
			break
		}
	}

	// Remove original file
	// NOTE(mkcp): We have to close the file before removing or windows can break with a file-in-use err.
	err = srcFile.Close()
	if err != nil {
		return err
	}
	err = os.Remove(srcPath)
	if err != nil {
		return err
	}

	// Write header file
	data := types.ZarfSplitPackageData{
		Count:     fileCount,
		Bytes:     fi.Size(),
		Sha256Sum: fmt.Sprintf("%x", hash.Sum(nil)),
	}
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("unable to marshal the split package data: %w", err)
	}
	path := fmt.Sprintf("%s.part000", srcPath)
	if err := os.WriteFile(path, b, helpers.ReadAllWriteUser); err != nil {
		return fmt.Errorf("unable to write the file %s: %w", path, err)
	}
	progressBar.Successf("Package split across %d files", fileCount+1)

	return nil
}
