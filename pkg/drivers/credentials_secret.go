/*
Copyright 2026 The Soteria Authors.

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

package drivers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	ErrVaultNotImplemented = errors.New("vault credential resolver is not yet implemented")
	ErrNoCredentialSource  = errors.New("credential source has neither SecretRef nor VaultRef set")
	ErrAmbiguousSource     = errors.New(
		"credential source has both SecretRef and VaultRef set, exactly one must be specified",
	)
	ErrSecretNotFound    = errors.New("referenced secret not found")
	ErrSecretKeyNotFound = errors.New("key not found in referenced secret")
)

// SecretCredentialResolver resolves credentials from Kubernetes Secrets.
type SecretCredentialResolver struct {
	Client corev1client.SecretsGetter
}

// Resolve fetches credential bytes from the external source referenced by
// CredentialSource. Only Kubernetes Secret resolution is implemented; Vault
// support returns ErrVaultNotImplemented until a dedicated story adds it.
func (r *SecretCredentialResolver) Resolve(ctx context.Context, source CredentialSource) ([]byte, error) {
	if source.SecretRef != nil && source.VaultRef != nil {
		return nil, ErrAmbiguousSource
	}
	switch {
	case source.SecretRef != nil:
		return r.resolveFromSecret(ctx, source.SecretRef)
	case source.VaultRef != nil:
		return nil, ErrVaultNotImplemented
	default:
		return nil, ErrNoCredentialSource
	}
}

func (r *SecretCredentialResolver) resolveFromSecret(ctx context.Context, ref *SecretRef) ([]byte, error) {
	secret, err := r.Client.Secrets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", ErrSecretNotFound, ref.Namespace, ref.Name)
		}
		return nil, fmt.Errorf("reading secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	value, ok := secret.Data[ref.Key]
	if !ok {
		available := make([]string, 0, len(secret.Data))
		for k := range secret.Data {
			available = append(available, k)
		}
		sort.Strings(available)
		return nil, fmt.Errorf(
			"%w: secret %s/%s does not contain key %q (available: %s)",
			ErrSecretKeyNotFound, ref.Namespace, ref.Name, ref.Key, strings.Join(available, ", "),
		)
	}

	return value, nil
}
