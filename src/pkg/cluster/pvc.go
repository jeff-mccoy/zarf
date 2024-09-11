// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

package cluster

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UpdateGiteaPVC updates the existing Gitea persistent volume claim and tells Gitea whether to create or not.
// REVIEW(mkcp): It looks like we've got some error+signal coming back from these functions where we return a both a
// string true/false downstream but sometimes with errors. So I'm not going to make these `return "", err` but we may
// want to consider if returning the error is necessary and not, for example, better served with a new type.
func (c *Cluster) UpdateGiteaPVC(ctx context.Context, pvcName string, shouldRollBack bool) (string, error) {
	if shouldRollBack {
		pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(ZarfNamespaceName).Get(ctx, pvcName, metav1.GetOptions{})
		if err != nil {
			return "false", err
		}
		delete(pvc.Labels, "app.kubernetes.io/managed-by")
		delete(pvc.Annotations, "meta.helm.sh/release-name")
		delete(pvc.Annotations, "meta.helm.sh/release-namespace")
		_, err = c.Clientset.CoreV1().PersistentVolumeClaims(ZarfNamespaceName).Update(ctx, pvc, metav1.UpdateOptions{})
		if err != nil {
			return "false", err
		}
		return "false", nil
	}

	if pvcName == "data-zarf-gitea-0" {
		pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(ZarfNamespaceName).Get(ctx, pvcName, metav1.GetOptions{})
		if err != nil {
			return "true", err
		}
		pvc.Labels["app.kubernetes.io/managed-by"] = "Helm"
		pvc.Annotations["meta.helm.sh/release-name"] = "zarf-gitea"
		pvc.Annotations["meta.helm.sh/release-namespace"] = "zarf"
		_, err = c.Clientset.CoreV1().PersistentVolumeClaims(ZarfNamespaceName).Update(ctx, pvc, metav1.UpdateOptions{})
		if err != nil {
			return "true", err
		}
		return "true", nil
	}

	return "false", nil
}
