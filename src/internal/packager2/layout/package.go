// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

package layout

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/defenseunicorns/pkg/helpers/v2"
	registryv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/mholt/archiver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/verify"

	"github.com/zarf-dev/zarf/src/api/v1alpha1"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/message"
	"github.com/zarf-dev/zarf/src/pkg/packager/sources"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/pkg/utils"
)

// PackageLayout manages the layout for a package.
type PackageLayout struct {
	dirPath string
	Pkg     v1alpha1.ZarfPackage
}

// PackageLayoutOptions are the options used when loading a package.
type PackageLayoutOptions struct {
	PublicKeyPath           string
	SkipSignatureValidation bool
	IsPartial               bool
}

// LoadFromTar unpacks the give compressed package and loads it.
func LoadFromTar(ctx context.Context, tarPath string, opt PackageLayoutOptions) (*PackageLayout, error) {
	dirPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	err = archiver.Walk(tarPath, func(f archiver.File) error {
		if f.IsDir() {
			return nil
		}
		header, ok := f.Header.(*tar.Header)
		if !ok {
			return fmt.Errorf("expected header to be *tar.Header but was %T", f.Header)
		}
		// If path has nested directories we want to create them.
		dir := filepath.Dir(header.Name)
		if dir != "." {
			err := os.MkdirAll(filepath.Join(dirPath, dir), helpers.ReadExecuteAllWriteUser)
			if err != nil {
				return err
			}
		}
		dst, err := os.Create(filepath.Join(dirPath, header.Name))
		if err != nil {
			return err
		}
		defer dst.Close()
		_, err = io.Copy(dst, f)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	p, err := LoadFromDir(ctx, dirPath, opt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// LoadFromDir loads and validates a package from the given directory path.
func LoadFromDir(ctx context.Context, dirPath string, opt PackageLayoutOptions) (*PackageLayout, error) {
	b, err := os.ReadFile(filepath.Join(dirPath, ZarfYAML))
	if err != nil {
		return nil, err
	}
	pkg, err := ParseZarfPackage(b)
	if err != nil {
		return nil, err
	}
	pkgLayout := &PackageLayout{
		dirPath: dirPath,
		Pkg:     pkg,
	}
	err = validatePackageIntegrity(pkgLayout, opt.IsPartial)
	if err != nil {
		return nil, err
	}
	err = validatePackageSignature(ctx, pkgLayout, opt.PublicKeyPath, opt.SkipSignatureValidation)
	if err != nil {
		return nil, err
	}
	return pkgLayout, nil
}

// Cleanup removes any temporary directories created.
func (p *PackageLayout) Cleanup() error {
	err := os.RemoveAll(p.dirPath)
	if err != nil {
		return err
	}
	return nil
}

// GetSBOM outputs the SBOM data from the package to the give destination path.
func (p *PackageLayout) GetSBOM(destPath string) (string, error) {
	path := filepath.Join(destPath, p.Pkg.Metadata.Name)
	err := archiver.Extract(filepath.Join(p.dirPath, SBOMTar), "", path)
	if err != nil {
		return "", err
	}
	return path, nil
}

// GetComponentDir returns a path to the directory in the given component.
func (p *PackageLayout) GetComponentDir(destPath, componentName string, ct ComponentDir) (string, error) {
	sourcePath := filepath.Join(p.dirPath, ComponentsDir, fmt.Sprintf("%s.tar", componentName))
	_, err := os.Stat(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("component %s does not exist in package: %w", componentName, err)
	}
	if err != nil {
		return "", err
	}
	tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	// TODO (phillebaba): We are not using archiver.Extract here because there is a bug in Windows where the files will not be extracted properly from nested directories.
	// https://github.com/zarf-dev/zarf/issues/3051
	err = archiver.Unarchive(sourcePath, tmpDir)
	if err != nil {
		return "", err
	}
	compPath := filepath.Join(tmpDir, componentName, string(ct))
	_, err = os.Stat(compPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("component %s could not access a %s directory: %w", componentName, ct, err)
	}
	if err != nil {
		return "", err
	}
	outPath := filepath.Join(destPath, string(ct))
	err = os.Rename(compPath, outPath)
	if err != nil {
		return "", err
	}
	return outPath, nil
}

// GetImage returns the image with the given reference in the package layout.
func (p *PackageLayout) GetImage(ref transform.Image) (registryv1.Image, error) {
	// Use the manifest within the index.json to load the specific image we want
	layoutPath := layout.Path(filepath.Join(p.dirPath, ImagesDir))
	imgIdx, err := layoutPath.ImageIndex()
	if err != nil {
		return nil, err
	}
	idxManifest, err := imgIdx.IndexManifest()
	if err != nil {
		return nil, err
	}
	// Search through all the manifests within this package until we find the annotation that matches our ref
	for _, manifest := range idxManifest.Manifests {
		if manifest.Annotations[ocispec.AnnotationBaseImageName] == ref.Reference ||
			// A backwards compatibility shim for older Zarf versions that would leave docker.io off of image annotations
			(manifest.Annotations[ocispec.AnnotationBaseImageName] == ref.Path+ref.TagOrDigest && ref.Host == "docker.io") {
			// This is the image we are looking for, load it and then return
			return layoutPath.Image(manifest.Digest)
		}
	}
	return nil, fmt.Errorf("unable to find the image %s", ref.Reference)
}

func (p *PackageLayout) Archive(ctx context.Context, dirPath string, maxPackageSize int) error {
	packageName := fmt.Sprintf("%s%s", sources.NameFromMetadata(&p.Pkg, false), sources.PkgSuffix(p.Pkg.Metadata.Uncompressed))
	tarballPath := filepath.Join(dirPath, packageName)
	err := os.Remove(tarballPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	message.Notef("Saving package to path %s", tarballPath)
	logger.From(ctx).Info("writing package to disk", "path", tarballPath)
	files, err := os.ReadDir(p.dirPath)
	if err != nil {
		return err
	}
	var filePaths []string
	for _, file := range files {
		filePaths = append(filePaths, filepath.Join(p.dirPath, file.Name()))
	}
	err = archiver.Archive(filePaths, tarballPath)
	if err != nil {
		return fmt.Errorf("unable to create package: %w", err)
	}
	fi, err := os.Stat(tarballPath)
	if err != nil {
		return fmt.Errorf("unable to read the package archive: %w", err)
	}
	// Convert Megabytes to bytes.
	chunkSize := maxPackageSize * 1000 * 1000
	// If a chunk size was specified and the package is larger than the chunk size, split it into chunks.
	if maxPackageSize > 0 && fi.Size() > int64(chunkSize) {
		if fi.Size()/int64(chunkSize) > 999 {
			return fmt.Errorf("unable to split the package archive into multiple files: must be less than 1,000 files")
		}
		// Remove any existing split files
		existingChunks, err := filepath.Glob(tarballPath + ".part*")
		if err != nil {
			return err
		}
		for _, chunk := range existingChunks {
			err := os.Remove(chunk)
			if err != nil {
				return err
			}
		}
		err = splitFile(tarballPath, chunkSize)
		if err != nil {
			return fmt.Errorf("unable to split the package archive into multiple files: %w", err)
		}
	}
	return nil
}

// Files returns a map off all the files in the package.
func (p *PackageLayout) Files() (map[string]string, error) {
	files := map[string]string{}
	err := filepath.Walk(p.dirPath, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(p.dirPath, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		files[path] = name
		return err
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func validatePackageIntegrity(pkgLayout *PackageLayout, isPartial bool) error {
	_, err := os.Stat(filepath.Join(pkgLayout.dirPath, ZarfYAML))
	if err != nil {
		return err
	}
	_, err = os.Stat(filepath.Join(pkgLayout.dirPath, Checksums))
	if err != nil {
		return err
	}
	err = helpers.SHAsMatch(filepath.Join(pkgLayout.dirPath, Checksums), pkgLayout.Pkg.Metadata.AggregateChecksum)
	if err != nil {
		return err
	}

	packageFiles, err := pkgLayout.Files()
	if err != nil {
		return err
	}
	// Remove files which are not in the checksums.
	delete(packageFiles, filepath.Join(pkgLayout.dirPath, ZarfYAML))
	delete(packageFiles, filepath.Join(pkgLayout.dirPath, Checksums))
	delete(packageFiles, filepath.Join(pkgLayout.dirPath, Signature))

	b, err := os.ReadFile(filepath.Join(pkgLayout.dirPath, Checksums))
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		// If the line is empty (i.e. there is no checksum) simply skip it, this can result from a package with no images/components.
		if line == "" {
			continue
		}

		split := strings.Split(line, " ")
		if len(split) != 2 {
			return fmt.Errorf("invalid checksum line: %s", line)
		}
		sha := split[0]
		rel := split[1]
		if sha == "" || rel == "" {
			return fmt.Errorf("invalid checksum line: %s", line)
		}

		path := filepath.Join(pkgLayout.dirPath, rel)
		_, ok := packageFiles[path]
		if !ok && isPartial {
			delete(packageFiles, path)
			continue
		}
		if !ok {
			return fmt.Errorf("file %s from checksum missing in layout", rel)
		}
		err = helpers.SHAsMatch(path, sha)
		if err != nil {
			return err
		}
		delete(packageFiles, path)
	}

	if len(packageFiles) > 0 {
		// TODO (phillebaba): Replace with maps.Keys after upgrading to Go 1.23.
		filePaths := []string{}
		for k := range packageFiles {
			filePaths = append(filePaths, k)
		}
		return fmt.Errorf("package contains additional files not present in the checksum %s", strings.Join(filePaths, ", "))
	}

	return nil
}

func validatePackageSignature(ctx context.Context, pkgLayout *PackageLayout, publicKeyPath string, skipSignatureValidation bool) error {
	if skipSignatureValidation {
		return nil
	}

	signaturePath := filepath.Join(pkgLayout.dirPath, Signature)
	sigExist := true
	_, err := os.Stat(signaturePath)
	if err != nil {
		sigExist = false
	}
	if !sigExist && publicKeyPath == "" {
		// Nobody was expecting a signature, so we can just return
		return nil
	} else if sigExist && publicKeyPath == "" {
		return errors.New("package is signed but no key was provided")
	} else if !sigExist && publicKeyPath != "" {
		return errors.New("a key was provided but the package is not signed")
	}

	keyOptions := options.KeyOpts{KeyRef: publicKeyPath}
	cmd := &verify.VerifyBlobCmd{
		KeyOpts:    keyOptions,
		SigRef:     signaturePath,
		IgnoreSCT:  true,
		Offline:    true,
		IgnoreTlog: true,
	}
	err = cmd.Exec(ctx, filepath.Join(pkgLayout.dirPath, ZarfYAML))
	if err != nil {
		return fmt.Errorf("package signature did not match the provided key: %w", err)
	}
	return nil
}
