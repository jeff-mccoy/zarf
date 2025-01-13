// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package helm contains operations for working with helm charts.
package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zarf-dev/zarf/src/pkg/logger"

	"github.com/Masterminds/semver/v3"
	"github.com/avast/retry-go/v4"
	plutoversionsfile "github.com/fairwindsops/pluto/v5"
	plutoapi "github.com/fairwindsops/pluto/v5/pkg/api"
	goyaml "github.com/goccy/go-yaml"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/internal/healthchecks"
	"github.com/zarf-dev/zarf/src/pkg/message"
	"github.com/zarf-dev/zarf/src/types"
)

// Use same default as Helm CLI does.
const maxHelmHistory = 10

// InstallOrUpgradeChart performs a helm install of the given chart.
func (h *Helm) InstallOrUpgradeChart(ctx context.Context) (types.ConnectStrings, string, error) {
	l := logger.From(ctx)
	start := time.Now()
	source := h.chart.URL
	if source == "" {
		source = "Zarf-generated"
	}
	spinner := message.NewProgressSpinner("Processing helm chart %s:%s source: %s",
		h.chart.Name,
		h.chart.Version,
		source)
	defer spinner.Stop()
	l.Info("processing Helm chart", "name", h.chart.Name, "version", h.chart.Version, "source", source)

	// If no release name is specified, use the chart name.
	if h.chart.ReleaseName == "" {
		h.chart.ReleaseName = h.chart.Name
	}

	// Setup K8s connection.
	err := h.createActionConfig(ctx, h.chart.Namespace, spinner)
	if err != nil {
		return nil, "", fmt.Errorf("unable to initialize the K8s client: %w", err)
	}

	postRender, err := h.newRenderer(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("unable to create helm renderer: %w", err)
	}

	histClient := action.NewHistory(h.actionConfig)
	var release *release.Release

	helmCtx, helmCtxCancel := context.WithTimeout(ctx, h.timeout)
	defer helmCtxCancel()

	err = retry.Do(func() error {
		var err error

		releases, histErr := histClient.Run(h.chart.ReleaseName)

		spinner.Updatef("Checking for existing helm deployment")
		l.Debug("checking for existing helm deployment")

		if errors.Is(histErr, driver.ErrReleaseNotFound) {
			// No prior release, try to install it.
			spinner.Updatef("Attempting chart installation")
			l.Info("performing Helm install", "chart", h.chart.Name)

			release, err = h.installChart(helmCtx, postRender)
		} else if histErr == nil && len(releases) > 0 {
			// Otherwise, there is a prior release so upgrade it.
			spinner.Updatef("Attempting chart upgrade")
			l.Info("performing Helm upgrade", "chart", h.chart.Name)

			lastRelease := releases[len(releases)-1]

			release, err = h.upgradeChart(helmCtx, lastRelease, postRender)
		} else {
			return fmt.Errorf("unable to verify the chart installation status: %w", histErr)
		}

		if err != nil {
			return err
		}

		spinner.Success()
		return nil
	}, retry.Context(ctx), retry.Attempts(uint(h.retries)), retry.Delay(500*time.Millisecond))
	if err != nil {
		removeMsg := "if you need to remove the failed chart, use `zarf package remove`"
		installErr := fmt.Errorf("unable to install chart after %d attempts: %w: %s", h.retries, err, removeMsg)

		releases, _ := histClient.Run(h.chart.ReleaseName)
		previouslyDeployedVersion := 0

		// Check for previous releases that successfully deployed
		for _, release := range releases {
			if release.Info.Status == "deployed" {
				previouslyDeployedVersion = release.Version
			}
		}

		// No prior releases means this was an initial install.
		if previouslyDeployedVersion == 0 {
			return nil, "", installErr
		}

		// Attempt to rollback on a failed upgrade.
		spinner.Updatef("Performing chart rollback")
		l.Info("performing Helm rollback", "chart", h.chart.Name)
		err = h.rollbackChart(h.chart.ReleaseName, previouslyDeployedVersion)
		if err != nil {
			return nil, "", fmt.Errorf("%w: unable to rollback: %w", installErr, err)
		}
		return nil, "", installErr
	}

	resourceList, err := h.actionConfig.KubeClient.Build(bytes.NewBufferString(release.Manifest), true)
	if err != nil {
		return nil, "", fmt.Errorf("unable to build the resource list: %w", err)
	}

	runtimeObjs := []runtime.Object{}
	for _, resource := range resourceList {
		runtimeObjs = append(runtimeObjs, resource.Object)
	}
	if !h.chart.NoWait {
		// Ensure we don't go past the timeout by using a context initialized with the helm timeout
		spinner.Updatef("Running health checks")
		l.Info("running health checks", "chart", h.chart.Name)
		if err := healthchecks.WaitForReadyRuntime(helmCtx, h.cluster.Watcher, runtimeObjs); err != nil {
			return nil, "", err
		}
	}
	spinner.Success()
	l.Debug("done processing Helm chart", "name", h.chart.Name, "duration", time.Since(start))

	// return any collected connect strings for zarf connect.
	return postRender.connectStrings, h.chart.ReleaseName, nil
}

// TemplateChart generates a helm template from a given chart.
func (h *Helm) TemplateChart(ctx context.Context) (manifest string, chartValues chartutil.Values, err error) {
	l := logger.From(ctx)
	spinner := message.NewProgressSpinner("Templating helm chart %s", h.chart.Name)
	defer spinner.Stop()
	l.Debug("templating helm chart", "name", h.chart.Name)

	err = h.createActionConfig(ctx, h.chart.Namespace, spinner)

	// Setup K8s connection.
	if err != nil {
		return "", nil, fmt.Errorf("unable to initialize the K8s client: %w", err)
	}

	// Bind the helm action.
	client := action.NewInstall(h.actionConfig)

	client.DryRun = true
	client.Replace = true // Skip the name check.
	client.ClientOnly = true
	client.IncludeCRDs = true
	// TODO: Further research this with regular/OCI charts
	client.Verify = false
	client.InsecureSkipTLSverify = config.CommonOptions.InsecureSkipTLSVerify
	if h.kubeVersion != "" {
		parsedKubeVersion, err := chartutil.ParseKubeVersion(h.kubeVersion)
		if err != nil {
			return "", nil, fmt.Errorf("invalid kube version %s: %w", h.kubeVersion, err)
		}
		client.KubeVersion = parsedKubeVersion
	}
	client.ReleaseName = h.chart.ReleaseName

	// If no release name is specified, use the chart name.
	if client.ReleaseName == "" {
		client.ReleaseName = h.chart.Name
	}

	// Namespace must be specified.
	client.Namespace = h.chart.Namespace

	loadedChart, chartValues, err := h.loadChartData()
	if err != nil {
		return "", nil, fmt.Errorf("unable to load chart data: %w", err)
	}

	client.PostRenderer, err = h.newRenderer(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("unable to create helm renderer: %w", err)
	}

	// Perform the loadedChart installation.
	templatedChart, err := client.RunWithContext(ctx, loadedChart, chartValues)
	if err != nil {
		return "", nil, fmt.Errorf("error generating helm chart template: %w", err)
	}

	manifest = templatedChart.Manifest

	for _, hook := range templatedChart.Hooks {
		manifest += fmt.Sprintf("\n---\n%s", hook.Manifest)
	}

	spinner.Success()

	return manifest, chartValues, nil
}

// RemoveChart removes a chart from the cluster.
func (h *Helm) RemoveChart(ctx context.Context, namespace string, name string, spinner *message.Spinner) error {
	// Establish a new actionConfig for the namespace.
	_ = h.createActionConfig(ctx, namespace, spinner)
	// Perform the uninstall.
	response, err := h.uninstallChart(name)
	message.Debug(response)
	logger.From(ctx).Debug("chart uninstalled", "response", response)
	return err
}

// UpdateReleaseValues updates values for a given chart release
// (note: this only works on single-deep charts, charts with dependencies (like loki-stack) will not work)
func (h *Helm) UpdateReleaseValues(ctx context.Context, updatedValues map[string]interface{}) error {
	l := logger.From(ctx)
	spinner := message.NewProgressSpinner("Updating values for helm release %s", h.chart.ReleaseName)
	defer spinner.Stop()
	l.Debug("updating values for helm release", "name", h.chart.ReleaseName)

	err := h.createActionConfig(ctx, h.chart.Namespace, spinner)
	if err != nil {
		return fmt.Errorf("unable to initialize the K8s client: %w", err)
	}

	postRender, err := h.newRenderer(ctx)
	if err != nil {
		return fmt.Errorf("unable to create helm renderer: %w", err)
	}

	histClient := action.NewHistory(h.actionConfig)
	histClient.Max = 1
	releases, histErr := histClient.Run(h.chart.ReleaseName)
	if histErr == nil && len(releases) > 0 {
		lastRelease := releases[len(releases)-1]

		// Setup a new upgrade action
		client := action.NewUpgrade(h.actionConfig)

		// Let each chart run for the default timeout.
		client.Timeout = h.timeout

		client.SkipCRDs = true

		// Namespace must be specified.
		client.Namespace = h.chart.Namespace

		// Post-processing our manifests to apply vars and run zarf helm logic in cluster
		client.PostRenderer = postRender

		// Set reuse values to only override the values we are explicitly given
		client.ReuseValues = true

		// Wait for the update operation to successfully complete
		client.Wait = true

		// Perform the loadedChart upgrade.
		_, err = client.RunWithContext(ctx, h.chart.ReleaseName, lastRelease.Chart, updatedValues)
		if err != nil {
			return err
		}

		spinner.Success()

		return nil
	}

	return fmt.Errorf("unable to find the %s helm release", h.chart.ReleaseName)
}

func (h *Helm) installChart(ctx context.Context, postRender *renderer) (*release.Release, error) {
	// Bind the helm action.
	client := action.NewInstall(h.actionConfig)

	// Let each chart run for the default timeout.
	client.Timeout = h.timeout

	// Default helm behavior for Zarf is to wait for the resources to deploy, NoWait overrides that for special cases (such as data-injection).
	client.Wait = !h.chart.NoWait

	// We need to include CRDs or operator installations will fail spectacularly.
	client.SkipCRDs = false

	// Must be unique per-namespace and < 53 characters. @todo: restrict helm loadedChart name to this.
	client.ReleaseName = h.chart.ReleaseName

	client.SkipSchemaValidation = !h.chart.ShouldRunSchemaValidation()

	// Namespace must be specified.
	client.Namespace = h.chart.Namespace

	// Post-processing our manifests to apply vars and run zarf helm logic in cluster
	client.PostRenderer = postRender

	loadedChart, chartValues, err := h.loadChartData()
	if err != nil {
		return nil, fmt.Errorf("unable to load chart data: %w", err)
	}

	// Perform the loadedChart installation.
	return client.RunWithContext(ctx, loadedChart, chartValues)
}

func (h *Helm) upgradeChart(ctx context.Context, lastRelease *release.Release, postRender *renderer) (*release.Release, error) {
	// Migrate any deprecated APIs (if applicable)
	err := h.migrateDeprecatedAPIs(ctx, lastRelease)
	if err != nil {
		return nil, fmt.Errorf("unable to check for API deprecations: %w", err)
	}

	// Setup a new upgrade action
	client := action.NewUpgrade(h.actionConfig)

	// Let each chart run for the default timeout.
	client.Timeout = h.timeout

	// Default helm behavior for Zarf is to wait for the resources to deploy, NoWait overrides that for special cases (such as data-injection).
	client.Wait = !h.chart.NoWait

	client.SkipCRDs = true

	client.SkipSchemaValidation = !h.chart.ShouldRunSchemaValidation()

	// Namespace must be specified.
	client.Namespace = h.chart.Namespace

	// Post-processing our manifests to apply vars and run zarf helm logic in cluster
	client.PostRenderer = postRender

	client.MaxHistory = maxHelmHistory

	loadedChart, chartValues, err := h.loadChartData()
	if err != nil {
		return nil, fmt.Errorf("unable to load chart data: %w", err)
	}

	// Perform the loadedChart upgrade.
	return client.RunWithContext(ctx, h.chart.ReleaseName, loadedChart, chartValues)
}

func (h *Helm) rollbackChart(name string, version int) error {
	client := action.NewRollback(h.actionConfig)
	client.CleanupOnFail = true
	client.Force = true
	client.Wait = true
	client.Timeout = h.timeout
	client.Version = version
	client.MaxHistory = maxHelmHistory
	return client.Run(name)
}

func (h *Helm) uninstallChart(name string) (*release.UninstallReleaseResponse, error) {
	client := action.NewUninstall(h.actionConfig)
	client.KeepHistory = false
	client.Wait = true
	client.Timeout = h.timeout
	return client.Run(name)
}

func (h *Helm) loadChartData() (*chart.Chart, chartutil.Values, error) {
	var (
		loadedChart *chart.Chart
		chartValues chartutil.Values
		err         error
	)

	if h.chartOverride == nil {
		// If there is no override, get the chart and values info.
		loadedChart, err = h.loadChartFromTarball()
		if err != nil {
			return nil, nil, fmt.Errorf("unable to load chart tarball: %w", err)
		}

		chartValues, err = h.parseChartValues()
		if err != nil {
			return loadedChart, nil, fmt.Errorf("unable to parse chart values: %w", err)
		}
	} else {
		// Otherwise, use the overrides instead.
		loadedChart = h.chartOverride
		chartValues = h.valuesOverrides
	}

	return loadedChart, chartValues, nil
}

func (h *Helm) migrateDeprecatedAPIs(ctx context.Context, latestRelease *release.Release) error {
	// Get the Kubernetes version from the current cluster
	kubeVersion, err := h.cluster.Clientset.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	kubeGitVersion, err := semver.NewVersion(kubeVersion.String())
	if err != nil {
		return err
	}

	// Use helm to re-split the manifest bytes (same call used by helm to pass this data to postRender)
	_, resources, err := releaseutil.SortManifests(map[string]string{"manifest": latestRelease.Manifest}, nil, releaseutil.InstallOrder)

	if err != nil {
		return fmt.Errorf("error re-rendering helm output: %w", err)
	}

	modifiedManifest := ""
	modified := false

	// Loop over the resources from the lastRelease manifest to check for deprecations
	for _, resource := range resources {
		// parse to unstructured to have access to more data than just the name
		rawData := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(resource.Content), rawData); err != nil {
			return fmt.Errorf("failed to unmarshal manifest: %w", err)
		}

		rawData, manifestModified, _ := handleDeprecations(rawData, *kubeGitVersion)
		manifestContent, err := yaml.Marshal(rawData)
		if err != nil {
			return fmt.Errorf("failed to marshal raw manifest after deprecation check: %w", err)
		}

		// If this is not a bad object, place it back into the manifest
		modifiedManifest += fmt.Sprintf("---\n# Source: %s\n%s\n", resource.Name, manifestContent)

		if manifestModified {
			modified = true
		}
	}

	// If the release was modified in the above loop, save it back to the cluster
	if modified {
		message.Warnf("Zarf detected deprecated APIs for the '%s' helm release.  Attempting automatic upgrade.", latestRelease.Name)
		logger.From(ctx).Warn("detected deprecated APIs for the helm release", "name", latestRelease.Name)

		// Update current release version to be superseded (same as the helm mapkubeapis plugin)
		latestRelease.Info.Status = release.StatusSuperseded
		if err := h.actionConfig.Releases.Update(latestRelease); err != nil {
			return err
		}

		// Use a shallow copy of current release version to update the object with the modification
		// and then store this new version (same as the helm mapkubeapis plugin)
		var newRelease = latestRelease
		newRelease.Manifest = modifiedManifest
		newRelease.Info.Description = "Kubernetes deprecated API upgrade - DO NOT rollback from this version"
		newRelease.Info.LastDeployed = h.actionConfig.Now()
		newRelease.Version = latestRelease.Version + 1
		newRelease.Info.Status = release.StatusDeployed
		if err := h.actionConfig.Releases.Create(newRelease); err != nil {
			return err
		}
	}

	return nil
}

// handleDeprecations takes in an unstructured object and the k8s version and returns the latest version of the object and if it was modified.
func handleDeprecations(rawData *unstructured.Unstructured, kubernetesVersion semver.Version) (*unstructured.Unstructured, bool, error) {
	deprecatedVersionContent := &plutoapi.VersionFile{}
	err := goyaml.Unmarshal(plutoversionsfile.Content(), deprecatedVersionContent)
	if err != nil {
		return rawData, false, err
	}
	for _, deprecation := range deprecatedVersionContent.DeprecatedVersions {
		if deprecation.Component == "k8s" && deprecation.Kind == rawData.GetKind() && deprecation.Name == rawData.GetAPIVersion() {
			removedVersion, err := semver.NewVersion(deprecation.RemovedIn)
			if err != nil {
				return rawData, false, err
			}

			if removedVersion.LessThan(&kubernetesVersion) {
				if deprecation.ReplacementAPI != "" {
					rawData.SetAPIVersion(deprecation.ReplacementAPI)
					return rawData, true, nil
				}

				return nil, true, nil
			}
		}
	}
	return rawData, false, nil
}
