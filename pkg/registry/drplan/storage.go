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

package drplan

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// NewREST creates the REST storage for DRPlan.
func NewREST(
	scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter,
) (*genericregistry.Store, *StatusREST, error) {
	store := &genericregistry.Store{
		NewFunc:                   func() runtime.Object { return &soteriav1alpha1.DRPlan{} },
		NewListFunc:               func() runtime.Object { return &soteriav1alpha1.DRPlanList{} },
		DefaultQualifiedResource:  soteriav1alpha1.Resource("drplans"),
		SingularQualifiedResource: soteriav1alpha1.Resource("drplan"),

		CreateStrategy: Strategy,
		UpdateStrategy: Strategy,
		DeleteStrategy: Strategy,
		TableConvertor: rest.NewDefaultTableConvertor(soteriav1alpha1.Resource("drplans")),
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

	return store, &StatusREST{store: &statusStore}, nil
}

// StatusREST implements the REST endpoint for the DRPlan status subresource.
type StatusREST struct {
	store *genericregistry.Store
}

func (r *StatusREST) New() runtime.Object {
	return &soteriav1alpha1.DRPlan{}
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
