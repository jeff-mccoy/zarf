// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package helm contains operations for working with helm charts.
package helm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zarf-dev/zarf/src/pkg/logger"

	"github.com/defenseunicorns/pkg/helpers/v2"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/client-go/util/homedir"

	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/internal/git"
	"github.com/zarf-dev/zarf/src/pkg/message"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/pkg/utils"
)

// PackageChart creates a chart archive from a path to a chart on the host os and builds chart dependencies
func (h *Helm) PackageChart(ctx context.Context, cosignKeyPath string) error {
	if len(h.chart.URL) > 0 {
		url, refPlain, err := transform.GitURLSplitRef(h.chart.URL)
		// check if the chart is a git url with a ref (if an error is returned url will be empty)
		isGitURL := strings.HasSuffix(url, ".git")
		if err != nil {
			// TODO(mkcp): Remove message on logger release
			message.Debugf("unable to parse the url, continuing with %s", h.chart.URL)
			logger.From(ctx).Debug("unable to parse the url, continuing", "url", h.chart.URL)
		}

		if isGitURL {
			// if it is a git url append chart version as if its a tag
			if refPlain == "" {
				h.chart.URL = fmt.Sprintf("%s@%s", h.chart.URL, h.chart.Version)
			}

			err = h.PackageChartFromGit(ctx, cosignKeyPath)
			if err != nil {
				return fmt.Errorf("unable to pull the chart %q from git: %w", h.chart.Name, err)
			}
		} else {
			err = h.DownloadPublishedChart(ctx, cosignKeyPath)
			if err != nil {
				return fmt.Errorf("unable to download the published chart %q: %w", h.chart.Name, err)
			}
		}
	} else {
		err := h.PackageChartFromLocalFiles(ctx, cosignKeyPath)
		if err != nil {
			return fmt.Errorf("unable to package the %q chart: %w", h.chart.Name, err)
		}
	}
	return nil
}

// PackageChartFromLocalFiles creates a chart archive from a path to a chart on the host os.
func (h *Helm) PackageChartFromLocalFiles(ctx context.Context, cosignKeyPath string) error {
	l := logger.From(ctx)
	l.Info("processing local helm chart",
		"name", h.chart.Name,
		"version", h.chart.Version,
		"path", h.chart.LocalPath,
	)
	// TODO(mkcp): Remove message on logger release
	spinner := message.NewProgressSpinner("Processing helm chart %s:%s from %s", h.chart.Name, h.chart.Version, h.chart.LocalPath)
	defer spinner.Stop()

	// Load and validate the chart
	cl, _, err := h.loadAndValidateChart(h.chart.LocalPath)
	if err != nil {
		return err
	}

	// Handle the chart directory or tarball
	var saved string
	temp := filepath.Join(h.chartPath, "temp")
	if _, ok := cl.(loader.DirLoader); ok {
		err = h.buildChartDependencies()
		if err != nil {
			return fmt.Errorf("unable to build dependencies for the chart: %w", err)
		}

		client := action.NewPackage()

		client.Destination = temp
		saved, err = client.Run(h.chart.LocalPath, nil)
	} else {
		saved = filepath.Join(temp, filepath.Base(h.chart.LocalPath))
		err = helpers.CreatePathAndCopy(h.chart.LocalPath, saved)
	}
	defer func(l *slog.Logger) {
		err := os.RemoveAll(temp)
		if err != nil {
			l.Error(err.Error())
		}
	}(l)

	if err != nil {
		return fmt.Errorf("unable to save the archive and create the package %s: %w", saved, err)
	}

	// Finalize the chart
	err = h.finalizeChartPackage(ctx, saved, cosignKeyPath)
	if err != nil {
		return err
	}

	spinner.Success()

	l.Debug("done processing local helm chart",
		"name", h.chart.Name,
		"version", h.chart.Version,
		"path", h.chart.LocalPath,
	)
	return nil
}

// PackageChartFromGit is a special implementation of chart archiving that supports the https://p1.dso.mil/#/products/big-bang/ model.
func (h *Helm) PackageChartFromGit(ctx context.Context, cosignKeyPath string) error {
	l := logger.From(ctx)
	l.Info("processing Helm chart", "name", h.chart.Name)
	// TODO(mkcp): Remove message on logger release
	spinner := message.NewProgressSpinner("Processing Helm chart %s", h.chart.Name)
	defer spinner.Stop()

	// Retrieve the repo containing the chart
	gitPath, err := DownloadChartFromGitToTemp(ctx, h.chart.URL)
	if err != nil {
		return err
	}
	defer func(l *slog.Logger) {
		if err := os.RemoveAll(gitPath); err != nil {
			l.Error(err.Error())
		}
	}(l)

	// Set the directory for the chart and package it
	h.chart.LocalPath = filepath.Join(gitPath, h.chart.GitPath)
	return h.PackageChartFromLocalFiles(ctx, cosignKeyPath)
}

// DownloadPublishedChart loads a specific chart version from a remote repo.
func (h *Helm) DownloadPublishedChart(ctx context.Context, cosignKeyPath string) error {
	l := logger.From(ctx)
	l.Info("processing Helm chart",
		"name", h.chart.Name,
		"version", h.chart.Version,
		"repo", h.chart.URL,
	)
	start := time.Now()
	// TODO(mkcp): Remove message on logger release
	spinner := message.NewProgressSpinner("Processing Helm chart %s:%s from repo %s", h.chart.Name, h.chart.Version, h.chart.URL)
	defer spinner.Stop()

	// Set up the helm pull config
	pull := action.NewPull()
	pull.Settings = cli.New()

	var (
		regClient *registry.Client
		chartURL  string
		err       error
	)
	repoFile, err := repo.LoadFile(pull.Settings.RepositoryConfig)

	// Not returning the error here since the repo file is only needed if we are pulling from a repo that requires authentication
	if err != nil {
		// TODO(mkcp): Remove message on logger release
		message.Debugf("Unable to load the repo file at %q: %s", pull.Settings.RepositoryConfig, err.Error())
		l.Debug("unable to load the repo file",
			"path", pull.Settings.RepositoryConfig,
			"error", err.Error(),
		)
	}

	var username string
	var password string

	// Handle OCI registries
	if registry.IsOCI(h.chart.URL) {
		regClient, err = registry.NewClient(registry.ClientOptEnableCache(true))
		if err != nil {
			return fmt.Errorf("unable to create the new registry client: %w", err)
		}
		chartURL = h.chart.URL
		// Explicitly set the pull version for OCI
		pull.Version = h.chart.Version
	} else {
		chartName := h.chart.Name
		if h.chart.RepoName != "" {
			chartName = h.chart.RepoName
		}

		if repoFile != nil {
			// TODO: @AustinAbro321 Currently this selects the last repo with the same url
			// We should introduce a new field in zarf to allow users to specify the local repo they want
			for _, repo := range repoFile.Repositories {
				if repo.URL == h.chart.URL {
					username = repo.Username
					password = repo.Password
				}
			}
		}

		chartURL, err = repo.FindChartInAuthRepoURL(h.chart.URL, username, password, chartName, h.chart.Version, pull.CertFile, pull.KeyFile, pull.CaFile, getter.All(pull.Settings))
		if err != nil {
			return fmt.Errorf("unable to pull the helm chart: %w", err)
		}
	}

	// Set up the chart chartDownloader
	chartDownloader := downloader.ChartDownloader{
		Out:            spinner,
		RegistryClient: regClient,
		// TODO: Further research this with regular/OCI charts
		Verify:  downloader.VerifyNever,
		Getters: getter.All(pull.Settings),
		Options: []getter.Option{
			getter.WithInsecureSkipVerifyTLS(config.CommonOptions.InsecureSkipTLSVerify),
			getter.WithBasicAuth(username, password),
		},
	}

	// Download the file into a temp directory since we don't control what name helm creates here
	temp := filepath.Join(h.chartPath, "temp")
	if err = helpers.CreateDirectory(temp, helpers.ReadWriteExecuteUser); err != nil {
		return fmt.Errorf("unable to create helm chart temp directory: %w", err)
	}
	defer func(l *slog.Logger) {
		err := os.RemoveAll(temp)
		if err != nil {
			l.Error(err.Error())
		}
	}(l)

	saved, _, err := chartDownloader.DownloadTo(chartURL, pull.Version, temp)
	if err != nil {
		return fmt.Errorf("unable to download the helm chart: %w", err)
	}

	// Validate the chart
	_, _, err = h.loadAndValidateChart(saved)
	if err != nil {
		return err
	}

	// Finalize the chart
	err = h.finalizeChartPackage(ctx, saved, cosignKeyPath)
	if err != nil {
		return err
	}

	spinner.Success()
	l.Debug("done downloading helm chart",
		"name", h.chart.Name,
		"version", h.chart.Version,
		"repo", h.chart.URL,
		"duration", time.Since(start),
	)
	return nil
}

// DownloadChartFromGitToTemp downloads a chart from git into a temp directory
func DownloadChartFromGitToTemp(ctx context.Context, url string) (string, error) {
	path, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return "", fmt.Errorf("unable to create tmpdir: %w", err)
	}
	repository, err := git.Clone(ctx, path, url, true)
	if err != nil {
		return "", err
	}
	return repository.Path(), nil
}

func (h *Helm) finalizeChartPackage(ctx context.Context, saved, cosignKeyPath string) error {
	// Ensure the name is consistent for deployments
	destinationTarball := StandardName(h.chartPath, h.chart) + ".tgz"
	err := os.Rename(saved, destinationTarball)
	if err != nil {
		return fmt.Errorf("unable to save the final chart tarball: %w", err)
	}

	err = h.packageValues(ctx, cosignKeyPath)
	if err != nil {
		return fmt.Errorf("unable to process the values for the package: %w", err)
	}
	return nil
}

func (h *Helm) packageValues(ctx context.Context, cosignKeyPath string) error {
	for valuesIdx, path := range h.chart.ValuesFiles {
		dst := StandardValuesName(h.valuesPath, h.chart, valuesIdx)

		if helpers.IsURL(path) {
			if err := utils.DownloadToFile(ctx, path, dst, cosignKeyPath); err != nil {
				return fmt.Errorf(lang.ErrDownloading, path, err.Error())
			}
		} else {
			if err := helpers.CreatePathAndCopy(path, dst); err != nil {
				return fmt.Errorf("unable to copy chart values file %s: %w", path, err)
			}
		}
	}

	return nil
}

// buildChartDependencies builds the helm chart dependencies
func (h *Helm) buildChartDependencies() error {
	// Download and build the specified dependencies
	regClient, err := registry.NewClient(registry.ClientOptEnableCache(true))
	if err != nil {
		return fmt.Errorf("unable to create a new registry client: %w", err)
	}

	h.settings = cli.New()
	defaultKeyring := filepath.Join(homedir.HomeDir(), ".gnupg", "pubring.gpg")
	if v, ok := os.LookupEnv("GNUPGHOME"); ok {
		defaultKeyring = filepath.Join(v, "pubring.gpg")
	}

	man := &downloader.Manager{
		Out:            &message.DebugWriter{},
		ChartPath:      h.chart.LocalPath,
		Getters:        getter.All(h.settings),
		RegistryClient: regClient,

		RepositoryConfig: h.settings.RepositoryConfig,
		RepositoryCache:  h.settings.RepositoryCache,
		Debug:            false,
		Verify:           downloader.VerifyIfPossible,
		Keyring:          defaultKeyring,
	}

	// Build the deps from the helm chart
	err = man.Build()
	var notFoundErr *downloader.ErrRepoNotFound
	if errors.As(err, &notFoundErr) {
		// If we encounter a repo not found error point the user to `zarf tools helm repo add`
		// TODO(mkcp): Remove message on logger release
		message.Warnf("%s. Please add the missing repo(s) via the following:", notFoundErr.Error())
		for _, repository := range notFoundErr.Repos {
			// TODO(mkcp): Remove message on logger release
			message.ZarfCommand(fmt.Sprintf("tools helm repo add <your-repo-name> %s", repository))
		}
		return err
	}
	if err != nil {
		// TODO(mkcp): Remove message on logger release
		message.ZarfCommand("tools helm dependency build --verify")
		message.Warnf("Unable to perform a rebuild of Helm dependencies: %s", err.Error())
		return err
	}
	return nil
}

func (h *Helm) loadAndValidateChart(location string) (loader.ChartLoader, *chart.Chart, error) {
	// Validate the chart
	cl, err := loader.Loader(location)
	if err != nil {
		return cl, nil, fmt.Errorf("unable to load the chart from %s: %w", location, err)
	}

	chart, err := cl.Load()
	if err != nil {
		return cl, chart, fmt.Errorf("validation failed for chart from %s: %w", location, err)
	}

	return cl, chart, nil
}
