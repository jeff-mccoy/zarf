// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package k8s provides a client for interacting with a Kubernetes cluster.	 	
package k8s

import (
	"context"
	"crypto/tls"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (k *Client) GetSecret(namespace, name string) (*corev1.Secret, error) {
	return k.Clientset.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (k *Client) GetSecretsWithLabel(namespace, labelSelector string) (*corev1.SecretList, error) {
	listOptions := metav1.ListOptions{LabelSelector: labelSelector}
	return k.Clientset.CoreV1().Secrets(namespace).List(context.TODO(), listOptions)
}

func (k *Client) GenerateSecret(namespace, name string, secretType corev1.SecretType) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    k.Labels,
		},
		Type: secretType,
		Data: map[string][]byte{},
	}
}

func (k *Client) GenerateTLSSecret(namespace, name string, conf GeneratedPKI) (*corev1.Secret, error) {
	if _, err := tls.X509KeyPair(conf.Cert, conf.Key); err != nil {
		return nil, err
	}

	secretTLS := k.GenerateSecret(namespace, name, corev1.SecretTypeTLS)
	secretTLS.Data[corev1.TLSCertKey] = conf.Cert
	secretTLS.Data[corev1.TLSPrivateKeyKey] = conf.Key

	return secretTLS, nil
}

func (k *Client) ReplaceTLSSecret(namespace, name string, conf GeneratedPKI) error {
	secret, err := k.GenerateTLSSecret(namespace, name, conf)
	if err != nil {
		return err
	}

	return k.ReplaceSecret(secret)
}

func (k *Client) ReplaceSecret(secret *corev1.Secret) error {
	if _, err := k.CreateNamespace(secret.Namespace, nil); err != nil {
		return fmt.Errorf("unable to create or read the namespace: %w", err)
	}

	if err := k.DeleteSecret(secret); err != nil {
		return err
	}

	return k.CreateSecret(secret)
}

func (k *Client) DeleteSecret(secret *corev1.Secret) error {
	namespaceSecrets := k.Clientset.CoreV1().Secrets(secret.Namespace)

	err := namespaceSecrets.Delete(context.TODO(), secret.Name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("error deleting the secret: %w", err)
	}

	return nil
}

func (k *Client) CreateSecret(secret *corev1.Secret) error {
	namespaceSecrets := k.Clientset.CoreV1().Secrets(secret.Namespace)

	// create the given secret
	if _, err := namespaceSecrets.Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("unable to create the secret: %w", err)
	}

	return nil
}
