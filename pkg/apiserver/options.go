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

	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
)

// SoteriaServerOptions holds the options for starting the Soteria API server.
type SoteriaServerOptions struct {
	RecommendedOptions *genericoptions.RecommendedOptions

	// ScyllaDB connection options
	ScyllaDBContactPoints string
	ScyllaDBKeyspace      string
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
	fs.StringVar(&o.ScyllaDBTLSCert, "scylladb-tls-cert", "",
		"Path to ScyllaDB TLS client certificate")
	fs.StringVar(&o.ScyllaDBTLSKey, "scylladb-tls-key", "",
		"Path to ScyllaDB TLS client key")
	fs.StringVar(&o.ScyllaDBTLSCA, "scylladb-tls-ca", "",
		"Path to ScyllaDB TLS CA certificate")
}

// Config builds the API server Config from the options.
func (o *SoteriaServerOptions) Config() (*Config, error) {
	serverConfig := genericapiserver.NewRecommendedConfig(soteriainstall.Codecs)

	// Must be set before ApplyTo — Admission options reference it.
	serverConfig.EffectiveVersion = basecompatibility.NewEffectiveVersionFromString(
		"1.35", "", "")

	// OpenAPI must be configured before ApplyTo — Authentication uses it.
	namer := apiopenapi.NewDefinitionNamer(soteriainstall.Scheme)
	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		soteriav1alpha1.GetOpenAPIDefinitions, namer)
	serverConfig.OpenAPIConfig.Info.Title = "Soteria"
	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(
		soteriav1alpha1.GetOpenAPIDefinitions, namer)
	serverConfig.OpenAPIV3Config.Info.Title = "Soteria"

	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, fmt.Errorf("applying recommended options: %w", err)
	}

	config := &Config{
		GenericConfig: serverConfig,
	}
	return config, nil
}
