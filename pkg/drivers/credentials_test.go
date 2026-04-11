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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var _ CredentialResolver = (*SecretCredentialResolver)(nil)

func TestSecretCredentialResolver_Resolve(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-creds",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"endpoint-token": []byte("super-secret-token"),
			"backup-key":     []byte("another-secret"),
		},
	}

	tests := []struct {
		name      string
		source    CredentialSource
		secrets   []*corev1.Secret
		wantData  []byte
		wantErr   error
		wantInErr string
	}{
		{
			name: "valid SecretRef returns correct credential bytes",
			source: CredentialSource{
				SecretRef: &SecretRef{
					Name:      "storage-creds",
					Namespace: "openshift-storage",
					Key:       "endpoint-token",
				},
			},
			secrets:  []*corev1.Secret{secret},
			wantData: []byte("super-secret-token"),
		},
		{
			name: "secret with multiple keys extracts correct key",
			source: CredentialSource{
				SecretRef: &SecretRef{
					Name:      "storage-creds",
					Namespace: "openshift-storage",
					Key:       "backup-key",
				},
			},
			secrets:  []*corev1.Secret{secret},
			wantData: []byte("another-secret"),
		},
		{
			name: "secret not found returns ErrSecretNotFound",
			source: CredentialSource{
				SecretRef: &SecretRef{
					Name:      "nonexistent",
					Namespace: "default",
					Key:       "token",
				},
			},
			secrets: nil,
			wantErr: ErrSecretNotFound,
		},
		{
			name: "key not found in secret returns ErrSecretKeyNotFound",
			source: CredentialSource{
				SecretRef: &SecretRef{
					Name:      "storage-creds",
					Namespace: "openshift-storage",
					Key:       "missing-key",
				},
			},
			secrets:   []*corev1.Secret{secret},
			wantErr:   ErrSecretKeyNotFound,
			wantInErr: "available: backup-key, endpoint-token",
		},
		{
			name: "VaultRef returns ErrVaultNotImplemented",
			source: CredentialSource{
				VaultRef: &VaultRef{
					Path: "secret/data/storage",
					Role: "soteria",
					Key:  "token",
				},
			},
			wantErr: ErrVaultNotImplemented,
		},
		{
			name:    "both refs nil returns ErrNoCredentialSource",
			source:  CredentialSource{},
			wantErr: ErrNoCredentialSource,
		},
		{
			name: "both refs set returns ErrAmbiguousSource",
			source: CredentialSource{
				SecretRef: &SecretRef{Name: "s", Namespace: "ns", Key: "k"},
				VaultRef:  &VaultRef{Path: "p", Role: "r", Key: "k"},
			},
			wantErr: ErrAmbiguousSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fakeClient *fakeclient.Clientset
			if len(tt.secrets) > 0 {
				objs := make([]runtime.Object, len(tt.secrets))
				for i, s := range tt.secrets {
					objs[i] = s
				}
				fakeClient = fakeclient.NewClientset(objs...)
			} else {
				fakeClient = fakeclient.NewClientset()
			}

			resolver := &SecretCredentialResolver{
				Client: fakeClient.CoreV1(),
			}

			got, err := resolver.Resolve(context.Background(), tt.source)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error wrapping %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error wrapping %v, got: %v", tt.wantErr, err)
				}
				if tt.wantInErr != "" {
					if msg := err.Error(); !strings.Contains(msg, tt.wantInErr) {
						t.Fatalf("expected error to contain %q, got: %s", tt.wantInErr, msg)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != string(tt.wantData) {
				t.Fatalf("got %q, want %q", got, tt.wantData)
			}
		})
	}
}

func TestSecretCredentialResolver_ContextCancelled(t *testing.T) {
	fakeClient := fakeclient.NewClientset()
	fakeClient.PrependReactor("get", "secrets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, context.Canceled
	})

	resolver := &SecretCredentialResolver{Client: fakeClient.CoreV1()}
	_, err := resolver.Resolve(context.Background(), CredentialSource{
		SecretRef: &SecretRef{Name: "s", Namespace: "ns", Key: "k"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrSecretNotFound) {
		t.Fatal("context.Canceled should NOT be wrapped as ErrSecretNotFound")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled to be propagated, got: %v", err)
	}
}

func TestCredentialSource_Validation(t *testing.T) {
	tests := []struct {
		name   string
		source CredentialSource
		valid  bool
	}{
		{
			name: "SecretRef only is valid",
			source: CredentialSource{
				SecretRef: &SecretRef{Name: "s", Namespace: "ns", Key: "k"},
			},
			valid: true,
		},
		{
			name: "VaultRef only is valid",
			source: CredentialSource{
				VaultRef: &VaultRef{Path: "p", Role: "r", Key: "k"},
			},
			valid: true,
		},
		{
			name:   "both nil is invalid",
			source: CredentialSource{},
			valid:  false,
		},
		{
			name: "both set is invalid",
			source: CredentialSource{
				SecretRef: &SecretRef{Name: "s", Namespace: "ns", Key: "k"},
				VaultRef:  &VaultRef{Path: "p", Role: "r", Key: "k"},
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasExactlyOne := (tt.source.SecretRef != nil) != (tt.source.VaultRef != nil)
			if hasExactlyOne != tt.valid {
				t.Fatalf("expected valid=%v for source %+v", tt.valid, tt.source)
			}
		})
	}
}
