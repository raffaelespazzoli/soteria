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

// Package install registers all Soteria API versions with a runtime.Scheme.
package install

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	v1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

var (
	// Scheme contains all registered Soteria API types.
	Scheme = runtime.NewScheme()
	// Codecs provides serializers for the registered Soteria API types.
	Codecs = serializer.NewCodecFactory(Scheme)
	// ParameterCodec encodes query parameters for the Soteria API group.
	ParameterCodec = runtime.NewParameterCodec(Scheme)
)

func init() {
	Install(Scheme)
}

// Install registers all Soteria API versions into the given scheme.
func Install(scheme *runtime.Scheme) {
	_ = v1alpha1.AddToScheme(scheme)
	_ = metav1.AddMetaToScheme(scheme)
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
}
