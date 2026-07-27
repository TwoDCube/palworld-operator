/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"crypto/rand"
	"math/big"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	palworldv1alpha1 "github.com/twodcube/palworld-operator/api/v1alpha1"
)

// Default Secret keys for server credentials.
const (
	DefaultAdminPasswordKey  = "adminPassword"
	DefaultServerPasswordKey = "serverPassword"
)

const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// GeneratePassword returns a cryptographically-random password of length n
// using an alphanumeric alphabet (avoids INI-breaking characters).
func GeneratePassword(n int) (string, error) {
	b := make([]byte, n)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = passwordAlphabet[idx.Int64()]
	}
	return string(b), nil
}

// AdminPasswordKey returns the Secret key holding the admin password.
func AdminPasswordKey(g *palworldv1alpha1.PalworldGame) string {
	if g.Spec.Credentials.AdminPasswordKey != "" {
		return g.Spec.Credentials.AdminPasswordKey
	}
	return DefaultAdminPasswordKey
}

// ServerPasswordKey returns the Secret key holding the join password.
func ServerPasswordKey(g *palworldv1alpha1.PalworldGame) string {
	if g.Spec.Credentials.ServerPasswordKey != "" {
		return g.Spec.Credentials.ServerPasswordKey
	}
	return DefaultServerPasswordKey
}

// CredentialsSecretName returns the name of the Secret the server reads
// credentials from: a user-supplied Secret if set, else the generated one.
func CredentialsSecretName(g *palworldv1alpha1.PalworldGame) string {
	if g.Spec.Credentials.SecretName != "" {
		return g.Spec.Credentials.SecretName
	}
	return GeneratedSecretName(g)
}

// DesiredGeneratedSecret builds the operator-managed credentials Secret using
// the supplied (freshly generated) admin password. It is only created when the
// user has not supplied their own Secret, and only on first creation so
// passwords are stable across reconciles.
func DesiredGeneratedSecret(g *palworldv1alpha1.PalworldGame, adminPassword string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GeneratedSecretName(g),
			Namespace: g.Namespace,
			Labels:    CommonLabels(g),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			DefaultAdminPasswordKey:  adminPassword,
			DefaultServerPasswordKey: "",
		},
	}
}
