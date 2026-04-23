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

package drexecution

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// NewREST creates the REST storage for DRExecution.
func NewREST(
	scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter,
) (*AuditProtectedREST, *StatusREST, error) {
	tc := DRExecutionTableConvertor{}
	store := &genericregistry.Store{
		NewFunc:                   func() runtime.Object { return &soteriav1alpha1.DRExecution{} },
		NewListFunc:               func() runtime.Object { return &soteriav1alpha1.DRExecutionList{} },
		DefaultQualifiedResource:  soteriav1alpha1.Resource("drexecutions"),
		SingularQualifiedResource: soteriav1alpha1.Resource("drexecution"),

		CreateStrategy: Strategy,
		UpdateStrategy: Strategy,
		DeleteStrategy: Strategy,
		TableConvertor: tc,
	}

	options := &generic.StoreOptions{
		RESTOptions: optsGetter,
		AttrFunc:    GetAttrs,
	}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, nil, err
	}

	statusStore := *store
	statusStore.UpdateStrategy = StatusStrategy

	return &AuditProtectedREST{Store: store}, &StatusREST{store: &statusStore}, nil
}

// AuditProtectedREST wraps the generic store to block deletion of completed
// DRExecution audit records. Completed executions (Succeeded, Failed, or
// PartiallySucceeded) cannot be deleted — they serve as compliance evidence
// (FR41). In-progress executions (Result == "") can be deleted for cleanup.
type AuditProtectedREST struct {
	*genericregistry.Store
}

func (r *AuditProtectedREST) Delete(
	ctx context.Context, name string,
	deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions,
) (runtime.Object, bool, error) {
	wrappedValidation := wrapAuditValidation(deleteValidation)
	return r.Store.Delete(ctx, name, wrappedValidation, options)
}

func (r *AuditProtectedREST) DeleteCollection(
	ctx context.Context, deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions, listOptions *metainternalversion.ListOptions,
) (runtime.Object, error) {
	wrappedValidation := wrapAuditValidation(deleteValidation)
	return r.Store.DeleteCollection(ctx, wrappedValidation, options, listOptions)
}

// wrapAuditValidation chains the caller-supplied deleteValidation (may be nil)
// with the audit-record guard. The resulting function runs inside the store's
// delete transaction, eliminating the TOCTOU window that would exist with a
// separate pre-check Get.
func wrapAuditValidation(deleteValidation rest.ValidateObjectFunc) rest.ValidateObjectFunc {
	return rest.ValidateObjectFunc(func(ctx context.Context, obj runtime.Object) error {
		if deleteValidation != nil {
			if err := deleteValidation(ctx, obj); err != nil {
				return err
			}
		}
		return validateAuditDelete(obj)
	})
}

func validateAuditDelete(obj runtime.Object) error {
	exec, ok := obj.(*soteriav1alpha1.DRExecution)
	if !ok {
		return nil
	}
	if exec.Status.Result != "" {
		return apierrors.NewForbidden(
			soteriav1alpha1.Resource("drexecutions"), exec.Name,
			fmt.Errorf("completed DRExecution audit records cannot be deleted (FR41)"))
	}
	return nil
}

// StatusREST implements the REST endpoint for the DRExecution status subresource.
type StatusREST struct {
	store *genericregistry.Store
}

func (r *StatusREST) New() runtime.Object {
	return &soteriav1alpha1.DRExecution{}
}

func (r *StatusREST) Destroy() {}

func (r *StatusREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, options)
}

func (r *StatusREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	return r.store.Update(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate, options)
}

func (r *StatusREST) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	return r.store.GetResetFields()
}

func (r *StatusREST) ConvertToTable(
	ctx context.Context, object runtime.Object, tableOptions runtime.Object,
) (*metav1.Table, error) {
	return r.store.ConvertToTable(ctx, object, tableOptions)
}

// ---------- Custom table convertor ----------

// DRExecutionTableConvertor produces rich kubectl columns:
// NAME, PLAN, MODE, RESULT, DURATION, AGE.
type DRExecutionTableConvertor struct{}

var execTableColumns = []metav1.TableColumnDefinition{
	{Name: "Name", Type: "string", Format: "name"},
	{Name: "Plan", Type: "string"},
	{Name: "Mode", Type: "string"},
	{Name: "Result", Type: "string"},
	{Name: "Duration", Type: "string"},
	{Name: "Age", Type: "string"},
}

func (DRExecutionTableConvertor) ConvertToTable(
	_ context.Context, object runtime.Object, _ runtime.Object,
) (*metav1.Table, error) {
	table := &metav1.Table{ColumnDefinitions: execTableColumns}

	switch obj := object.(type) {
	case *soteriav1alpha1.DRExecution:
		table.Rows = append(table.Rows, execToRow(obj))
	case *soteriav1alpha1.DRExecutionList:
		for i := range obj.Items {
			table.Rows = append(table.Rows, execToRow(&obj.Items[i]))
		}
	}

	return table, nil
}

func execToRow(exec *soteriav1alpha1.DRExecution) metav1.TableRow {
	return metav1.TableRow{
		Object: runtime.RawExtension{Object: exec},
		Cells: []any{
			exec.Name,
			exec.Spec.PlanName,
			string(exec.Spec.Mode),
			string(exec.Status.Result),
			execDuration(exec),
			translateTimestampSince(exec.CreationTimestamp),
		},
	}
}

func execDuration(exec *soteriav1alpha1.DRExecution) string {
	if exec.Status.StartTime == nil || exec.Status.CompletionTime == nil {
		return ""
	}
	d := exec.Status.CompletionTime.Sub(exec.Status.StartTime.Time)
	return duration.HumanDuration(d)
}

const unknownTimestamp = "<unknown>"

func translateTimestampSince(timestamp metav1.Time) string {
	if timestamp.IsZero() {
		return unknownTimestamp
	}
	return duration.HumanDuration(metav1.Now().Sub(timestamp.Time))
}
