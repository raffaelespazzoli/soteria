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
	"strings"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/soteria-project/soteria/pkg/drivers"
)

// DriverName is the plan-level name used in DRPlanSpec.VolumeReplicationDriver.
const DriverName = "csi-extension"

const (
	vgIDPrefix = "csi-ext-"
	nsPrefix   = "ns-"
)

var _ drivers.StorageProvider = (*Driver)(nil)

// Driver is a StorageProvider that manages volume replication through CSI
// Addons VolumeReplication and VolumeGroupReplication CRDs. It holds a
// controller-runtime client for creating, reading, updating, and deleting
// VR/VGR resources in the cluster.
type Driver struct {
	client client.Client
}

// New creates a new csi-extension Driver with the given Kubernetes client.
// The client must have the CSI Addons replication types registered in its
// scheme (see replicationv1alpha1.AddToScheme).
func New(c client.Client) *Driver {
	return &Driver{client: c}
}

func vgIDFromNamespace(namespace, name string) drivers.VolumeGroupID {
	return drivers.VolumeGroupID(vgIDPrefix + namespace + "/" + name)
}

// parseVGID extracts namespace and VG name from a driver-assigned VolumeGroupID.
// Falls back to empty namespace if the ID lacks the separator (legacy/malformed).
func parseVGID(id drivers.VolumeGroupID) (namespace, name string) {
	s := strings.TrimPrefix(string(id), vgIDPrefix)
	if ns, n, ok := strings.Cut(s, "/"); ok {
		return ns, n
	}
	return "", s
}

// isMultiVM returns true when the volume group name indicates a
// namespace-level (multi-VM) group that requires a VolumeGroupReplication
// CR instead of individual VolumeReplication CRs.
func isMultiVM(vgName string) bool {
	return strings.HasPrefix(vgName, nsPrefix)
}

func replicationStateFromLabels(lbls map[string]string) replicationv1alpha1.ReplicationState {
	if lbls[SiteRoleLabel] == SiteRoleSecondary {
		return ReplicationStateSecondary
	}
	return ReplicationStatePrimary
}

func finalizerForSiteRole(lbls map[string]string) string {
	identity := lbls[SiteIdentityLabel]
	if identity == "" {
		identity = lbls[SiteRoleLabel]
	}
	if identity == SiteRoleSecondary {
		return FinalizerSiteSecondary
	}
	return FinalizerSitePrimary
}

// removeSiteFinalizers strips both site-specific finalizers from obj.
// Returns true if any finalizer was actually removed.
// Used by DeleteVolumeGroup for complete single-site teardown (tests,
// conformance, engine workflows). The DRPlan deletion path uses
// cleanupVolumeReplicationFinalizers instead, which removes only the
// local site's finalizer and waits for the peer to remove its own.
func removeSiteFinalizers(obj client.Object) bool {
	a := controllerutil.RemoveFinalizer(obj, FinalizerSitePrimary)
	b := controllerutil.RemoveFinalizer(obj, FinalizerSiteSecondary)
	return a || b
}

func vgLabels(vgName string, specLabels map[string]string) map[string]string {
	m := map[string]string{
		LabelVolumeGroup: vgName,
	}
	if planName := specLabels[LabelDRPlan]; planName != "" {
		m[LabelDRPlan] = planName
	}
	return m
}

func vgLabelSelector(vgName string) client.ListOption {
	return client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(labels.Set{LabelVolumeGroup: vgName}),
	}
}

func (d *Driver) CreateVolumeGroup(ctx context.Context, spec drivers.VolumeGroupSpec) (drivers.VolumeGroupInfo, error) {
	if err := ctx.Err(); err != nil {
		return drivers.VolumeGroupInfo{}, err
	}
	if len(spec.PVCNames) == 0 {
		return drivers.VolumeGroupInfo{}, fmt.Errorf("csi-extension: CreateVolumeGroup requires at least one PVC")
	}

	state := replicationStateFromLabels(spec.Labels)

	if isMultiVM(spec.Name) {
		return d.createVGR(ctx, spec, state)
	}
	return d.createVRs(ctx, spec, state)
}

func (d *Driver) createVRs(
	ctx context.Context, spec drivers.VolumeGroupSpec,
	state replicationv1alpha1.ReplicationState,
) (drivers.VolumeGroupInfo, error) {
	logger := log.FromContext(ctx)
	vrClass := spec.Labels[VolumeReplicationClassLabel]
	crLabels := vgLabels(spec.Name, spec.Labels)

	for _, pvcName := range spec.PVCNames {
		vr := &replicationv1alpha1.VolumeReplication{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vgIDPrefix + spec.Name + "-" + pvcName,
				Namespace: spec.Namespace,
			},
		}
		siteFinalizer := finalizerForSiteRole(spec.Labels)
		_, err := controllerutil.CreateOrUpdate(ctx, d.client, vr, func() error {
			vr.Labels = crLabels
			vr.Spec.ReplicationState = state
			controllerutil.AddFinalizer(vr, siteFinalizer)
			if vr.CreationTimestamp.IsZero() {
				vr.Spec.VolumeReplicationClass = vrClass
				vr.Spec.DataSource = corev1.TypedLocalObjectReference{
					Kind: "PersistentVolumeClaim",
					Name: pvcName,
				}
			}
			return nil
		})
		if err != nil {
			return drivers.VolumeGroupInfo{}, fmt.Errorf("creating or updating VolumeReplication for PVC %s: %w", pvcName, err)
		}
	}

	logger.V(1).Info("Created/updated VolumeReplication CRs", "volumeGroup", spec.Name, "pvcCount", len(spec.PVCNames))
	return drivers.VolumeGroupInfo{
		ID:       vgIDFromNamespace(spec.Namespace, spec.Name),
		Name:     spec.Name,
		PVCNames: append([]string(nil), spec.PVCNames...),
	}, nil
}

func (d *Driver) createVGR(
	ctx context.Context, spec drivers.VolumeGroupSpec,
	state replicationv1alpha1.ReplicationState,
) (drivers.VolumeGroupInfo, error) {
	logger := log.FromContext(ctx)
	vgrClass := spec.Labels[VolumeGroupReplicationClassLabel]
	vrClass := spec.Labels[VolumeReplicationClassLabel]
	crLabels := vgLabels(spec.Name, spec.Labels)

	// Label PVCs so the VGR source selector can match them.
	for _, pvcName := range spec.PVCNames {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := d.client.Get(ctx, client.ObjectKey{Namespace: spec.Namespace, Name: pvcName}, pvc); err != nil {
			return drivers.VolumeGroupInfo{}, fmt.Errorf("getting PVC %s/%s: %w", spec.Namespace, pvcName, err)
		}
		if pvc.Labels[LabelVolumeGroup] != spec.Name {
			patch := client.MergeFrom(pvc.DeepCopy())
			if pvc.Labels == nil {
				pvc.Labels = make(map[string]string)
			}
			pvc.Labels[LabelVolumeGroup] = spec.Name
			if err := d.client.Patch(ctx, pvc, patch); err != nil {
				return drivers.VolumeGroupInfo{}, fmt.Errorf("labeling PVC %s/%s: %w", spec.Namespace, pvcName, err)
			}
		}
	}

	vgr := &replicationv1alpha1.VolumeGroupReplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vgIDPrefix + spec.Name,
			Namespace: spec.Namespace,
		},
	}
	siteFinalizer := finalizerForSiteRole(spec.Labels)
	_, err := controllerutil.CreateOrUpdate(ctx, d.client, vgr, func() error {
		vgr.Labels = crLabels
		vgr.Spec.ReplicationState = state
		controllerutil.AddFinalizer(vgr, siteFinalizer)
		if vgr.CreationTimestamp.IsZero() {
			vgr.Spec.VolumeGroupReplicationClassName = vgrClass
			vgr.Spec.VolumeReplicationClassName = vrClass
			vgr.Spec.Source = replicationv1alpha1.VolumeGroupReplicationSource{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						LabelVolumeGroup: spec.Name,
					},
				},
			}
		}
		return nil
	})
	if err != nil {
		return drivers.VolumeGroupInfo{}, fmt.Errorf("creating or updating VolumeGroupReplication for %s: %w", spec.Name, err)
	}

	logger.V(1).Info("Created/updated VolumeGroupReplication CR", "volumeGroup", spec.Name, "pvcCount", len(spec.PVCNames))
	return drivers.VolumeGroupInfo{
		ID:       vgIDFromNamespace(spec.Namespace, spec.Name),
		Name:     spec.Name,
		PVCNames: append([]string(nil), spec.PVCNames...),
	}, nil
}

func (d *Driver) DeleteVolumeGroup(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	logger := log.FromContext(ctx)
	namespace, vgName := parseVGID(id)
	opts := []client.ListOption{vgLabelSelector(vgName)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := d.client.List(ctx, &vrList, opts...); err != nil {
		return fmt.Errorf("listing VolumeReplication CRs for %s: %w", vgName, err)
	}
	for i := range vrList.Items {
		vr := &vrList.Items[i]
		if removeSiteFinalizers(vr) {
			if err := d.client.Update(ctx, vr); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("removing finalizers from VolumeReplication %s: %w", vr.Name, err)
			}
		}
		if err := d.client.Delete(ctx, vr); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting VolumeReplication %s: %w", vr.Name, err)
		}
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := d.client.List(ctx, &vgrList, opts...); err != nil {
		return fmt.Errorf("listing VolumeGroupReplication CRs for %s: %w", vgName, err)
	}
	for i := range vgrList.Items {
		vgr := &vgrList.Items[i]
		if removeSiteFinalizers(vgr) {
			if err := d.client.Update(ctx, vgr); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("removing finalizers from VolumeGroupReplication %s: %w", vgr.Name, err)
			}
		}
		if err := d.client.Delete(ctx, vgr); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting VolumeGroupReplication %s: %w", vgr.Name, err)
		}
	}

	logger.V(1).Info("Deleted volume group CRs", "volumeGroup", vgName,
		"namespace", namespace, "vrCount", len(vrList.Items), "vgrCount", len(vgrList.Items))
	return nil
}

func (d *Driver) GetVolumeGroup(ctx context.Context, id drivers.VolumeGroupID) (drivers.VolumeGroupInfo, error) {
	if err := ctx.Err(); err != nil {
		return drivers.VolumeGroupInfo{}, err
	}

	namespace, vgName := parseVGID(id)
	info, err := d.getByName(ctx, vgName, namespace)
	if err != nil {
		return drivers.VolumeGroupInfo{}, err
	}
	if info == nil {
		return drivers.VolumeGroupInfo{}, drivers.ErrVolumeGroupNotFound
	}
	return *info, nil
}

// getByName looks up VR/VGR CRs by the volume-group label. When namespace
// is empty it searches across all namespaces. Returns nil if no CRs exist.
func (d *Driver) getByName(ctx context.Context, vgName, namespace string) (*drivers.VolumeGroupInfo, error) {
	opts := []client.ListOption{vgLabelSelector(vgName)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	var vgrList replicationv1alpha1.VolumeGroupReplicationList
	if err := d.client.List(ctx, &vgrList, opts...); err != nil {
		return nil, fmt.Errorf("listing VolumeGroupReplication CRs: %w", err)
	}
	if len(vgrList.Items) > 0 {
		vgr := &vgrList.Items[0]
		pvcNames, err := d.pvcNamesFromSelector(ctx, vgr.Namespace, vgr.Spec.Source.Selector)
		if err != nil {
			return nil, fmt.Errorf("resolving PVC names for VolumeGroupReplication %s: %w", vgr.Name, err)
		}
		return &drivers.VolumeGroupInfo{
			ID:       vgIDFromNamespace(namespace, vgName),
			Name:     vgName,
			PVCNames: pvcNames,
		}, nil
	}

	var vrList replicationv1alpha1.VolumeReplicationList
	if err := d.client.List(ctx, &vrList, opts...); err != nil {
		return nil, fmt.Errorf("listing VolumeReplication CRs: %w", err)
	}
	if len(vrList.Items) > 0 {
		pvcNames := make([]string, 0, len(vrList.Items))
		for _, vr := range vrList.Items {
			pvcNames = append(pvcNames, vr.Spec.DataSource.Name)
		}
		return &drivers.VolumeGroupInfo{
			ID:       vgIDFromNamespace(namespace, vgName),
			Name:     vgName,
			PVCNames: pvcNames,
		}, nil
	}

	return nil, nil
}

func (d *Driver) pvcNamesFromSelector(
	ctx context.Context, namespace string, sel *metav1.LabelSelector,
) ([]string, error) {
	if sel == nil {
		return nil, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return nil, fmt.Errorf("parsing label selector: %w", err)
	}
	var pvcList corev1.PersistentVolumeClaimList
	if err := d.client.List(ctx, &pvcList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return nil, fmt.Errorf("listing PVCs: %w", err)
	}
	names := make([]string, len(pvcList.Items))
	for i, pvc := range pvcList.Items {
		names[i] = pvc.Name
	}
	return names, nil
}

func (d *Driver) SetSource(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	set, err := d.listCRsForVG(ctx, id)
	if err != nil {
		return err
	}
	return d.updateReplicationState(ctx, set, ReplicationStatePrimary)
}

func (d *Driver) StopReplication(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	set, err := d.listCRsForVG(ctx, id)
	if err != nil {
		return err
	}
	return d.updateReplicationState(ctx, set, ReplicationStateSecondary)
}

func (d *Driver) ResyncVolume(ctx context.Context, id drivers.VolumeGroupID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	set, err := d.listCRsForVG(ctx, id)
	if err != nil {
		return err
	}
	return d.updateReplicationState(ctx, set, ReplicationStateResync)
}

func (d *Driver) GetReplicationStatus(
	ctx context.Context, id drivers.VolumeGroupID,
) (drivers.ReplicationStatus, error) {
	if err := ctx.Err(); err != nil {
		return drivers.ReplicationStatus{}, err
	}

	namespace, vgName := parseVGID(id)
	opts := []client.ListOption{vgLabelSelector(vgName)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	if isMultiVM(vgName) {
		var list replicationv1alpha1.VolumeGroupReplicationList
		if err := d.client.List(ctx, &list, opts...); err != nil {
			return drivers.ReplicationStatus{}, fmt.Errorf("listing VolumeGroupReplication CRs for %s: %w", vgName, err)
		}
		if len(list.Items) == 0 {
			return drivers.ReplicationStatus{}, drivers.ErrVolumeGroupNotFound
		}
		return statusFromVGR(&list.Items[0]), nil
	}

	var list replicationv1alpha1.VolumeReplicationList
	if err := d.client.List(ctx, &list, opts...); err != nil {
		return drivers.ReplicationStatus{}, fmt.Errorf("listing VolumeReplication CRs for %s: %w", vgName, err)
	}
	if len(list.Items) == 0 {
		return drivers.ReplicationStatus{}, drivers.ErrVolumeGroupNotFound
	}
	return aggregateVRStatus(list.Items), nil
}
