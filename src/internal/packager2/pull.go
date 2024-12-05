// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

package packager2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/defenseunicorns/pkg/helpers/v2"
	"github.com/defenseunicorns/pkg/oci"
	goyaml "github.com/goccy/go-yaml"
	"github.com/mholt/archiver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/api/v1alpha1"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/layout"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/packager/filters"
	"github.com/zarf-dev/zarf/src/pkg/packager/sources"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/pkg/zoci"
)

// Check the thing
// TODO: write unit tests
func CheckIfPackageExists(ctx context.Context, src string, dir string) (bool, error) {
	newRemote, err := zoci.NewRemote(ctx, src, oci.PlatformForArch(config.GetArch()))

	if err != nil {
		return false, err
	}

	pkg, err := newRemote.FetchZarfYAML(ctx)
	if err != nil {
		return false, err
	}

	// Zarf init start with "zarf-init" / not all packages have versions etc.
	packageName := fmt.Sprintf("%s%s", sources.NameFromMetadata(&pkg, false), sources.PkgSuffix(pkg.Metadata.Uncompressed))

	if packageName == "" {
		return false, nil
	}
	fullPath := filepath.Join(dir, packageName)
	var localPkg v1alpha1.ZarfPackage

	err = archiver.Walk(fullPath, func(f archiver.File) error {
		if f.Name() == layout.ZarfYAML {
			b, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			if err := goyaml.Unmarshal(b, &localPkg); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return false, err
	}

	if localPkg.Metadata.AggregateChecksum != pkg.Metadata.AggregateChecksum {
		return false, nil
	}
	return true, nil
}

// Pull fetches the Zarf package from the given sources.
func Pull(ctx context.Context, src, dir, shasum string, filter filters.ComponentFilterStrategy) error {
	u, err := url.Parse(src)
	l := logger.From(ctx)

	if err != nil {
		return err
	}
	if u.Scheme == "" {
		return errors.New("scheme cannot be empty")
	}
	if u.Host == "" {
		return errors.New("host cannot be empty")
	}

	tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return err
	}
	defer os.Remove(tmpDir)
	tmpPath := filepath.Join(tmpDir, "data.tar.zst")

	switch u.Scheme {
	case "oci":
		localPkgExists, err := CheckIfPackageExists(ctx, src, dir)
		if err != nil {
			return err
		}
		if localPkgExists {
			l.Info("package already exists locally in directory", "Directory", dir)
			return nil
		}
		_, err = pullOCI(ctx, src, tmpPath, shasum, filter)
		if err != nil {
			return err
		}

	case "http", "https":
		err := pullHTTP(ctx, src, tmpPath, shasum)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown scheme %s", u.Scheme)
	}

	name, err := nameFromMetadata(tmpPath)
	if err != nil {
		return err
	}
	tarPath := filepath.Join(dir, name)
	err = os.Remove(tarPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dstFile, err := os.Create(tarPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	srcFile, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}
	return nil
}

func pullOCI(ctx context.Context, src, tarPath, shasum string, filter filters.ComponentFilterStrategy) (bool, error) {
	tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return false, err
	}
	defer os.Remove(tmpDir)
	if shasum != "" {
		src = fmt.Sprintf("%s@sha256:%s", src, shasum)
	}
	arch := config.GetArch()
	remote, err := zoci.NewRemote(ctx, src, oci.PlatformForArch(arch))
	if err != nil {
		return false, err
	}
	desc, err := remote.ResolveRoot(ctx)
	if err != nil {
		return false, fmt.Errorf("could not fetch images index: %w", err)
	}
	layersToPull := []ocispec.Descriptor{}
	isPartial := false
	if supportsFiltering(desc.Platform) {
		root, err := remote.FetchRoot(ctx)
		if err != nil {
			return false, err
		}
		if len(root.Layers) != len(layersToPull) {
			isPartial = true
		}
		pkg, err := remote.FetchZarfYAML(ctx)
		if err != nil {
			return false, err
		}
		pkg.Components, err = filter.Apply(pkg)
		if err != nil {
			return false, err
		}
		layersToPull, err = remote.LayersFromRequestedComponents(ctx, pkg.Components)
		if err != nil {
			return false, err
		}
	}
	_, err = remote.PullPackage(ctx, tmpDir, config.CommonOptions.OCIConcurrency, layersToPull...)
	if err != nil {
		return false, err
	}
	allTheLayers, err := filepath.Glob(filepath.Join(tmpDir, "*"))
	if err != nil {
		return false, err
	}
	err = archiver.Archive(allTheLayers, tarPath)
	if err != nil {
		return false, err
	}
	return isPartial, nil
}

func pullHTTP(ctx context.Context, src, tarPath, shasum string) error {
	if shasum == "" {
		return errors.New("shasum cannot be empty")
	}
	f, err := os.Create(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, err := io.Copy(io.Discard, resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected http response status code %s for source %s", resp.Status, src)
	}
	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	received, err := helpers.GetSHA256OfFile(tarPath)
	if err != nil {
		return err
	}
	if received != shasum {
		return fmt.Errorf("shasum mismatch for file %s, expected %s but got %s", tarPath, shasum, received)
	}
	return nil
}

func nameFromMetadata(path string) (string, error) {
	var pkg v1alpha1.ZarfPackage
	err := archiver.Walk(path, func(f archiver.File) error {
		if f.Name() == layout.ZarfYAML {
			b, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			if err := goyaml.Unmarshal(b, &pkg); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if pkg.Metadata.Name == "" {
		return "", fmt.Errorf("%s does not contain a zarf.yaml", path)
	}

	arch := config.GetArch(pkg.Metadata.Architecture, pkg.Build.Architecture)
	if pkg.Build.Architecture == zoci.SkeletonArch {
		arch = zoci.SkeletonArch
	}

	var name string
	switch pkg.Kind {
	case v1alpha1.ZarfInitConfig:
		name = fmt.Sprintf("zarf-init-%s", arch)
	case v1alpha1.ZarfPackageConfig:
		name = fmt.Sprintf("zarf-package-%s-%s", pkg.Metadata.Name, arch)
	default:
		name = fmt.Sprintf("zarf-%s-%s", strings.ToLower(string(pkg.Kind)), arch)
	}
	if pkg.Build.Differential {
		name = fmt.Sprintf("%s-%s-differential-%s", name, pkg.Build.DifferentialPackageVersion, pkg.Metadata.Version)
	} else if pkg.Metadata.Version != "" {
		name = fmt.Sprintf("%s-%s", name, pkg.Metadata.Version)
	}
	return fmt.Sprintf("%s.tar.zst", name), nil
}

func supportsFiltering(platform *ocispec.Platform) bool {
	if platform == nil {
		return false
	}
	skeletonPlatform := zoci.PlatformForSkeleton()
	if platform.Architecture == skeletonPlatform.Architecture && platform.OS == skeletonPlatform.OS {
		return false
	}
	return true
}
