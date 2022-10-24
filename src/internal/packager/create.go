package packager

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/defenseunicorns/zarf/src/internal/kustomize"
	"github.com/defenseunicorns/zarf/src/internal/packager/validate"
	"github.com/defenseunicorns/zarf/src/types"

	"github.com/defenseunicorns/zarf/src/config"
	"github.com/defenseunicorns/zarf/src/internal/git"
	"github.com/defenseunicorns/zarf/src/internal/helm"
	"github.com/defenseunicorns/zarf/src/internal/images"
	"github.com/defenseunicorns/zarf/src/internal/sbom"
	"github.com/defenseunicorns/zarf/src/pkg/message"
	"github.com/defenseunicorns/zarf/src/pkg/utils"
	"github.com/mholt/archiver/v3"
)

// Create generates a zarf package tarball for consumption by
func (p *Package) Create(baseDir string) {
	var originalDir string

	// Change the working directory if this run has an alternate base dir
	if baseDir != "" {
		originalDir, _ = os.Getwd()
		_ = os.Chdir(baseDir)
		message.Note(fmt.Sprintf("Using build directory %s", baseDir))
	}

	if err := config.LoadConfig(config.ZarfYAML, false); err != nil {
		message.Fatal(err, "Unable to read the zarf.yaml file")
	}

	p.composeComponents()

	// After components are composed, template the active package
	if err := config.FillActiveTemplate(); err != nil {
		message.Fatalf(err, "Unable to fill variables in template: %s", err.Error())
	}

	components := config.GetComponents()

	seedImage := config.ZarfSeedImage

	configFile := p.tempPath.ZarfYaml

	// Save the transformed config
	if err := config.BuildConfig(configFile); err != nil {
		message.Fatalf(err, "Unable to write the %s file", configFile)
	}

	// Perform early package validation
	validate.Run()

	if !confirmAction("Create", nil) {
		os.Exit(0)
	}

	if config.IsZarfInitConfig() {
		// Load seed images into their own happy little tarball for ease of import on init
		pulledImages := images.PullAll([]string{seedImage}, p.tempPath.SeedImage)
		// Ignore SBOM creation if there the flag is set
		if !p.config.CreateOptions.SkipSBOM {
			sbom.CatalogImages(pulledImages, p.tempPath.Sboms, p.tempPath.SeedImage)
		}
	}

	var combinedImageList []string
	for _, component := range components {
		p.addComponent(component)
		// Combine all component images into a single entry for efficient layer reuse
		combinedImageList = append(combinedImageList, component.Images...)
	}

	// Images are handled separately from other component assets
	if len(combinedImageList) > 0 {
		uniqueList := utils.Unique(combinedImageList)
		pulledImages := images.PullAll(uniqueList, p.tempPath.Images)

		if p.config.CreateOptions.SkipSBOM {
			message.Debug("Skipping SBOM processing per --skip-sbom flag")
		} else {
			sbom.CatalogImages(pulledImages, p.tempPath.Sboms, p.tempPath.Images)
		}
	}

	// In case the directory was changed, reset to prevent breaking relative target paths
	if originalDir != "" {
		_ = os.Chdir(originalDir)
	}

	packageName := filepath.Join(p.config.CreateOptions.OutputDirectory, config.GetPackageName())

	_ = os.RemoveAll(packageName)
	err := archiver.Archive([]string{p.tempPath.Base + string(os.PathSeparator)}, packageName)
	if err != nil {
		message.Fatal(err, "Unable to create the package archive")
	}
}

func (p *Package) addComponent(component types.ZarfComponent) {
	message.HeaderInfof("📦 %s COMPONENT", strings.ToUpper(component.Name))
	componentPath, err := p.createComponentPaths(component)
	if err != nil {
		message.Fatal(err, "Unable to create component paths")
	}

	// Loop through each component prepare script and execute it
	for _, script := range component.Scripts.Prepare {
		p.loopScriptUntilSuccess(script, component.Scripts)
	}

	if len(component.Charts) > 0 {
		_ = utils.CreateDirectory(componentPath.Charts, 0700)
		_ = utils.CreateDirectory(componentPath.Values, 0700)
		re := regexp.MustCompile(`\.git$`)
		for _, chart := range component.Charts {
			isGitURL := re.MatchString(chart.Url)
			URLLen := len(chart.Url)
			if isGitURL {
				_ = helm.DownloadChartFromGit(chart, componentPath.Charts)
			} else if URLLen > 0 {
				helm.DownloadPublishedChart(chart, componentPath.Charts)
			} else {
				path := helm.CreateChartFromLocalFiles(chart, componentPath.Charts)
				zarfFilename := fmt.Sprintf("%s-%s.tgz", chart.Name, chart.Version)
				if !strings.HasSuffix(path, zarfFilename) {
					message.Fatalf(fmt.Errorf("error creating chart archive"), "User provided chart name and/or version does not match given chart")
				}
			}
			for idx, path := range chart.ValuesFiles {
				chartValueName := helm.StandardName(componentPath.Values, chart) + "-" + strconv.Itoa(idx)
				if err := utils.CreatePathAndCopy(path, chartValueName); err != nil {
					message.Fatalf(err, "Unable to copy values file %s", path)
				}
			}
		}
	}

	if len(component.Files) > 0 {
		_ = utils.CreateDirectory(componentPath.Files, 0700)
		for index, file := range component.Files {
			message.Debugf("Loading %#v", file)
			destinationFile := filepath.Join(componentPath.Files, strconv.Itoa(index))
			if utils.IsUrl(file.Source) {
				utils.DownloadToFile(file.Source, destinationFile, component.CosignKeyPath)
			} else {
				if err := utils.CreatePathAndCopy(file.Source, destinationFile); err != nil {
					message.Fatalf(err, "Unable to copy %s", file.Source)
				}
			}

			// Abort packaging on invalid shasum (if one is specified)
			if file.Shasum != "" {
				utils.ValidateSha256Sum(file.Shasum, destinationFile)
			}

			info, _ := os.Stat(destinationFile)

			if file.Executable || info.IsDir() {
				_ = os.Chmod(destinationFile, 0700)
			} else {
				_ = os.Chmod(destinationFile, 0600)
			}
		}
	}

	if len(component.DataInjections) > 0 {
		spinner := message.NewProgressSpinner("Loading data injections")
		defer spinner.Success()
		for _, data := range component.DataInjections {
			spinner.Updatef("Copying data injection %s for %s", data.Target.Path, data.Target.Selector)
			destinationFile := filepath.Join(componentPath.DataInjections, filepath.Base(data.Target.Path))
			if err := utils.CreatePathAndCopy(data.Source, destinationFile); err != nil {
				spinner.Fatalf(err, "Unable to copy data injection %s", data.Source)
			}
		}
	}

	if len(component.Manifests) > 0 {
		// Get the proper count of total manifests to add
		manifestCount := 0
		for _, manifest := range component.Manifests {
			manifestCount += len(manifest.Files)
			manifestCount += len(manifest.Kustomizations)
		}

		spinner := message.NewProgressSpinner("Loading %d K8s manifests", manifestCount)
		defer spinner.Success()

		if err := utils.CreateDirectory(componentPath.Manifests, 0700); err != nil {
			spinner.Fatalf(err, "Unable to create the manifest path %s", componentPath.Manifests)
		}

		// Iterate over all manifests
		for _, manifest := range component.Manifests {
			for _, file := range manifest.Files {
				// Copy manifests without any processing
				spinner.Updatef("Copying manifest %s", file)
				destination := fmt.Sprintf("%s/%s", componentPath.Manifests, file)
				if err := utils.CreatePathAndCopy(file, destination); err != nil {
					spinner.Fatalf(err, "Unable to copy the manifest %s", file)
				}
			}
			for idx, kustomization := range manifest.Kustomizations {
				// Generate manifests from kustomizations and place in the package
				spinner.Updatef("Building kustomization for %s", kustomization)
				destination := fmt.Sprintf("%s/kustomization-%s-%d.yaml", componentPath.Manifests, manifest.Name, idx)
				if err := kustomize.BuildKustomization(kustomization, destination, manifest.KustomizeAllowAnyDirectory); err != nil {
					spinner.Fatalf(err, "unable to build the kustomization for %s", kustomization)
				}
			}
		}
	}

	// Load all specified git repos
	if len(component.Repos) > 0 {
		spinner := message.NewProgressSpinner("Loading %d git repos", len(component.Repos))
		defer spinner.Success()
		for _, url := range component.Repos {
			// Pull all the references if there is no `@` in the string
			_, err := git.Pull(url, componentPath.Repos, spinner)
			if err != nil {
				message.Fatalf(err, fmt.Sprintf("Unable to pull the repo with the url of (%s}", url))
			}
		}
	}

}
