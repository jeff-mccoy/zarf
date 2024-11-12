// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package test provides e2e tests for Zarf.
package test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/internal/gitea"
	"github.com/zarf-dev/zarf/src/pkg/cluster"
	"github.com/zarf-dev/zarf/src/types"
)

func TestGit(t *testing.T) {
	t.Log("E2E: Git")

	buildPath := filepath.Join("src", "test", "packages", "22-git-data")
	stdOut, stdErr, err := e2e.Zarf(t, "package", "create", buildPath, "-o=build", "--confirm")
	require.NoError(t, err, stdOut, stdErr)

	path := fmt.Sprintf("build/zarf-package-git-data-test-%s-1.0.0.tar.zst", e2e.Arch)
	defer e2e.CleanFiles(t, path)

	// Deploy the git data example (with component globbing to test that as well)
	stdOut, stdErr, err = e2e.Zarf(t, "package", "deploy", path, "--components=full-repo,specific-*", "--confirm")
	require.NoError(t, err, stdOut, stdErr)

	c, err := cluster.NewCluster()
	require.NoError(t, err)
	ctx := context.Background()
	tunnelGit, err := c.Connect(ctx, cluster.ZarfGit)
	require.NoError(t, err)
	defer tunnelGit.Close()

	testGitServerConnect(t, tunnelGit.HTTPEndpoint())
	testGitServerReadOnly(ctx, t, tunnelGit.HTTPEndpoint())
	testGitServerTagAndHash(ctx, t, tunnelGit.HTTPEndpoint())
}

func TestGitOpsFlux(t *testing.T) {
	t.Log("E2E: GitOps / Flux")

	waitFluxPodInfoDeployment(t)
}

func TestGitOpsArgoCD(t *testing.T) {
	t.Log("E2E: ArgoCD / Flux")

	waitArgoDeployment(t)
}

func testGitServerConnect(t *testing.T, gitURL string) {
	// Make sure Gitea comes up cleanly
	resp, err := http.Get(gitURL + "/explore/repos")
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
}

func testGitServerReadOnly(ctx context.Context, t *testing.T, gitURL string) {
	timeoutCtx, cancel := context.WithTimeout(ctx, cluster.DefaultTimeout)
	defer cancel()
	c, err := cluster.NewClusterWithWait(timeoutCtx)
	require.NoError(t, err)

	// Init the state variable
	state, err := c.LoadZarfState(ctx)
	require.NoError(t, err)
	giteaClient, err := gitea.NewClient(gitURL, types.ZarfGitReadUser, state.GitServer.PullPassword)
	require.NoError(t, err)
	repoName := "zarf-public-test-2363058019"

	// Get the repo as the readonly user
	b, statusCode, err := giteaClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s", state.GitServer.PushUsername, repoName), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, statusCode)

	// Make sure the only permissions are pull (read)
	var bodyMap map[string]interface{}
	err = json.Unmarshal(b, &bodyMap)
	require.NoError(t, err)
	permissionsMap, ok := bodyMap["permissions"].(map[string]interface{})
	require.True(t, ok, "permissions key is not of right type")
	admin, ok := permissionsMap["admin"].(bool)
	require.True(t, ok)
	require.False(t, admin)
	push, ok := permissionsMap["push"].(bool)
	require.True(t, ok)
	require.False(t, push)
	pull, ok := permissionsMap["pull"].(bool)
	require.True(t, ok)
	require.True(t, pull)
}

func testGitServerTagAndHash(ctx context.Context, t *testing.T, gitURL string) {
	timeoutCtx, cancel := context.WithTimeout(ctx, cluster.DefaultTimeout)
	defer cancel()
	c, err := cluster.NewClusterWithWait(timeoutCtx)
	require.NoError(t, err)

	// Init the state variable
	state, err := c.LoadZarfState(ctx)
	require.NoError(t, err, "Failed to load Zarf state")
	giteaClient, err := gitea.NewClient(gitURL, types.ZarfGitReadUser, state.GitServer.PullPassword)
	require.NoError(t, err)
	repoName := "zarf-public-test-2363058019"

	// Make sure the pushed tag exists
	repoTag := "v0.0.1"
	b, statusCode, err := giteaClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s/tags/%s", types.ZarfGitPushUser, repoName, repoTag), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, statusCode)
	var tagMap map[string]interface{}
	err = json.Unmarshal(b, &tagMap)
	require.NoError(t, err)
	require.Equal(t, repoTag, tagMap["name"])

	// Get the Zarf repo commit
	repoHash := "01a23218923f24194133b5eb11268cf8d73ff1bb"
	b, statusCode, err = giteaClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/repos/%s/%s/git/commits/%s", types.ZarfGitPushUser, repoName, repoHash), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, statusCode)
	require.NoError(t, err)
	require.Contains(t, string(b), repoHash)
}

func waitFluxPodInfoDeployment(t *testing.T) {
	ctx := context.Background()
	cluster, err := cluster.NewClusterWithWait(ctx)
	require.NoError(t, err)
	zarfState, err := cluster.LoadZarfState(ctx)
	require.NoError(t, err, "Failed to load Zarf state")
	registryAddress, err := cluster.GetServiceInfoFromRegistryAddress(ctx, zarfState.RegistryInfo.Address)
	require.NoError(t, err)
	// Deploy the flux example and verify that it works
	path := fmt.Sprintf("build/zarf-package-podinfo-flux-%s.tar.zst", e2e.Arch)
	stdOut, stdErr, err := e2e.Zarf(t, "package", "deploy", path, "--confirm")
	require.NoError(t, err, stdOut, stdErr)

	// Tests the URL mutation for GitRepository CRD for Flux.
	stdOut, stdErr, err = e2e.Kubectl(t, "get", "gitrepositories", "podinfo", "-n", "flux-system", "-o", "jsonpath={.spec.url}")
	require.NoError(t, err, stdOut, stdErr)
	expectedMutatedRepoURL := fmt.Sprintf("%s/%s/podinfo-1646971829.git", types.ZarfInClusterGitServiceURL, types.ZarfGitPushUser)
	require.Equal(t, expectedMutatedRepoURL, stdOut)

	// Tests the URL mutation for HelmRepository CRD for Flux.
	stdOut, stdErr, err = e2e.Kubectl(t, "get", "helmrepositories", "podinfo", "-n", "flux-system", "-o", "jsonpath={.spec.url}")
	require.NoError(t, err, stdOut, stdErr)
	expectedMutatedRepoURL = fmt.Sprintf("oci://%s/stefanprodan/charts", registryAddress)
	require.Equal(t, expectedMutatedRepoURL, stdOut)
	stdOut, stdErr, err = e2e.Kubectl(t, "get", "helmrelease", "podinfo", "-n", "flux-system", "-o", "jsonpath={.spec.chart.spec.version}")
	require.NoError(t, err, stdOut, stdErr)
	expectedMutatedRepoTag := "6.4.0"
	require.Equal(t, expectedMutatedRepoTag, stdOut)

	// Tests the URL mutation for OCIRepository CRD for Flux.
	stdOut, stdErr, err = e2e.Kubectl(t, "get", "ocirepositories", "podinfo", "-n", "flux-system", "-o", "jsonpath={.spec.url}")
	require.NoError(t, err, stdOut, stdErr)
	expectedMutatedRepoURL = fmt.Sprintf("oci://%s/stefanprodan/manifests/podinfo", registryAddress)
	require.Equal(t, expectedMutatedRepoURL, stdOut)
	stdOut, stdErr, err = e2e.Kubectl(t, "get", "ocirepositories", "podinfo", "-n", "flux-system", "-o", "jsonpath={.spec.ref.tag}")
	require.NoError(t, err, stdOut, stdErr)
	expectedMutatedRepoTag = "6.4.0-zarf-2823281104"
	require.Equal(t, expectedMutatedRepoTag, stdOut)

	// Remove the flux example when deployment completes
	stdOut, stdErr, err = e2e.Zarf(t, "package", "remove", "podinfo-flux", "--confirm")
	require.NoError(t, err, stdOut, stdErr)

	// Prune the flux images to reduce disk pressure
	stdOut, stdErr, err = e2e.Zarf(t, "tools", "registry", "prune", "--confirm")
	require.NoError(t, err, stdOut, stdErr)
}

func waitArgoDeployment(t *testing.T) {
	// Deploy the argocd example and verify that it works
	path := fmt.Sprintf("build/zarf-package-argocd-%s.tar.zst", e2e.Arch)
	stdOut, stdErr, err := e2e.Zarf(t, "package", "deploy", path, "--components=argocd-apps", "--confirm")
	require.NoError(t, err, stdOut, stdErr)

	expectedMutatedRepoURL := fmt.Sprintf("%s/%s/podinfo-1646971829.git", types.ZarfInClusterGitServiceURL, types.ZarfGitPushUser)

	// Tests the mutation of the private repository Secret for ArgoCD.
	stdOut, stdErr, err = e2e.Kubectl(t, "get", "secret", "argocd-repo-github-podinfo", "-n", "argocd", "-o", "jsonpath={.data.url}")
	require.NoError(t, err, stdOut, stdErr)

	expectedMutatedPrivateRepoURLSecret, err := base64.StdEncoding.DecodeString(stdOut)
	require.NoError(t, err, stdOut, stdErr)
	require.Equal(t, expectedMutatedRepoURL, string(expectedMutatedPrivateRepoURLSecret))

	// Tests the mutation of the repoURL for Application CRD source(s) for ArgoCD.
	stdOut, stdErr, err = e2e.Kubectl(t, "get", "application", "apps", "-n", "argocd", "-o", "jsonpath={.spec.sources[0].repoURL}")
	require.NoError(t, err, stdOut, stdErr)
	require.Equal(t, expectedMutatedRepoURL, stdOut)

	// Remove the argocd example when deployment completes
	stdOut, stdErr, err = e2e.Zarf(t, "package", "remove", "argocd", "--confirm")
	require.NoError(t, err, stdOut, stdErr)

	// Prune the ArgoCD images to reduce disk pressure
	stdOut, stdErr, err = e2e.Zarf(t, "tools", "registry", "prune", "--confirm")
	require.NoError(t, err, stdOut, stdErr)
}
