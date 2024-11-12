// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/internal/healthchecks"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	v1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

func TestInjector(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset()
	c := &Cluster{
		Clientset: cs,
		Watcher:   healthchecks.NewImmediateWatcher(status.CurrentStatus),
	}
	cs.PrependReactor("delete-collection", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		delAction, ok := action.(k8stesting.DeleteCollectionActionImpl)
		if !ok {
			return false, nil, fmt.Errorf("action is not of type DeleteCollectionActionImpl")
		}
		if delAction.GetListRestrictions().Labels.String() != "zarf-injector=payload" {
			return false, nil, nil
		}
		gvr := delAction.Resource
		gvk := delAction.Resource.GroupVersion().WithKind("ConfigMap")
		list, err := cs.Tracker().List(gvr, gvk, delAction.Namespace)
		require.NoError(t, err)
		cmList, ok := list.(*corev1.ConfigMapList)
		require.True(t, ok)
		for _, cm := range cmList.Items {
			v, ok := cm.Labels["zarf-injector"]
			if !ok {
				continue
			}
			if v != "payload" {
				continue
			}
			err = cs.Tracker().Delete(gvr, delAction.Namespace, cm.Name)
			require.NoError(t, err)
		}
		return true, nil, nil
	})

	// Setup nodes and pods with images
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10"),
				corev1.ResourceMemory: resource.MustParse("100Gi"),
			},
		},
	}
	_, err := cs.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	require.NoError(t, err)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "good",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName: "node1",
			Containers: []corev1.Container{
				{
					Image: "ubuntu:latest",
				},
			},
		},
	}
	_, err = cs.CoreV1().Pods(pod.ObjectMeta.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(t, err)

	err = c.StopInjection(ctx)
	require.NoError(t, err)

	for range 2 {
		tmpDir := t.TempDir()
		binData := []byte("foobar")
		err := os.WriteFile(filepath.Join(tmpDir, "zarf-injector"), binData, 0o644)
		require.NoError(t, err)

		idx, err := random.Index(1, 1, 1)
		require.NoError(t, err)
		_, err = layout.Write(filepath.Join(tmpDir, "seed-images"), idx)
		require.NoError(t, err)

		err = c.StartInjection(ctx, tmpDir, t.TempDir(), nil)
		require.NoError(t, err)

		podList, err := cs.CoreV1().Pods(ZarfNamespaceName).List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		require.Len(t, podList.Items, 1)
		require.Equal(t, "injector", podList.Items[0].ObjectMeta.Name)

		svcList, err := cs.CoreV1().Services(ZarfNamespaceName).List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		require.Len(t, svcList.Items, 1)
		expected, err := os.ReadFile("./testdata/expected-injection-service.json")
		require.NoError(t, err)
		svc, err := cs.CoreV1().Services(ZarfNamespaceName).Get(ctx, "zarf-injector", metav1.GetOptions{})
		// Managed fields are auto-set and contain timestamps
		svc.ManagedFields = nil
		require.NoError(t, err)
		b, err := json.MarshalIndent(svc, "", "  ")
		require.NoError(t, err)
		require.Equal(t, strings.TrimSpace(string(expected)), string(b))

		cmList, err := cs.CoreV1().ConfigMaps(ZarfNamespaceName).List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		require.Len(t, cmList.Items, 2)
		cm, err := cs.CoreV1().ConfigMaps(ZarfNamespaceName).Get(ctx, "rust-binary", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, binData, cm.BinaryData["zarf-injector"])
	}

	err = c.StopInjection(ctx)
	require.NoError(t, err)

	podList, err := cs.CoreV1().Pods(ZarfNamespaceName).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, podList.Items)
	svcList, err := cs.CoreV1().Services(ZarfNamespaceName).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, svcList.Items)
	cmList, err := cs.CoreV1().ConfigMaps(ZarfNamespaceName).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, cmList.Items)
}

func TestBuildInjectionPod(t *testing.T) {
	t.Parallel()

	resReq := v1ac.ResourceRequirements().
		WithRequests(corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(".5"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		}).
		WithLimits(
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			})
	pod := buildInjectionPod("injection-node", "docker.io/library/ubuntu:latest", []string{"foo", "bar"}, "shasum", resReq)
	require.Equal(t, "injector", *pod.Name)
	b, err := json.MarshalIndent(pod, "", "  ")
	require.NoError(t, err)

	expected, err := os.ReadFile("./testdata/expected-injection-pod.json")
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(expected)), string(b))
}

func TestGetInjectorImageAndNode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cs := fake.NewClientset()

	c := &Cluster{
		Clientset: cs,
	}

	nodes := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-resources",
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("400m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-schedule-taint",
			},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{
					{
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1000m"),
					corev1.ResourceMemory: resource.MustParse("10Gi"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "good",
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1000m"),
					corev1.ResourceMemory: resource.MustParse("10Gi"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-execute-taint",
			},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{
					{
						Effect: corev1.TaintEffectNoExecute,
					},
				},
			},
			Status: corev1.NodeStatus{
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1000m"),
					corev1.ResourceMemory: resource.MustParse("10Gi"),
				},
			},
		},
	}
	for i, node := range nodes {
		_, err := cs.CoreV1().Nodes().Create(ctx, &node, metav1.CreateOptions{})
		require.NoError(t, err)
		podName := fmt.Sprintf("pod-%d", i)
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				NodeName: node.ObjectMeta.Name,
				InitContainers: []corev1.Container{
					{
						Image: podName + "-init",
					},
				},
				Containers: []corev1.Container{
					{
						Image: podName + "-container",
					},
				},
				EphemeralContainers: []corev1.EphemeralContainer{
					{
						EphemeralContainerCommon: corev1.EphemeralContainerCommon{
							Image: podName + "-ephemeral",
						},
					},
				},
			},
		}
		_, err = cs.CoreV1().Pods(pod.Namespace).Create(ctx, &pod, metav1.CreateOptions{})
		require.NoError(t, err)
	}

	resReq := v1ac.ResourceRequirements().
		WithRequests(corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(".5"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		}).
		WithLimits(
			corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			})
	image, node, err := c.getInjectorImageAndNode(ctx, resReq)
	require.NoError(t, err)
	require.Equal(t, "pod-2-container", image)
	require.Equal(t, "good", node)
}
