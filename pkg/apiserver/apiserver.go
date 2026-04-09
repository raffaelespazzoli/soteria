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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/apiserver/pkg/storage/storagebackend/factory"
	"k8s.io/client-go/tools/cache"

	soteriainstall "github.com/soteria-project/soteria/pkg/apis/soteria.io/install"
	soteriav1alpha1 "github.com/soteria-project/soteria/pkg/apis/soteria.io/v1alpha1"
	scylladb "github.com/soteria-project/soteria/pkg/storage/scylladb"

	drexecutionregistry "github.com/soteria-project/soteria/pkg/registry/drexecution"
	drgroupstatusregistry "github.com/soteria-project/soteria/pkg/registry/drgroupstatus"
	drplanregistry "github.com/soteria-project/soteria/pkg/registry/drplan"

	cacherstorage "k8s.io/apiserver/pkg/storage/cacher"
)

// Config holds the configuration for the Soteria extension API server.
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig
	// ScyllaStoreFactory creates per-resource ScyllaDB storage.Interface instances.
	ScyllaStoreFactory *ScyllaStoreFactory
}

// completedConfig is the internal completed configuration.
type completedConfig struct {
	GenericConfig      genericapiserver.CompletedConfig
	ScyllaStoreFactory *ScyllaStoreFactory
}

// CompletedConfig is the result of calling Config.Complete().
type CompletedConfig struct {
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data.
func (cfg *Config) Complete() CompletedConfig {
	c := completedConfig{
		GenericConfig:      cfg.GenericConfig.Complete(),
		ScyllaStoreFactory: cfg.ScyllaStoreFactory,
	}
	return CompletedConfig{&c}
}

// SoteriaServer wraps the generic API server with Soteria-specific configuration.
type SoteriaServer struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

// New creates a new SoteriaServer and installs the soteria.io API group.
func (c CompletedConfig) New() (*SoteriaServer, error) {
	genericServer, err := c.GenericConfig.New("soteria-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, fmt.Errorf("creating generic API server: %w", err)
	}

	s := &SoteriaServer{
		GenericAPIServer: genericServer,
	}

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(
		soteriav1alpha1.GroupName,
		soteriainstall.Scheme,
		soteriainstall.ParameterCodec,
		soteriainstall.Codecs,
	)

	optsGetter := c.ScyllaStoreFactory.RESTOptionsGetter()

	v1alpha1storage := map[string]rest.Storage{}

	drplanStore, drplanStatusStore, err := drplanregistry.NewREST(soteriainstall.Scheme, optsGetter)
	if err != nil {
		return nil, fmt.Errorf("creating DRPlan storage: %w", err)
	}
	v1alpha1storage["drplans"] = drplanStore
	v1alpha1storage["drplans/status"] = drplanStatusStore

	drexecStore, drexecStatusStore, err := drexecutionregistry.NewREST(soteriainstall.Scheme, optsGetter)
	if err != nil {
		return nil, fmt.Errorf("creating DRExecution storage: %w", err)
	}
	v1alpha1storage["drexecutions"] = drexecStore
	v1alpha1storage["drexecutions/status"] = drexecStatusStore

	drgroupStore, drgroupStatusStore, err := drgroupstatusregistry.NewREST(soteriainstall.Scheme, optsGetter)
	if err != nil {
		return nil, fmt.Errorf("creating DRGroupStatus storage: %w", err)
	}
	v1alpha1storage["drgroupstatuses"] = drgroupStore
	v1alpha1storage["drgroupstatuses/status"] = drgroupStatusStore

	apiGroupInfo.VersionedResourcesStorageMap["v1alpha1"] = v1alpha1storage

	if err := s.GenericAPIServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, fmt.Errorf("installing API group: %w", err)
	}

	return s, nil
}

// ScyllaStoreFactory creates per-resource ScyllaDB-backed storage.Interface instances
// wrapped with the k8s.io/apiserver cacher layer.
type ScyllaStoreFactory struct {
	StoreConfig scylladb.StoreConfig
	Codec       runtime.Codec
	// UseCacher controls whether storage is wrapped with the apiserver cacher.
	// Set to false for integration tests that don't need caching.
	UseCacher bool
	// CriticalFieldDetectors maps resource types to detectors that identify
	// state-machine field changes requiring cross-DC LWT (Serial consistency).
	CriticalFieldDetectors map[schema.GroupResource]scylladb.CriticalFieldDetector
}

// RESTOptionsGetter returns a generic.RESTOptionsGetter backed by ScyllaDB.
func (f *ScyllaStoreFactory) RESTOptionsGetter() generic.RESTOptionsGetter {
	return &soteriaRESTOptionsGetter{
		factory: f,
	}
}

// soteriaRESTOptionsGetter implements generic.RESTOptionsGetter to bridge
// the k8s.io/apiserver generic registry with ScyllaDB storage. The standard
// Kubernetes path uses etcd via StorageFactory; here we replace that entirely.
// The Decorator function returned by GetRESTOptions creates a ScyllaDB Store
// for the requested GroupResource and optionally wraps it with the apiserver
// cacher (NewCacherFromConfig + CacheDelegator). The cacher provides in-memory
// watch fan-out so that many client watches share a single CDC reader.
type soteriaRESTOptionsGetter struct {
	factory *ScyllaStoreFactory
}

func (g *soteriaRESTOptionsGetter) GetRESTOptions(resource schema.GroupResource, _ runtime.Object) (generic.RESTOptions, error) {
	cfg := g.factory.StoreConfig
	cfg.GroupResource = resource
	cfg.ResourcePrefix = "/" + soteriav1alpha1.GroupName + "/" + resource.Resource
	if detector, ok := g.factory.CriticalFieldDetectors[resource]; ok {
		cfg.CriticalFieldDetector = detector
	}

	decoratorFn := g.decoratorFor(cfg)

	return generic.RESTOptions{
		StorageConfig: &storagebackend.ConfigForResource{
			Config: storagebackend.Config{
				Codec: g.factory.Codec,
			},
			GroupResource: resource,
		},
		Decorator:               decoratorFn,
		DeleteCollectionWorkers: 1,
		EnableGarbageCollection: false,
		ResourcePrefix:          cfg.ResourcePrefix,
	}, nil
}

func (g *soteriaRESTOptionsGetter) decoratorFor(cfg scylladb.StoreConfig) generic.StorageDecorator {
	return func(
		_ *storagebackend.ConfigForResource,
		resourcePrefix string,
		keyFunc func(obj runtime.Object) (string, error),
		newFunc func() runtime.Object,
		newListFunc func() runtime.Object,
		getAttrsFunc storage.AttrFunc,
		trigger storage.IndexerFuncs,
		indexers *cache.Indexers,
	) (storage.Interface, factory.DestroyFunc, error) {
		cfg.ResourcePrefix = resourcePrefix
		cfg.NewFunc = newFunc
		cfg.NewListFunc = newListFunc
		scyllaStore := scylladb.NewStore(cfg)

		if !g.factory.UseCacher {
			return scyllaStore, func() {}, nil
		}

		cacherConfig := cacherstorage.Config{
			Storage:             scyllaStore,
			Versioner:           scyllaStore.Versioner(),
			GroupResource:       cfg.GroupResource,
			ResourcePrefix:      resourcePrefix,
			KeyFunc:             keyFunc,
			GetAttrsFunc:        getAttrsFunc,
			IndexerFuncs:        trigger,
			Indexers:            indexers,
			NewFunc:             newFunc,
			NewListFunc:         newListFunc,
			Codec:               g.factory.Codec,
			EventsHistoryWindow: 2 * time.Minute,
		}
		cacher, err := cacherstorage.NewCacherFromConfig(cacherConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("creating cacher: %w", err)
		}
		delegator := cacherstorage.NewCacheDelegator(cacher, scyllaStore)
		destroyFunc := func() { delegator.Stop() }
		return delegator, destroyFunc, nil
	}
}
