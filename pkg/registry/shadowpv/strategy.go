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

package shadowpv

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	"github.com/soteria-project/soteria/pkg/drivers"
)

type shadowpvStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	planGetter rest.Getter
}

var Strategy = shadowpvStrategy{
	ObjectTyper:   soteriainstall.Scheme,
	NameGenerator: names.SimpleNameGenerator,
}

// SetPlanStorage injects the DRPlan REST storage so PrepareForCreate can
// resolve the plan UID for setting OwnerReference on new ShadowPVs.
func (s *shadowpvStrategy) SetPlanStorage(g rest.Getter) {
	s.planGetter = g
}

func (shadowpvStrategy) NamespaceScoped() bool { return false }

func (shadowpvStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	spv := obj.(*soteriav1alpha1.ShadowPV)
	spv.Status = soteriav1alpha1.ShadowPVStatus{}
	spv.Generation = 1

	setOwnerReference(ctx, spv)
}

func setOwnerReference(ctx context.Context, spv *soteriav1alpha1.ShadowPV) {
	if Strategy.planGetter == nil {
		return
	}

	planName := spv.Labels[drivers.LabelDRPlan]
	if planName == "" {
		return
	}

	planObj, err := Strategy.planGetter.Get(ctx, planName, &metav1.GetOptions{})
	if err != nil {
		logger := log.FromContext(ctx)
		logger.Error(err, "Could not fetch DRPlan for OwnerReference, proceeding without it",
			"planName", planName)
		return
	}

	plan, ok := planObj.(*soteriav1alpha1.DRPlan)
	if !ok {
		logger := log.FromContext(ctx)
		logger.Error(nil, "Plan storage returned unexpected type, proceeding without OwnerReference",
			"planName", planName, "type", fmt.Sprintf("%T", planObj))
		return
	}

	spv.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         soteriav1alpha1.SchemeGroupVersion.String(),
			Kind:               "DRPlan",
			Name:               plan.Name,
			UID:                plan.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

func (shadowpvStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newSPV := obj.(*soteriav1alpha1.ShadowPV)
	oldSPV := old.(*soteriav1alpha1.ShadowPV)
	newSPV.Status = oldSPV.Status
	newSPV.OwnerReferences = oldSPV.OwnerReferences
}

func (shadowpvStrategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	spv := obj.(*soteriav1alpha1.ShadowPV)
	return soteriav1alpha1.ValidateShadowPV(spv)
}

func (shadowpvStrategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	newSPV := obj.(*soteriav1alpha1.ShadowPV)
	oldSPV := old.(*soteriav1alpha1.ShadowPV)
	return soteriav1alpha1.ValidateShadowPVUpdate(newSPV, oldSPV)
}

func (shadowpvStrategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string    { return nil }
func (shadowpvStrategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string { return nil }
func (shadowpvStrategy) AllowCreateOnUpdate() bool                                        { return false }
func (shadowpvStrategy) AllowUnconditionalUpdate() bool                                   { return false }
func (shadowpvStrategy) Canonicalize(_ runtime.Object)                                    {}

// GetAttrs returns labels and fields of a ShadowPV for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	spv, ok := obj.(*soteriav1alpha1.ShadowPV)
	if !ok {
		return nil, nil, field.Invalid(field.NewPath(""), obj, "expected ShadowPV")
	}
	return spv.Labels, fields.Set{
		"metadata.name": spv.Name,
	}, nil
}

// MatchShadowPV returns a SelectionPredicate for ShadowPV.
func MatchShadowPV(label labels.Selector, fieldSel fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fieldSel,
		GetAttrs: GetAttrs,
	}
}

// ---------- Status subresource strategy ----------

type shadowpvStatusStrategy struct {
	shadowpvStrategy
}

var StatusStrategy = shadowpvStatusStrategy{Strategy}

func (shadowpvStatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newSPV := obj.(*soteriav1alpha1.ShadowPV)
	oldSPV := old.(*soteriav1alpha1.ShadowPV)
	newSPV.Spec = oldSPV.Spec
	newSPV.Labels = oldSPV.Labels
	newSPV.OwnerReferences = oldSPV.OwnerReferences
}

func (shadowpvStatusStrategy) ValidateUpdate(_ context.Context, _, _ runtime.Object) field.ErrorList {
	return field.ErrorList{}
}

// ---------- Custom table convertor ----------

// ShadowPVTableConvertor produces kubectl columns: NAME, PLAN, PV-COUNT, AGE.
type ShadowPVTableConvertor struct{}

var shadowpvTableColumns = []metav1.TableColumnDefinition{
	{Name: "Name", Type: "string", Format: "name"},
	{Name: "Plan", Type: "string"},
	{Name: "PV Count", Type: "integer"},
	{Name: "Age", Type: "string"},
}

func (ShadowPVTableConvertor) ConvertToTable(
	_ context.Context, object runtime.Object, _ runtime.Object,
) (*metav1.Table, error) {
	table := &metav1.Table{ColumnDefinitions: shadowpvTableColumns}

	switch obj := object.(type) {
	case *soteriav1alpha1.ShadowPV:
		table.Rows = append(table.Rows, shadowpvToRow(obj))
	case *soteriav1alpha1.ShadowPVList:
		for i := range obj.Items {
			table.Rows = append(table.Rows, shadowpvToRow(&obj.Items[i]))
		}
	}

	return table, nil
}

func shadowpvToRow(spv *soteriav1alpha1.ShadowPV) metav1.TableRow {
	return metav1.TableRow{
		Object: runtime.RawExtension{Object: spv},
		Cells: []any{
			spv.Name,
			spv.Labels[drivers.LabelDRPlan],
			len(spv.Spec.PVs),
			translateTimestampSince(spv.CreationTimestamp),
		},
	}
}

func translateTimestampSince(timestamp metav1.Time) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}
	return duration.HumanDuration(metav1.Now().Sub(timestamp.Time))
}
