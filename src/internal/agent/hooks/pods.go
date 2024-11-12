// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package hooks provides HTTP handlers for the mutating webhook.
package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/internal/agent/operations"
	"github.com/zarf-dev/zarf/src/pkg/cluster"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	v1 "k8s.io/api/admission/v1"

	corev1 "k8s.io/api/core/v1"
)

const annotationPrefix = "zarf.dev"

// NewPodMutationHook creates a new instance of pods mutation hook.
func NewPodMutationHook(ctx context.Context, cluster *cluster.Cluster) operations.Hook {
	return operations.Hook{
		Create: func(r *v1.AdmissionRequest) (*operations.Result, error) {
			return mutatePod(ctx, r, cluster)
		},
		Update: func(r *v1.AdmissionRequest) (*operations.Result, error) {
			return mutatePod(ctx, r, cluster)
		},
	}
}

func parsePod(object []byte) (*corev1.Pod, error) {
	var pod corev1.Pod
	if err := json.Unmarshal(object, &pod); err != nil {
		return nil, err
	}
	return &pod, nil
}

func getImageAnnotationKey(containerName string) string {
	return fmt.Sprintf("%s/original-image-%s", annotationPrefix, containerName)
}

func mutatePod(ctx context.Context, r *v1.AdmissionRequest, cluster *cluster.Cluster) (*operations.Result, error) {
	l := logger.From(ctx)
	pod, err := parsePod(r.Object.Raw)
	if err != nil {
		return nil, fmt.Errorf(lang.AgentErrParsePod, err)
	}

	if pod.Labels != nil && pod.Labels["zarf-agent"] == "patched" {
		// We've already played with this pod, just keep swimming 🐟
		return &operations.Result{
			Allowed:  true,
			PatchOps: []operations.PatchOperation{},
		}, nil
	}

	state, err := cluster.LoadZarfState(ctx)
	if err != nil {
		return nil, err
	}
	registryURL := state.RegistryInfo.Address

	// Pods do not have a metadata.name at the time of admission if from a deployment so we don't log the name
	l.Info("using the Zarf registry URL to mutate the Pod", "registry", registryURL)

	var patches []operations.PatchOperation

	// Add the zarf secret to the podspec
	zarfSecret := []corev1.LocalObjectReference{{Name: config.ZarfImagePullSecretName}}
	patches = append(patches, operations.ReplacePatchOperation("/spec/imagePullSecrets", zarfSecret))

	updatedAnnotations := pod.Annotations
	if updatedAnnotations == nil {
		updatedAnnotations = make(map[string]string)
	}

	// update the image host for each init container
	for idx, container := range pod.Spec.InitContainers {
		path := fmt.Sprintf("/spec/initContainers/%d/image", idx)
		replacement, err := transform.ImageTransformHost(registryURL, container.Image)
		if err != nil {
			return nil, err
		}
		updatedAnnotations[getImageAnnotationKey(container.Name)] = container.Image
		patches = append(patches, operations.ReplacePatchOperation(path, replacement))
	}

	// update the image host for each ephemeral container
	for idx, container := range pod.Spec.EphemeralContainers {
		path := fmt.Sprintf("/spec/ephemeralContainers/%d/image", idx)
		replacement, err := transform.ImageTransformHost(registryURL, container.Image)
		if err != nil {
			return nil, err
		}
		updatedAnnotations[getImageAnnotationKey(container.Name)] = container.Image
		patches = append(patches, operations.ReplacePatchOperation(path, replacement))
	}

	// update the image host for each normal container
	for idx, container := range pod.Spec.Containers {
		path := fmt.Sprintf("/spec/containers/%d/image", idx)
		replacement, err := transform.ImageTransformHost(registryURL, container.Image)
		if err != nil {
			return nil, err
		}
		updatedAnnotations[getImageAnnotationKey(container.Name)] = container.Image
		patches = append(patches, operations.ReplacePatchOperation(path, replacement))
	}

	// update the image host for each volume that contains a "image" reference
	for idx, volume := range pod.Spec.Volumes {
		if volume.Image != nil {
			path := fmt.Sprintf("/spec/volume/%d/image/reference", idx)
			replacement, err := transform.ImageTransformHost(registryURL, volume.Image.Reference)
			if err != nil {
				return nil, err
			}
			updatedAnnotations[getImageAnnotationKey(volume.Image.Reference)] = volume.Image.Reference
			patches = append(patches, operations.ReplacePatchOperation(path, replacement))
		}
	}

	patches = append(patches, getLabelPatch(pod.Labels))

	patches = append(patches, operations.ReplacePatchOperation("/metadata/annotations", updatedAnnotations))

	return &operations.Result{
		Allowed:  true,
		PatchOps: patches,
	}, nil
}
