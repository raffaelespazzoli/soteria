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

package apiserver

import (
	"fmt"

	"github.com/spf13/pflag"
	apiopenapi "k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	basecompatibility "k8s.io/component-base/compatibility"
	"k8s.io/kube-openapi/pkg/util"
	"k8s.io/kube-openapi/pkg/validation/spec"

	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// SoteriaServerOptions holds the options for starting the Soteria API server.
type SoteriaServerOptions struct {
	RecommendedOptions *genericoptions.RecommendedOptions

	// ScyllaDB connection options
	ScyllaDBContactPoints string
	ScyllaDBKeyspace      string
	ScyllaDBLocalDC       string
	ScyllaDBTLSCert       string
	ScyllaDBTLSKey        string
	ScyllaDBTLSCA         string
}

// NewSoteriaServerOptions creates default options.
func NewSoteriaServerOptions() *SoteriaServerOptions {
	o := &SoteriaServerOptions{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			"",
			soteriainstall.Codecs.LegacyCodec(soteriav1alpha1.SchemeGroupVersion),
		),
	}
	// Soteria uses ScyllaDB, not etcd
	o.RecommendedOptions.Etcd = nil
	return o
}

// AddFlags registers command-line flags for the server options.
func (o *SoteriaServerOptions) AddFlags(fs *pflag.FlagSet) {
	o.RecommendedOptions.AddFlags(fs)
	fs.StringVar(&o.ScyllaDBContactPoints, "scylladb-contact-points", "localhost:9042",
		"Comma-separated ScyllaDB contact points")
	fs.StringVar(&o.ScyllaDBKeyspace, "scylladb-keyspace", "soteria",
		"ScyllaDB keyspace name")
	fs.StringVar(&o.ScyllaDBLocalDC, "scylladb-local-dc", "",
		"Local ScyllaDB datacenter name for DC-aware routing (e.g. 'dc1')")
	fs.StringVar(&o.ScyllaDBTLSCert, "scylladb-tls-cert", "",
		"Path to ScyllaDB TLS client certificate")
	fs.StringVar(&o.ScyllaDBTLSKey, "scylladb-tls-key", "",
		"Path to ScyllaDB TLS client key")
	fs.StringVar(&o.ScyllaDBTLSCA, "scylladb-tls-ca", "",
		"Path to ScyllaDB TLS CA certificate")
}

func gvk(group, version, kind string) []any {
	return []any{
		map[string]any{
			"group":   group,
			"version": version,
			"kind":    kind,
		},
	}
}

// soteriaGVKExtensions maps raw Go definition names of root API types to their
// x-kubernetes-group-version-kind extensions. Sub-types (Spec, Status, etc.)
// don't need GVK extensions — only runtime.Object root types do.
var soteriaGVKExtensions = map[string][]any{
	"github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1.DRPlan":            gvk("soteria.io", "v1alpha1", "DRPlan"),
	"github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1.DRPlanList":        gvk("soteria.io", "v1alpha1", "DRPlanList"),
	"github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1.DRExecution":       gvk("soteria.io", "v1alpha1", "DRExecution"),
	"github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1.DRExecutionList":   gvk("soteria.io", "v1alpha1", "DRExecutionList"),
	"github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1.DRGroupStatus":     gvk("soteria.io", "v1alpha1", "DRGroupStatus"),
	"github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1.DRGroupStatusList": gvk("soteria.io", "v1alpha1", "DRGroupStatusList"),
}

// soteriaGetDefinitionName converts all definition names from raw Go package
// paths to REST-friendly names (dots instead of slashes) and injects
// x-kubernetes-group-version-kind for root API types.
//
// This is needed because:
// 1. openapi-gen uses raw Go paths as keys (e.g., github.com/soteria-project/...)
// 2. Scheme.ToOpenAPIDefinitionName produces REST-friendly names (com.github.soteria-project...)
// 3. The standard DefinitionNamer can't match (2) against (1), silently dropping GVK extensions
// 4. Raw Go paths with '/' break structured-merge-diff JSON Pointer resolution
func soteriaGetDefinitionName(name string) (string, spec.Extensions) {
	friendly := util.ToRESTFriendlyName(name)
	if gvk, ok := soteriaGVKExtensions[name]; ok {
		return friendly, spec.Extensions{
			"x-kubernetes-group-version-kind": gvk,
		}
	}
	return friendly, nil
}

// Config builds the API server Config from the options.
func (o *SoteriaServerOptions) Config() (*Config, error) {
	serverConfig := genericapiserver.NewRecommendedConfig(soteriainstall.Codecs)

	// Must be set before ApplyTo — Admission options reference it.
	serverConfig.EffectiveVersion = basecompatibility.NewEffectiveVersionFromString(
		"1.35", "", "")

	// OpenAPI must be configured before ApplyTo — Authentication uses it.
	// DefaultOpenAPIV3Config pre-builds definitions using the namer, but the
	// standard DefinitionNamer can't resolve raw Go paths to GVK extensions.
	// We override GetDefinitionName and nil the pre-built Definitions so the
	// builder rebuilds them with correct REST-friendly names and $ref links.
	namer := apiopenapi.NewDefinitionNamer(soteriainstall.Scheme)

	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		soteriav1alpha1.GetOpenAPIDefinitions, namer)
	serverConfig.OpenAPIConfig.Info.Title = "Soteria"
	serverConfig.OpenAPIConfig.GetDefinitionName = soteriaGetDefinitionName

	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(
		soteriav1alpha1.GetOpenAPIDefinitions, namer)
	serverConfig.OpenAPIV3Config.Info.Title = "Soteria"
	serverConfig.OpenAPIV3Config.GetDefinitionName = soteriaGetDefinitionName
	serverConfig.OpenAPIV3Config.Definitions = nil

	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, fmt.Errorf("applying recommended options: %w", err)
	}

	config := &Config{
		GenericConfig: serverConfig,
	}
	return config, nil
}
