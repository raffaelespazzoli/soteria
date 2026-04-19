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

package preflight

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	storagev1client "k8s.io/client-go/kubernetes/typed/storage/v1"

	"github.com/soteria-project/soteria/pkg/drivers"
)

var _ drivers.StorageClassLister = (*KubeStorageClassLister)(nil)

// KubeStorageClassLister resolves a StorageClass name to its CSI provisioner
// by reading from the Kubernetes API. This is the production implementation
// of drivers.StorageClassLister used by the preflight resolver and wired in
// cmd/soteria/main.go.
type KubeStorageClassLister struct {
	Client storagev1client.StorageV1Interface
}

func (l *KubeStorageClassLister) GetProvisioner(ctx context.Context, storageClassName string) (string, error) {
	if l.Client == nil {
		return "", fmt.Errorf("nil StorageClass client, cannot fetch storage class %q", storageClassName)
	}
	sc, err := l.Client.StorageClasses().Get(ctx, storageClassName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("fetching storage class %q: %w", storageClassName, err)
	}
	return sc.Provisioner, nil
}
