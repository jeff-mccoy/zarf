// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

//go:build !alt_language

// Package lang contains the language strings for english used by Zarf
// Alternative languages can be created by duplicating this file and changing the build tag to "//go:build alt_language && <language>".
package lang

import (
	"errors"
)

// All language strings should be in the form of a constant
// The constants should be grouped by the top level package they are used in (or common)
// The format should be <PathName><Err/Info><ShortDescription>
// Debug messages will not be a part of the language strings since they are not intended to be user facing
// Include sprintf formatting directives in the string if needed.
const (
	ErrUnmarshal                    = "failed to unmarshal file: %w"
	ErrWritingFile                  = "failed to write file %s: %s"
	ErrDownloading                  = "failed to download %s: %s"
	ErrCreatingDir                  = "failed to create directory %s: %s"
	ErrRemoveFile                   = "failed to remove file %s: %s"
	ErrUnarchive                    = "failed to unarchive %s: %s"
	ErrFileExtract                  = "failed to extract filename %s from archive %s: %s"
	ErrFileNameExtract              = "failed to extract filename from URL %s: %s"
	ErrUnableToGenerateRandomSecret = "unable to generate a random secret"
)

// Lint messages
const (
	UnsetVarLintWarning            = "There are templates that are not set and won't be evaluated during lint"
	PkgValidateTemplateDeprecation = "Package template %q is using the deprecated syntax ###ZARF_PKG_VAR_%s###. This will be removed in Zarf v1.0.0. Please update to ###ZARF_PKG_TMPL_%s###."
)

// Zarf CLI commands.
const (
	// root zarf command
	RootCmdShort = "DevSecOps for Airgap"
	RootCmdLong  = "Zarf eliminates the complexity of air gap software delivery for Kubernetes clusters and cloud native workloads\n" +
		"using a declarative packaging strategy to support DevSecOps in offline and semi-connected environments."

	RootCmdFlagLogLevel              = "Log level when running Zarf. Valid options are: warn, info, debug, trace"
	RootCmdFlagArch                  = "Architecture for OCI images and Zarf packages"
	RootCmdFlagSkipLogFile           = "Disable log file creation"
	RootCmdFlagNoProgress            = "Disable fancy UI progress bars, spinners, logos, etc"
	RootCmdFlagNoColor               = "Disable colors in output"
	RootCmdFlagCachePath             = "Specify the location of the Zarf cache directory"
	RootCmdFlagTempDir               = "Specify the temporary directory to use for intermediate files"
	RootCmdFlagInsecure              = "Allow access to insecure registries and disable other recommended security enforcements such as package checksum and signature validation. This flag should only be used if you have a specific reason and accept the reduced security posture."
	RootCmdFlagPlainHTTP             = "Force the connections over HTTP instead of HTTPS. This flag should only be used if you have a specific reason and accept the reduced security posture."
	RootCmdFlagInsecureSkipTLSVerify = "Skip checking server's certificate for validity. This flag should only be used if you have a specific reason and accept the reduced security posture."

	RootCmdDeprecatedDeploy = "Deprecated: Please use \"zarf package deploy %s\" to deploy this package.  This warning will be removed in Zarf v1.0.0."
	RootCmdDeprecatedCreate = "Deprecated: Please use \"zarf package create\" to create this package.  This warning will be removed in Zarf v1.0.0."

	// zarf connect
	CmdConnectShort = "Accesses services or pods deployed in the cluster"
	CmdConnectLong  = "Uses a k8s port-forward to connect to resources within the cluster referenced by your kube-context.\n" +
		"Two default options for this command are <REGISTRY|GIT>. These will connect to the Zarf created resources " +
		"(assuming they were selected when performing the `zarf init` command).\n\n" +
		"Packages can provide service manifests that define their own shortcut connection options. These options will be " +
		"printed to the terminal when the package finishes deploying.\n If you don't remember what connection shortcuts your deployed " +
		"package offers, you can search your cluster for services that have the 'zarf.dev/connect-name' label. The value of that label is " +
		"the name you will pass into the 'zarf connect' command.\n\n" +
		"Even if the packages you deploy don't define their own shortcut connection options, you can use the command flags " +
		"to connect into specific resources. You can read the command flag descriptions below to get a better idea how to connect " +
		"to whatever resource you are trying to connect to."

	// zarf connect list
	CmdConnectListShort = "Lists all available connection shortcuts"

	CmdConnectFlagName       = "Specify the resource name.  E.g. name=unicorns or name=unicorn-pod-7448499f4d-b5bk6. Ignored if connect-name is supplied."
	CmdConnectFlagNamespace  = "Specify the namespace.  E.g. namespace=default. Ignored if connect-name is supplied."
	CmdConnectFlagType       = "Specify the resource type.  E.g. type=svc or type=pod. Ignored if connect-name is supplied."
	CmdConnectFlagLocalPort  = "(Optional, autogenerated if not provided) Specify the local port to bind to.  E.g. local-port=42000."
	CmdConnectFlagRemotePort = "Specify the remote port of the resource to bind to.  E.g. remote-port=8080. Ignored if connect-name is supplied."
	CmdConnectFlagCliOnly    = "Disable browser auto-open"

	CmdConnectPreparingTunnel = "Preparing a tunnel to connect to %s"
	CmdConnectEstablishedCLI  = "Tunnel established at %s, waiting for user to interrupt (ctrl-c to end)"
	CmdConnectEstablishedWeb  = "Tunnel established at %s, opening your default web browser (ctrl-c to end)"
	CmdConnectTunnelClosed    = "Tunnel to %s successfully closed due to user interrupt"

	// zarf destroy
	CmdDestroyShort = "Tears down Zarf and removes its components from the environment"
	CmdDestroyLong  = "Tear down Zarf.\n\n" +
		"Deletes everything in the 'zarf' namespace within your connected k8s cluster.\n\n" +
		"If Zarf deployed your k8s cluster, this command will also tear your cluster down by " +
		"searching through /opt/zarf for any scripts that start with 'zarf-clean-' and executing them. " +
		"Since this is a cleanup operation, Zarf will not stop the teardown if one of the scripts produce " +
		"an error.\n\n" +
		"If Zarf did not deploy your k8s cluster, this command will delete the Zarf namespace, delete secrets " +
		"and labels that only Zarf cares about, and optionally uninstall components that Zarf deployed onto " +
		"the cluster. Since this is a cleanup operation, Zarf will not stop the uninstalls if one of the " +
		"resources produce an error while being deleted."

	CmdDestroyFlagConfirm          = "REQUIRED. Confirm the destroy action to prevent accidental deletions"
	CmdDestroyFlagRemoveComponents = "Also remove any installed components outside the zarf namespace"

	CmdDestroyErrScriptPermissionDenied = "Received 'permission denied' when trying to execute the script (%s). Please double-check you have the correct kube-context."

	// zarf init
	CmdInitShort = "Prepares a k8s cluster for the deployment of Zarf packages"
	CmdInitLong  = "Injects an OCI registry as well as an optional git server " +
		"into a Kubernetes cluster in the zarf namespace " +
		"to support future application deployments.\n" +
		"If you do not have a cluster already configured, this command will give you " +
		"the ability to install a cluster locally.\n\n" +
		"This command looks for a zarf-init package in the local directory that the command was executed " +
		"from. If no package is found in the local directory and the Zarf CLI exists somewhere outside of " +
		"the current directory, Zarf will failover and attempt to find a zarf-init package in the directory " +
		"that the Zarf binary is located in.\n\n\n\n"

	CmdInitExample = `
# Initializing without any optional components:
$ zarf init

# Initializing w/ Zarfs internal git server:
$ zarf init --components=git-server

# Initializing w/ an internal registry but with a different nodeport:
$ zarf init --nodeport=30333

# Initializing w/ an external registry:
$ zarf init --registry-push-password={PASSWORD} --registry-push-username={USERNAME} --registry-url={URL}

# Initializing w/ an external git server:
$ zarf init --git-push-password={PASSWORD} --git-push-username={USERNAME} --git-url={URL}

# Initializing w/ an external artifact server:
$ zarf init --artifact-push-password={PASSWORD} --artifact-push-username={USERNAME} --artifact-url={URL}

# NOTE: Not specifying a pull username/password will use the push user for pulling as well.
`

	CmdInitErrValidateGit      = "the 'git-push-username' and 'git-push-password' flags must be provided if the 'git-url' flag is provided"
	CmdInitErrValidateRegistry = "the 'registry-push-username' and 'registry-push-password' flags must be provided if the 'registry-url' flag is provided"
	CmdInitErrValidateArtifact = "the 'artifact-push-username' and 'artifact-push-token' flags must be provided if the 'artifact-url' flag is provided"

	CmdInitPullAsk       = "It seems the init package could not be found locally, but can be pulled from oci://%s"
	CmdInitPullNote      = "Note: This will require an internet connection."
	CmdInitPullConfirm   = "Do you want to pull this init package?"
	CmdInitPullErrManual = "pull the init package manually and place it in the current working directory"

	CmdInitFlagSet = "Specify deployment variables to set on the command line (KEY=value)"

	CmdInitFlagConfirm      = "Confirms package deployment without prompting. ONLY use with packages you trust. Skips prompts to review SBOM, configure variables, select optional components and review potential breaking changes."
	CmdInitFlagComponents   = "Specify which optional components to install.  E.g. --components=git-server"
	CmdInitFlagStorageClass = "Specify the storage class to use for the registry and git server.  E.g. --storage-class=standard"

	CmdInitFlagGitURL      = "External git server url to use for this Zarf cluster"
	CmdInitFlagGitPushUser = "Username to access to the git server Zarf is configured to use. User must be able to create repositories via 'git push'"
	CmdInitFlagGitPushPass = "Password for the push-user to access the git server"
	CmdInitFlagGitPullUser = "Username for pull-only access to the git server"
	CmdInitFlagGitPullPass = "Password for the pull-only user to access the git server"

	CmdInitFlagRegURL      = "External registry url address to use for this Zarf cluster"
	CmdInitFlagRegNodePort = "Nodeport to access a registry internal to the k8s cluster. Between [30000-32767]"
	CmdInitFlagRegPushUser = "Username to access to the registry Zarf is configured to use"
	CmdInitFlagRegPushPass = "Password for the push-user to connect to the registry"
	CmdInitFlagRegPullUser = "Username for pull-only access to the registry"
	CmdInitFlagRegPullPass = "Password for the pull-only user to access the registry"
	CmdInitFlagRegSecret   = "Registry secret value"

	CmdInitFlagArtifactURL       = "[alpha] External artifact registry url to use for this Zarf cluster"
	CmdInitFlagArtifactPushUser  = "[alpha] Username to access to the artifact registry Zarf is configured to use. User must be able to upload package artifacts."
	CmdInitFlagArtifactPushToken = "[alpha] API Token for the push-user to access the artifact registry"

	// zarf internal
	CmdInternalShort = "Internal tools used by zarf"

	CmdInternalAgentShort = "Runs the zarf agent"
	CmdInternalAgentLong  = "NOTE: This command is a hidden command and generally shouldn't be run by a human.\n" +
		"This command starts up a http webhook that Zarf deployments use to mutate pods to conform " +
		"with the Zarf container registry and Gitea server URLs."

	CmdInternalProxyShort = "[alpha] Runs the zarf agent http proxy"
	CmdInternalProxyLong  = "[alpha] NOTE: This command is a hidden command and generally shouldn't be run by a human.\n" +
		"This command starts up a http proxy that can be used by running pods to transform queries " +
		"that conform to Gitea / Gitlab repository and package URLs in the airgap."

	CmdInternalGenerateCliDocsShort   = "Creates auto-generated markdown of all the commands for the CLI"
	CmdInternalGenerateCliDocsSuccess = "Successfully created the CLI documentation"

	CmdInternalConfigSchemaShort = "Generates a JSON schema for the zarf.yaml configuration"

	CmdInternalTypesSchemaShort = "Generates a JSON schema for the Zarf types (DeployedPackage ZarfPackage ZarfState)"

	CmdInternalCreateReadOnlyGiteaUserShort = "Creates a read-only user in Gitea"
	CmdInternalCreateReadOnlyGiteaUserLong  = "Creates a read-only user in Gitea by using the Gitea API. " +
		"This is called internally by the supported Gitea package component."
	CmdInternalCreateReadOnlyGiteaUserErr = "Unable to create a read-only user in the Gitea service."

	CmdInternalArtifactRegistryGiteaTokenShort = "Creates an artifact registry token for Gitea"
	CmdInternalArtifactRegistryGiteaTokenLong  = "Creates an artifact registry token in Gitea using the Gitea API. " +
		"This is called internally by the supported Gitea package component."

	CmdInternalUpdateGiteaPVCShort = "Updates an existing Gitea persistent volume claim"
	CmdInternalUpdateGiteaPVCLong  = "Updates an existing Gitea persistent volume claim by assessing if claim is a custom user provided claim or default." +
		"This is called internally by the supported Gitea package component."
	CmdInternalUpdateGiteaPVCErr          = "Unable to update the existing Gitea persistent volume claim."
	CmdInternalFlagUpdateGiteaPVCRollback = "Roll back previous Gitea persistent volume claim updates."

	CmdInternalIsValidHostnameShort = "Checks if the current machine's hostname is RFC1123 compliant"

	CmdInternalCrc32Short = "Generates a decimal CRC32 for the given text"

	// zarf package
	CmdPackageShort                       = "Zarf package commands for creating, deploying, and inspecting packages"
	CmdPackageFlagConcurrency             = "Number of concurrent layer operations to perform when interacting with a remote package."
	CmdPackageFlagFlagPublicKey           = "Path to public key file for validating signed packages"
	CmdPackageFlagSkipSignatureValidation = "Skip validating the signature of the Zarf package"
	CmdPackageFlagRetries                 = "Number of retries to perform for Zarf deploy operations like git/image pushes or Helm installs"

	CmdPackageCreateShort = "Creates a Zarf package from a given directory or the current directory"
	CmdPackageCreateLong  = "Builds an archive of resources and dependencies defined by the 'zarf.yaml' in the specified directory.\n" +
		"Private registries and repositories are accessed via credentials in your local '~/.docker/config.json', " +
		"'~/.git-credentials' and '~/.netrc'.\n"

	CmdPackageDeployShort = "Deploys a Zarf package from a local file or URL (runs offline)"
	CmdPackageDeployLong  = "Unpacks resources and dependencies from a Zarf package archive and deploys them onto the target system.\n" +
		"Kubernetes clusters are accessed via credentials in your current kubecontext defined in '~/.kube/config'"

	CmdPackageMirrorShort = "Mirrors a Zarf package's internal resources to specified image registries and git repositories"
	CmdPackageMirrorLong  = "Unpacks resources and dependencies from a Zarf package archive and mirrors them into the specified\n" +
		"image registries and git repositories within the target environment"
	CmdPackageMirrorExample = `
# Mirror resources to internal Zarf resources
$ zarf package mirror-resources <your-package.tar.zst> \
	--registry-url http://zarf-docker-registry.zarf.svc.cluster.local:5000 \
	--registry-push-username zarf-push \
	--registry-push-password <generated-registry-push-password> \
	--git-url http://zarf-gitea-http.zarf.svc.cluster.local:3000 \
	--git-push-username zarf-git-user \
	--git-push-password <generated-git-push-password>

# Mirror resources to external resources
$ zarf package mirror-resources <your-package.tar.zst> \
	--registry-url registry.enterprise.corp \
	--registry-push-username <registry-push-username> \
	--registry-push-password <registry-push-password> \
	--git-url https://git.enterprise.corp \
	--git-push-username <git-push-username> \
	--git-push-password <git-push-password>
`

	CmdPackageInspectShort = "Displays the definition of a Zarf package (runs offline)"
	CmdPackageInspectLong  = "Displays the 'zarf.yaml' definition for the specified package and optionally allows SBOMs to be viewed"

	CmdPackageListShort         = "Lists out all of the packages that have been deployed to the cluster (runs offline)"
	CmdPackageListNoPackageWarn = "Unable to get the packages deployed to the cluster"

	CmdPackageCreateFlagConfirm               = "Confirm package creation without prompting"
	CmdPackageCreateFlagSet                   = "Specify package variables to set on the command line (KEY=value)"
	CmdPackageCreateFlagOutput                = "Specify the output (either a directory or an oci:// URL) for the created Zarf package"
	CmdPackageCreateFlagSbom                  = "View SBOM contents after creating the package"
	CmdPackageCreateFlagSbomOut               = "Specify an output directory for the SBOMs from the created Zarf package"
	CmdPackageCreateFlagSkipSbom              = "Skip generating SBOM for this package"
	CmdPackageCreateFlagMaxPackageSize        = "Specify the maximum size of the package in megabytes, packages larger than this will be split into multiple parts to be loaded onto smaller media (i.e. DVDs). Use 0 to disable splitting."
	CmdPackageCreateFlagSigningKey            = "Path to private key file for signing packages"
	CmdPackageCreateFlagSigningKeyPassword    = "Password to the private key file used for signing packages"
	CmdPackageCreateFlagDeprecatedKey         = "[Deprecated] Path to private key file for signing packages (use --signing-key instead)"
	CmdPackageCreateFlagDeprecatedKeyPassword = "[Deprecated] Password to the private key file used for signing packages (use --signing-key-pass instead)"
	CmdPackageCreateFlagDifferential          = "[beta] Build a package that only contains the differential changes from local resources and differing remote resources from the specified previously built package"
	CmdPackageCreateFlagRegistryOverride      = "Specify a map of domains to override on package create when pulling images (e.g. --registry-override docker.io=dockerio-reg.enterprise.intranet)"
	CmdPackageCreateFlagFlavor                = "The flavor of components to include in the resulting package (i.e. have a matching or empty \"only.flavor\" key)"
	CmdPackageCreateCleanPathErr              = "Invalid characters in Zarf cache path, defaulting to %s"

	CmdPackageDeployFlagConfirm                        = "Confirms package deployment without prompting. ONLY use with packages you trust. Skips prompts to review SBOM, configure variables, select optional components and review potential breaking changes."
	CmdPackageDeployFlagAdoptExistingResources         = "Adopts any pre-existing K8s resources into the Helm charts managed by Zarf. ONLY use when you have existing deployments you want Zarf to takeover."
	CmdPackageDeployFlagSet                            = "Specify deployment variables to set on the command line (KEY=value)"
	CmdPackageDeployFlagComponents                     = "Comma-separated list of components to deploy.  Adding this flag will skip the prompts for selected components.  Globbing component names with '*' and deselecting 'default' components with a leading '-' are also supported."
	CmdPackageDeployFlagShasum                         = "Shasum of the package to deploy. Required if deploying a remote https package."
	CmdPackageDeployFlagSget                           = "[Deprecated] Path to public sget key file for remote packages signed via cosign. This flag will be removed in v1.0.0 please use the --key flag instead."
	CmdPackageDeployFlagTimeout                        = "Timeout for health checks and Helm operations such as installs and rollbacks"
	CmdPackageDeployValidateArchitectureErr            = "this package architecture is %s, but the target cluster only has the %s architecture(s). These architectures must be compatible when \"images\" are present"
	CmdPackageDeployValidateLastNonBreakingVersionWarn = "The version of this Zarf binary '%s' is less than the LastNonBreakingVersion of '%s'. You may need to upgrade your Zarf version to at least '%s' to deploy this package"
	CmdPackageDeployInvalidCLIVersionWarn              = "CLIVersion is set to '%s' which can cause issues with package creation and deployment. To avoid such issues, please set the value to the valid semantic version for this version of Zarf."

	CmdPackageMirrorFlagComponents = "Comma-separated list of components to mirror.  This list will be respected regardless of a component's 'required' or 'default' status.  Globbing component names with '*' and deselecting components with a leading '-' are also supported."
	CmdPackageMirrorFlagNoChecksum = "Turns off the addition of a checksum to image tags (as would be used by the Zarf Agent) while mirroring images."

	CmdPackageInspectFlagSbom       = "View SBOM contents while inspecting the package"
	CmdPackageInspectFlagSbomOut    = "Specify an output directory for the SBOMs from the inspected Zarf package"
	CmdPackageInspectFlagListImages = "List images in the package (prints to stdout)"

	CmdPackageRemoveShort          = "Removes a Zarf package that has been deployed already (runs offline)"
	CmdPackageRemoveFlagConfirm    = "REQUIRED. Confirm the removal action to prevent accidental deletions"
	CmdPackageRemoveFlagComponents = "Comma-separated list of components to remove.  This list will be respected regardless of a component's 'required' or 'default' status.  Globbing component names with '*' and deselecting components with a leading '-' are also supported."

	CmdPackagePublishShort   = "Publishes a Zarf package to a remote registry"
	CmdPackagePublishExample = `
# Publish a package to a remote registry
$ zarf package publish my-package.tar oci://my-registry.com/my-namespace

# Publish a skeleton package to a remote registry
$ zarf package publish ./path/to/dir oci://my-registry.com/my-namespace
`
	CmdPackagePublishFlagSigningKey         = "Path to a private key file for signing or re-signing packages with a new key"
	CmdPackagePublishFlagSigningKeyPassword = "Password to the private key file used for publishing packages"

	CmdPackagePullShort   = "Pulls a Zarf package from a remote registry and save to the local file system"
	CmdPackagePullExample = `
# Pull a package matching the current architecture
$ zarf package pull oci://ghcr.io/defenseunicorns/packages/dos-games:1.0.0

# Pull a package matching a specific architecture
$ zarf package pull oci://ghcr.io/defenseunicorns/packages/dos-games:1.0.0 -a arm64

# Pull a skeleton package
$ zarf package pull oci://ghcr.io/defenseunicorns/packages/dos-games:1.0.0 -a skeleton`
	CmdPackagePullFlagOutputDirectory = "Specify the output directory for the pulled Zarf package"
	CmdPackagePullFlagShasum          = "Shasum of the package to pull. Required if pulling a https package. A shasum can be retrieved using 'zarf dev sha256sum <url>'"

	CmdPackageChoose                = "Choose or type the package file"
	CmdPackageClusterSourceFallback = "%q does not satisfy any current sources, assuming it is a package deployed to a cluster"
	CmdPackageInvalidSource         = "Unable to identify source from %q: %s"

	// zarf dev (prepare is an alias for dev)
	CmdDevShort = "Commands useful for developing packages"

	CmdDevDeployShort      = "[beta] Creates and deploys a Zarf package from a given directory"
	CmdDevDeployLong       = "[beta] Creates and deploys a Zarf package from a given directory, setting options like YOLO mode for faster iteration."
	CmdDevDeployFlagNoYolo = "Disable the YOLO mode default override and create / deploy the package as-defined"

	CmdDevGenerateShort   = "[alpha] Creates a zarf.yaml automatically from a given remote (git) Helm chart"
	CmdDevGenerateExample = "zarf dev generate podinfo --url https://github.com/stefanprodan/podinfo.git --version 6.4.0 --gitPath charts/podinfo"

	CmdDevPatchGitShort = "Converts all .git URLs to the specified Zarf HOST and with the Zarf URL pattern in a given FILE.  NOTE:\n" +
		"This should only be used for manifests that are not mutated by the Zarf Agent Mutating Webhook."
	CmdDevPatchGitOverwritePrompt = "Overwrite the file %s with these changes?"

	CmdDevSha256sumShort         = "Generates a SHA256SUM for the given file"
	CmdDevSha256sumRemoteWarning = "This is a remote source. If a published checksum is available you should use that rather than calculating it directly from the remote link."

	CmdDevFindImagesShort = "Evaluates components in a Zarf file to identify images specified in their helm charts and manifests"
	CmdDevFindImagesLong  = "Evaluates components in a Zarf file to identify images specified in their helm charts and manifests.\n\n" +
		"Components that have repos that host helm charts can be processed by providing the --repo-chart-path."

	CmdDevGenerateConfigShort = "Generates a config file for Zarf"
	CmdDevGenerateConfigLong  = "Generates a Zarf config file for controlling how the Zarf CLI operates. Optionally accepts a filename to write the config to.\n\n" +
		"The extension will determine the format of the config file, e.g. env-1.yaml, env-2.json, env-3.toml etc.\n" +
		"Accepted extensions are json, toml, yaml.\n\n" +
		"NOTE: This file must not already exist. If no filename is provided, the config will be written to the current working directory as zarf-config.toml."

	CmdDevFlagExtractPath          = `The path inside of an archive to use to calculate the sha256sum (i.e. for use with "files.extractPath")`
	CmdDevFlagSet                  = "Specify package variables to set on the command line (KEY=value). Note, if using a config file, this will be set by [package.create.set]."
	CmdDevFlagRepoChartPath        = `If git repos hold helm charts, often found with gitops tools, specify the chart path, e.g. "/" or "/chart"`
	CmdDevFlagGitAccount           = "User or organization name for the git account that the repos are created under."
	CmdDevFlagKubeVersion          = "Override the default helm template KubeVersion when performing a package chart template"
	CmdDevFlagRegistry             = "Override the ###ZARF_REGISTRY### value"
	CmdDevFlagFindImagesWhy        = "Prints the source manifest for the specified image"
	CmdDevFlagFindImagesSkipCosign = "Skip searching for cosign artifacts related to discovered images"
	CmdDevFlagFindImagesUpdate     = "Updates zarf.yaml with found images"

	CmdDevLintShort = "Lints the given package for valid schema and recommended practices"
	CmdDevLintLong  = "Verifies the package schema, checks if any variables won't be evaluated, and checks for unpinned images/repos/files"

	// zarf tools
	CmdToolsShort = "Collection of additional tools to make airgap easier"

	CmdToolsArchiverShort           = "Compresses/Decompresses generic archives, including Zarf packages"
	CmdToolsArchiverCompressShort   = "Compresses a collection of sources based off of the destination file extension."
	CmdToolsArchiverDecompressShort = "Decompresses an archive or Zarf package based off of the source file extension."

	CmdToolsRegistryShort     = "Tools for working with container registries using go-containertools"
	CmdToolsRegistryZarfState = "Retrieving registry information from Zarf state"
	CmdToolsRegistryTunnel    = "Opening a tunnel from %s locally to %s in the cluster"

	CmdToolsRegistryCatalogExample = `
# List the repos internal to Zarf
$ zarf tools registry catalog

# List the repos for reg.example.com
$ zarf tools registry catalog reg.example.com
`
	CmdToolsRegistryListExample = `
# List the tags for a repo internal to Zarf
$ zarf tools registry ls 127.0.0.1:31999/stefanprodan/podinfo

# List the tags for a repo hosted at reg.example.com
$ zarf tools registry ls reg.example.com/stefanprodan/podinfo
`

	CmdToolsRegistryPushExample = `
# Push an image into an internal repo in Zarf
$ zarf tools registry push image.tar 127.0.0.1:31999/stefanprodan/podinfo:6.4.0

# Push an image into an repo hosted at reg.example.com
$ zarf tools registry push image.tar reg.example.com/stefanprodan/podinfo:6.4.0
`

	CmdToolsRegistryPullExample = `
# Pull an image from an internal repo in Zarf to a local tarball
$ zarf tools registry pull 127.0.0.1:31999/stefanprodan/podinfo:6.4.0 image.tar

# Pull an image from a repo hosted at reg.example.com to a local tarball
$ zarf tools registry pull reg.example.com/stefanprodan/podinfo:6.4.0 image.tar
`

	CmdToolsRegistryDeleteExample = `
# Delete an image digest from an internal repo in Zarf
$ zarf tools registry delete 127.0.0.1:31999/stefanprodan/podinfo@sha256:57a654ace69ec02ba8973093b6a786faa15640575fbf0dbb603db55aca2ccec8

# Delete an image digest from a repo hosted at reg.example.com
$ zarf tools registry delete reg.example.com/stefanprodan/podinfo@sha256:57a654ace69ec02ba8973093b6a786faa15640575fbf0dbb603db55aca2ccec8
`

	CmdToolsRegistryDigestExample = `
# Return an image digest for an internal repo in Zarf
$ zarf tools registry digest 127.0.0.1:31999/stefanprodan/podinfo:6.4.0

# Return an image digest from a repo hosted at reg.example.com
$ zarf tools registry digest reg.example.com/stefanprodan/podinfo:6.4.0
`

	CmdToolsRegistryPruneShort       = "Prunes images from the registry that are not currently being used by any Zarf packages."
	CmdToolsRegistryPruneFlagConfirm = "Confirm the image prune action to prevent accidental deletions"
	CmdToolsRegistryPruneImageList   = "The following image digests will be pruned from the registry:"
	CmdToolsRegistryPruneNoImages    = "There are no images to prune"
	CmdToolsRegistryPruneLookup      = "Looking up images within package definitions"
	CmdToolsRegistryPruneCatalog     = "Cataloging images in the registry"
	CmdToolsRegistryPruneCalculate   = "Calculating images to prune"
	CmdToolsRegistryPruneDelete      = "Deleting unused images"

	CmdToolsRegistryFlagVerbose  = "Enable debug logs"
	CmdToolsRegistryFlagInsecure = "Allow image references to be fetched without TLS"
	CmdToolsRegistryFlagNonDist  = "Allow pushing non-distributable (foreign) layers"
	CmdToolsRegistryFlagPlatform = "Specifies the platform in the form os/arch[/variant][:osversion] (e.g. linux/amd64)."

	CmdToolsGetGitPasswdShort       = "[Deprecated] Returns the push user's password for the Git server"
	CmdToolsGetGitPasswdLong        = "[Deprecated] Reads the password for a user with push access to the configured Git server in Zarf State. Note that this command has been replaced by 'zarf tools get-creds git' and will be removed in Zarf v1.0.0."
	CmdToolsGetGitPasswdDeprecation = "Deprecated: This command has been replaced by 'zarf tools get-creds git' and will be removed in Zarf v1.0.0."
	CmdToolsYqExample               = `
# yq defaults to 'eval' command if no command is specified. See "zarf tools yq eval --help" for more examples.

# read the "stuff" node from "myfile.yml"
zarf tools yq '.stuff' < myfile.yml

# update myfile.yml in place
zarf tools yq -i '.stuff = "foo"' myfile.yml

# print contents of sample.json as idiomatic YAML
zarf tools yq -P sample.json
`
	CmdToolsYqEvalAllExample = `
# Merge f2.yml into f1.yml (inplace)
zarf tools yq eval-all --inplace 'select(fileIndex == 0) * select(fileIndex == 1)' f1.yml f2.yml
## the same command and expression using shortened names:
zarf tools yq ea -i 'select(fi == 0) * select(fi == 1)' f1.yml f2.yml


# Merge all given files
zarf tools yq ea '. as $item ireduce ({}; . * $item )' file1.yml file2.yml ...

# Pipe from STDIN
## use '-' as a filename to pipe from STDIN
cat file2.yml | zarf tools yq ea '.a.b' file1.yml - file3.yml
`
	CmdToolsYqEvalExample = `
# Reads field under the given path for each file
zarf tools yq e '.a.b' f1.yml f2.yml

# Prints out the file
zarf tools yq e sample.yaml

# Pipe from STDIN
## use '-' as a filename to pipe from STDIN
cat file2.yml | zarf tools yq e '.a.b' file1.yml - file3.yml

# Creates a new yaml document
## Note that editing an empty file does not work.
zarf tools yq e -n '.a.b.c = "cat"'

# Update a file in place
zarf tools yq e '.a.b = "cool"' -i file.yaml
`
	CmdToolsMonitorShort = "Launches a terminal UI to monitor the connected cluster using K9s."

	CmdToolsHelmShort = "Subset of the Helm CLI included with Zarf to help manage helm charts."
	CmdToolsHelmLong  = "Subset of the Helm CLI that includes the repo and dependency commands for managing helm charts destined for the air gap."

	CmdToolsClearCacheShort         = "Clears the configured git and image cache directory"
	CmdToolsClearCacheDir           = "Cache directory set to: %s"
	CmdToolsClearCacheSuccess       = "Successfully cleared the cache from %s"
	CmdToolsClearCacheFlagCachePath = "Specify the location of the Zarf artifact cache (images and git repositories)"

	CmdToolsDownloadInitShort               = "Downloads the init package for the current Zarf version into the specified directory"
	CmdToolsDownloadInitFlagOutputDirectory = "Specify a directory to place the init package in."

	CmdToolsGenPkiShort       = "Generates a Certificate Authority and PKI chain of trust for the given host"
	CmdToolsGenPkiSuccess     = "Successfully created a chain of trust for %s"
	CmdToolsGenPkiFlagAltName = "Specify Subject Alternative Names for the certificate"

	CmdToolsGenKeyShort                = "Generates a cosign public/private keypair that can be used to sign packages"
	CmdToolsGenKeyPrompt               = "Private key password (empty for no password): "
	CmdToolsGenKeyPromptAgain          = "Private key password again (empty for no password): "
	CmdToolsGenKeyPromptExists         = "File %s already exists. Overwrite? "
	CmdToolsGenKeyErrUnableGetPassword = "unable to get password for private key: %s"
	CmdToolsGenKeyErrPasswordsNotMatch = "passwords do not match"
	CmdToolsGenKeySuccess              = "Generated key pair and written to %s and %s"

	CmdToolsSbomShort = "Generates a Software Bill of Materials (SBOM) for the given package"

	CmdToolsWaitForShort = "Waits for a given Kubernetes resource to be ready"
	CmdToolsWaitForLong  = "By default Zarf will wait for all Kubernetes resources to be ready before completion of a component during a deployment.\n" +
		"This command can be used to wait for a Kubernetes resources to exist and be ready that may be created by a Gitops tool or a Kubernetes operator.\n" +
		"You can also wait for arbitrary network endpoints using REST or TCP checks.\n\n"
	CmdToolsWaitForExample = `
# Wait for Kubernetes resources:
$ zarf tools wait-for pod my-pod-name ready -n default                  #  wait for pod my-pod-name in namespace default to be ready
$ zarf tools wait-for p cool-pod-name ready -n cool                     #  wait for pod (using p alias) cool-pod-name in namespace cool to be ready
$ zarf tools wait-for deployment podinfo available -n podinfo           #  wait for deployment podinfo in namespace podinfo to be available
$ zarf tools wait-for pod app=podinfo ready -n podinfo                  #  wait for pod with label app=podinfo in namespace podinfo to be ready
$ zarf tools wait-for svc zarf-docker-registry exists -n zarf           #  wait for service zarf-docker-registry in namespace zarf to exist
$ zarf tools wait-for svc zarf-docker-registry -n zarf                  #  same as above, except exists is the default condition
$ zarf tools wait-for crd addons.k3s.cattle.io                          #  wait for crd addons.k3s.cattle.io to exist
$ zarf tools wait-for sts test-sts '{.status.availableReplicas}'=23     #  wait for statefulset test-sts to have 23 available replicas

# Wait for network endpoints:
$ zarf tools wait-for http localhost:8080 200                           #  wait for a 200 response from http://localhost:8080
$ zarf tools wait-for tcp localhost:8080                                #  wait for a connection to be established on localhost:8080
$ zarf tools wait-for https 1.1.1.1 200                                 #  wait for a 200 response from https://1.1.1.1
$ zarf tools wait-for http google.com                                   #  wait for any 2xx response from http://google.com
$ zarf tools wait-for http google.com success                           #  wait for any 2xx response from http://google.com
`
	CmdToolsWaitForFlagTimeout   = "Specify the timeout duration for the wait command."
	CmdToolsWaitForFlagNamespace = "Specify the namespace of the resources to wait for."

	CmdToolsKubectlDocs = "Kubectl command. See https://kubernetes.io/docs/reference/kubectl/overview/ for more information."

	CmdToolsGetCredsShort   = "Displays a table of credentials for deployed Zarf services. Pass a service key to get a single credential"
	CmdToolsGetCredsLong    = "Display a table of credentials for deployed Zarf services. Pass a service key to get a single credential. i.e. 'zarf tools get-creds registry'"
	CmdToolsGetCredsExample = `
# Print all Zarf credentials:
$ zarf tools get-creds

# Get specific Zarf credentials:
$ zarf tools get-creds registry
$ zarf tools get-creds registry-readonly
$ zarf tools get-creds git
$ zarf tools get-creds git-readonly
$ zarf tools get-creds artifact
`

	CmdToolsUpdateCredsShort   = "Updates the credentials for deployed Zarf services. Pass a service key to update credentials for a single service"
	CmdToolsUpdateCredsLong    = "Updates the credentials for deployed Zarf services. Pass a service key to update credentials for a single service. i.e. 'zarf tools update-creds registry'"
	CmdToolsUpdateCredsExample = `
# Autogenerate all Zarf credentials at once:
$ zarf tools update-creds

# Autogenerate specific Zarf service credentials:
$ zarf tools update-creds registry
$ zarf tools update-creds git
$ zarf tools update-creds artifact
$ zarf tools update-creds agent

# Update all Zarf credentials w/external services at once:
$ zarf tools update-creds \
	--registry-push-username={USERNAME} --registry-push-password={PASSWORD} \
	--git-push-username={USERNAME} --git-push-password={PASSWORD} \
	--artifact-push-username={USERNAME} --artifact-push-token={PASSWORD}

# NOTE: Any credentials omitted from flags without a service key specified will be autogenerated - URLs will only change if specified.
# Config options can also be set with the 'init' section of a Zarf config file.

# Update specific Zarf credentials w/external services:
$ zarf tools update-creds registry --registry-push-username={USERNAME} --registry-push-password={PASSWORD}
$ zarf tools update-creds git --git-push-username={USERNAME} --git-push-password={PASSWORD}
$ zarf tools update-creds artifact --artifact-push-username={USERNAME} --artifact-push-token={PASSWORD}

# NOTE: Not specifying a pull username/password will keep the previous pull username/password.
`
	CmdToolsUpdateCredsConfirmFlag          = "Confirm updating credentials without prompting"
	CmdToolsUpdateCredsConfirmProvided      = "Confirm flag specified, continuing without prompting."
	CmdToolsUpdateCredsConfirmContinue      = "Continue with these changes?"
	CmdToolsUpdateCredsUnableUpdateRegistry = "Unable to update Zarf Registry values: %s"
	CmdToolsUpdateCredsUnableUpdateAgent    = "Unable to update Zarf Agent TLS secrets: %s"
	CmdToolsUpdateCredsUnableUpdateCreds    = "Unable to update Zarf credentials"

	// zarf version
	CmdVersionShort = "Shows the version of the running Zarf binary"
	CmdVersionLong  = "Displays the version of the Zarf release that the current binary was built from."

	// tools version
	CmdToolsVersionShort = "Print the version"
)

// Zarf Agent messages
// These are only seen in the Kubernetes logs.
const (
	AgentErrBadRequest             = "could not read request body: %s"
	AgentErrCouldNotDeserializeReq = "could not deserialize request: %s"
	AgentErrParsePod               = "failed to parse pod: %w"
	AgentErrHostnameMatch          = "failed to complete hostname matching: %w"
	AgentErrInvalidMethod          = "invalid method only POST requests are allowed"
	AgentErrInvalidOp              = "invalid operation: %s"
	AgentErrInvalidType            = "only content type 'application/json' is supported"
	AgentErrMarshallJSONPatch      = "unable to marshall the json patch"
	AgentErrMarshalResponse        = "unable to marshal the response"
	AgentErrNilReq                 = "malformed admission review: request is nil"
)

// Package create
const (
	PkgCreateErrDifferentialSameVersion = "unable to create differential package. Please ensure the differential package version and reference package version are not the same. The package version must be incremented"
	PkgCreateErrDifferentialNoVersion   = "unable to create differential package. Please ensure both package versions are set"
)

// Collection of reusable error messages.
var (
	ErrInitNotFound        = errors.New("this command requires a zarf-init package, but one was not found on the local system. Re-run the last command again without '--confirm' to download the package")
	ErrUnableToCheckArch   = errors.New("unable to get the configured cluster's architecture")
	ErrUnableToGetPackages = errors.New("unable to load the Zarf Package data from the cluster")
)

// Collection of reusable warn messages.
var (
	WarnSGetDeprecation = "Using sget to download resources is being deprecated and will removed in the v1.0.0 release of Zarf. Please publish the packages as OCI artifacts instead."
)
