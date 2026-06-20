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

package csiextension

import (
	"context"
	"fmt"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/soteria-project/soteria/pkg/drivers"
)

// crSet holds the VolumeReplication and/or VolumeGroupReplication CRs
// found for a volume group. Exactly one slice will be non-empty, matching
// the rendering rule: single-VM groups use VR CRs, multi-VM groups use
// a single VGR CR.
type crSet struct {
	vrs  []replicationv1alpha1.VolumeReplication
	vgrs []replicationv1alpha1.VolumeGroupReplication
}

// currentState returns the replication state of the first CR in the set.
// VGR CRs take precedence. Returns empty string if the set is empty.
func (s crSet) currentState() replicationv1alpha1.ReplicationState {
	if len(s.vgrs) > 0 {
		return s.vgrs[0].Spec.ReplicationState
	}
	if len(s.vrs) > 0 {
		return s.vrs[0].Spec.ReplicationState
	}
	return ""
}

// listCRsForVG locates the VR or VGR CRs belonging to a volume group
// by querying for the soteria.io/volume-group label. Returns
// ErrVolumeGroupNotFound when no matching CRs exist.
func (d *Driver) listCRsForVG(ctx context.Context, id drivers.VolumeGroupID) (crSet, error) {
	namespace, vgName := parseVGID(id)
	opts := []client.ListOption{vgLabelSelector(vgName)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	var set crSet

	if isMultiVM(vgName) {
		var list replicationv1alpha1.VolumeGroupReplicationList
		if err := d.client.List(ctx, &list, opts...); err != nil {
			return crSet{}, fmt.Errorf("listing VolumeGroupReplication CRs for %s: %w", vgName, err)
		}
		set.vgrs = list.Items
	} else {
		var list replicationv1alpha1.VolumeReplicationList
		if err := d.client.List(ctx, &list, opts...); err != nil {
			return crSet{}, fmt.Errorf("listing VolumeReplication CRs for %s: %w", vgName, err)
		}
		set.vrs = list.Items
	}

	if len(set.vrs) == 0 && len(set.vgrs) == 0 {
		return crSet{}, drivers.ErrVolumeGroupNotFound
	}
	return set, nil
}

// updateReplicationState sets all CRs in the set to the target state,
// skipping any CR that is already in that state (idempotent). Each CR
// is updated with retry.RetryOnConflict to handle concurrent status
// updates from the noop controller (which bumps resourceVersion via
// Status().Update, causing 409 Conflict on our spec Update).
func (d *Driver) updateReplicationState(
	ctx context.Context, set crSet, target replicationv1alpha1.ReplicationState,
) error {
	logger := log.FromContext(ctx)

	for i := range set.vrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if set.vrs[i].Spec.ReplicationState == target {
			continue
		}
		name := set.vrs[i].Name
		key := client.ObjectKeyFromObject(&set.vrs[i])
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var fresh replicationv1alpha1.VolumeReplication
			if err := d.client.Get(ctx, key, &fresh); err != nil {
				return err
			}
			if fresh.Spec.ReplicationState == target {
				return nil
			}
			fresh.Spec.ReplicationState = target
			return d.client.Update(ctx, &fresh)
		}); err != nil {
			return fmt.Errorf("updating VolumeReplication %s replication state: %w", name, err)
		}
		logger.V(1).Info("Updated VolumeReplication replication state", "name", name, "state", target)
	}

	for i := range set.vgrs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if set.vgrs[i].Spec.ReplicationState == target {
			continue
		}
		name := set.vgrs[i].Name
		key := client.ObjectKeyFromObject(&set.vgrs[i])
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var fresh replicationv1alpha1.VolumeGroupReplication
			if err := d.client.Get(ctx, key, &fresh); err != nil {
				return err
			}
			if fresh.Spec.ReplicationState == target {
				return nil
			}
			fresh.Spec.ReplicationState = target
			return d.client.Update(ctx, &fresh)
		}); err != nil {
			return fmt.Errorf("updating VolumeGroupReplication %s replication state: %w", name, err)
		}
		logger.V(1).Info("Updated VolumeGroupReplication replication state", "name", name, "state", target)
	}

	return nil
}
