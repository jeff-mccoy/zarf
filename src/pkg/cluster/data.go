// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package cluster contains Zarf-specific cluster management functions.
package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/defenseunicorns/pkg/helpers/v2"

	"github.com/defenseunicorns/zarf/src/config"
	"github.com/defenseunicorns/zarf/src/pkg/layout"
	"github.com/defenseunicorns/zarf/src/pkg/message"
	"github.com/defenseunicorns/zarf/src/pkg/utils"
	"github.com/defenseunicorns/zarf/src/pkg/utils/exec"
	"github.com/defenseunicorns/zarf/src/types"
)

// HandleDataInjection waits for the target pod(s) to come up and inject the data into them
// todo:  this currently requires kubectl but we should have enough k8s work to make this native now.
func (c *Cluster) HandleDataInjection(ctx context.Context, wg *sync.WaitGroup, data types.ZarfDataInjection, componentPath *layout.ComponentPaths, dataIdx int) {
	defer wg.Done()
	injectionCompletionMarker := filepath.Join(componentPath.DataInjections, config.GetDataInjectionMarker())
	if err := os.WriteFile(injectionCompletionMarker, []byte("🦄"), helpers.ReadWriteUser); err != nil {
		message.WarnErrf(err, "Unable to create the data injection completion marker")
		return
	}

	tarCompressFlag := ""
	if data.Compress {
		tarCompressFlag = "-z"
	}

	// Pod filter to ensure we only use the current deployment's pods
	podFilterByInitContainer := func(pod corev1.Pod) bool {
		// Look everywhere in the pod for a matching data injection marker
		return strings.Contains(message.JSONValue(pod), config.GetDataInjectionMarker())
	}

	// Get the OS shell to execute commands in
	shell, shellArgs := exec.GetOSShell(exec.Shell{Windows: "cmd"})

	if _, _, err := exec.Cmd(shell, append(shellArgs, "tar --version")...); err != nil {
		message.WarnErr(err, "Unable to execute tar on this system.  Please ensure it is installed and on your $PATH.")
		return
	}

iterator:
	// The eternal loop because some data injections can take a very long time
	for {
		message.Debugf("Attempting to inject data into %s", data.Target)
		source := filepath.Join(componentPath.DataInjections, filepath.Base(data.Target.Path))
		if helpers.InvalidPath(source) {
			// The path is likely invalid because of how we compose OCI components, add an index suffix to the filename
			source = filepath.Join(componentPath.DataInjections, strconv.Itoa(dataIdx), filepath.Base(data.Target.Path))
			if helpers.InvalidPath(source) {
				message.Warnf("Unable to find the data injection source path %s", source)
				return
			}
		}

		target := podLookup{
			Namespace: data.Target.Namespace,
			Selector:  data.Target.Selector,
			Container: data.Target.Container,
		}

		// Wait until the pod we are injecting data into becomes available
		pods := waitForPodsAndContainers(ctx, c.Clientset, target, podFilterByInitContainer)
		if len(pods) < 1 {
			continue
		}

		// Inject into all the pods
		for _, pod := range pods {
			// Try to use the embedded kubectl if we can
			zarfCommand, err := utils.GetFinalExecutableCommand()
			kubectlBinPath := "kubectl"
			if err != nil {
				message.Warnf("Unable to get the zarf executable path, falling back to host kubectl: %s", err)
			} else {
				kubectlBinPath = fmt.Sprintf("%s tools kubectl", zarfCommand)
			}
			kubectlCmd := fmt.Sprintf("%s exec -i -n %s %s -c %s ", kubectlBinPath, data.Target.Namespace, pod.Name, data.Target.Container)

			// Note that each command flag is separated to provide the widest cross-platform tar support
			tarCmd := fmt.Sprintf("tar -c %s -f -", tarCompressFlag)
			untarCmd := fmt.Sprintf("tar -x %s -v -f - -C %s", tarCompressFlag, data.Target.Path)

			// Must create the target directory before trying to change to it for untar
			mkdirCmd := fmt.Sprintf("%s -- mkdir -p %s", kubectlCmd, data.Target.Path)
			if err := exec.CmdWithPrint(shell, append(shellArgs, mkdirCmd)...); err != nil {
				message.Warnf("Unable to create the data injection target directory %s in pod %s", data.Target.Path, pod.Name)
				continue iterator
			}

			cpPodCmd := fmt.Sprintf("%s -C %s . | %s -- %s",
				tarCmd,
				source,
				kubectlCmd,
				untarCmd,
			)

			// Do the actual data injection
			if err := exec.CmdWithPrint(shell, append(shellArgs, cpPodCmd)...); err != nil {
				message.Warnf("Error copying data into the pod %#v: %#v\n", pod.Name, err)
				continue iterator
			}

			// Leave a marker in the target container for pods to track the sync action
			cpPodCmd = fmt.Sprintf("%s -C %s %s | %s -- %s",
				tarCmd,
				componentPath.DataInjections,
				config.GetDataInjectionMarker(),
				kubectlCmd,
				untarCmd,
			)

			if err := exec.CmdWithPrint(shell, append(shellArgs, cpPodCmd)...); err != nil {
				message.Warnf("Error saving the zarf sync completion file after injection into pod %#v\n", pod.Name)
				continue iterator
			}
		}

		// Do not look for a specific container after injection in case they are running an init container
		podOnlyTarget := podLookup{
			Namespace: data.Target.Namespace,
			Selector:  data.Target.Selector,
		}

		// Block one final time to make sure at least one pod has come up and injected the data
		// Using only the pod as the final selector because we don't know what the container name will be
		// Still using the init container filter to make sure we have the right running pod
		_ = waitForPodsAndContainers(ctx, c.Clientset, podOnlyTarget, podFilterByInitContainer)

		// Cleanup now to reduce disk pressure
		_ = os.RemoveAll(source)

		// Return to stop the loop
		return
	}
}

// podLookup is a struct for specifying a pod to target for data injection or lookups.
type podLookup struct {
	Namespace string
	Selector  string
	Container string
}

// podFilter is a function that returns true if the pod should be targeted for data injection or lookups.
type podFilter func(pod corev1.Pod) bool

// WaitForPodsAndContainers attempts to find pods matching the given selector and optional inclusion filter
// It will wait up to 90 seconds for the pods to be found and will return a list of matching pod names
// If the timeout is reached, an empty list will be returned.
// TODO: Test, refactor and/or remove.
func waitForPodsAndContainers(ctx context.Context, clientset kubernetes.Interface, target podLookup, include podFilter) []corev1.Pod {
	waitCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-waitCtx.Done():
			message.Debug("Pod lookup failed: %v", ctx.Err())
			return nil
		case <-timer.C:
			listOpts := metav1.ListOptions{
				LabelSelector: target.Selector,
			}
			podList, err := clientset.CoreV1().Pods(target.Namespace).List(ctx, listOpts)
			if err != nil {
				message.Debug("Unable to find matching pods: %w", err)
				return nil
			}

			message.Debug("Found %d pods for target %#v", len(podList.Items), target)

			var readyPods = []corev1.Pod{}

			// Sort the pods from newest to oldest
			sort.Slice(podList.Items, func(i, j int) bool {
				return podList.Items[i].CreationTimestamp.After(podList.Items[j].CreationTimestamp.Time)
			})

			for _, pod := range podList.Items {
				message.Debug("Testing pod %q", pod.Name)

				// If an include function is provided, only keep pods that return true
				if include != nil && !include(pod) {
					continue
				}

				// Handle container targeting
				if target.Container != "" {
					message.Debug("Testing pod %q for container %q", pod.Name, target.Container)

					// Check the status of initContainers for a running match
					for _, initContainer := range pod.Status.InitContainerStatuses {
						isRunning := initContainer.State.Running != nil
						if initContainer.Name == target.Container && isRunning {
							// On running match in initContainer break this loop
							readyPods = append(readyPods, pod)
							break
						}
					}

					// Check the status of regular containers for a running match
					for _, container := range pod.Status.ContainerStatuses {
						isRunning := container.State.Running != nil
						if container.Name == target.Container && isRunning {
							readyPods = append(readyPods, pod)
							break
						}
					}
				} else {
					status := pod.Status.Phase
					message.Debug("Testing pod %q phase, want (%q) got (%q)", pod.Name, corev1.PodRunning, status)
					// Regular status checking without a container
					if status == corev1.PodRunning {
						readyPods = append(readyPods, pod)
						break
					}
				}
			}
			if len(readyPods) > 0 {
				return readyPods
			}
			timer.Reset(3 * time.Second)
		}
	}
}
