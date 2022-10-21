package store

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/etcd3"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

// StorageFactory known how to get a store for resource for cluster.
type StorageFactory interface {
	// Build create a store for resource in member cluster.
	Build(cluster string, resource schema.GroupVersionResource) (*genericregistry.Store, error)
}

type storageFactory struct {
	versioner    storage.Versioner
	chanSizes    map[schema.GroupResource]int
	restMapper   meta.RESTMapper
	clientGetter func(string) (dynamic.Interface, error)
}

// NewStoreFactory returns StorageFactory
func NewStoreFactory(chanSizes map[schema.GroupResource]int, restMapper meta.RESTMapper, clientGetter func(string) (dynamic.Interface, error)) StorageFactory {
	return &storageFactory{
		versioner:    etcd3.APIObjectVersioner{},
		chanSizes:    chanSizes,
		restMapper:   restMapper,
		clientGetter: clientGetter,
	}
}

// Build implements StorageFactory.Build
func (f *storageFactory) Build(cluster string, resource schema.GroupVersionResource) (*genericregistry.Store, error) {
	kind, err := f.restMapper.KindFor(resource)
	if err != nil {
		return nil, err
	}
	mapping, err := f.restMapper.RESTMapping(kind.GroupKind(), kind.Version)
	if err != nil {
		return nil, err
	}
	namespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace

	newClientFunc := func() (dynamic.NamespaceableResourceInterface, error) {
		c, err := f.clientGetter(cluster)
		if err != nil {
			return nil, err
		}
		return c.Resource(resource), nil
	}

	restOptions, err := createRESTOptions(resource.GroupResource(), f.chanSizes, f.versioner, newClientFunc)
	if err != nil {
		return nil, err
	}

	return createStore(resource, kind, namespaced, restOptions)
}

func createStore(gvr schema.GroupVersionResource, gvk schema.GroupVersionKind, namespaced bool, restOptions *generic.RESTOptions) (*genericregistry.Store, error) {
	s := &genericregistry.Store{
		DefaultQualifiedResource: gvr.GroupResource(),
		NewFunc: func() runtime.Object {
			o := &unstructured.Unstructured{}
			o.SetAPIVersion(gvk.GroupVersion().String())
			o.SetKind(gvk.Kind)
			return o
		},
		NewListFunc: func() runtime.Object {
			o := &unstructured.UnstructuredList{}
			o.SetAPIVersion(gvk.GroupVersion().String())
			// TODO: it's unsafe guesses kind name for resource list
			o.SetKind(gvk.Kind + "List")
			return o
		},
		TableConvertor: rest.NewDefaultTableConvertor(gvr.GroupResource()),
		// CreateStrategy tells whether the resource is namespaced.
		// see: vendor/k8s.io/apiserver/pkg/registry/generic/registry/store.go#L1310-L1318
		CreateStrategy: restCreateStrategy(namespaced),
		// Assign `DeleteStrategy` to pass vendor/k8s.io/apiserver/pkg/registry/generic/registry/store.go#L1320-L1322
		DeleteStrategy: restDeleteStrategy,
	}

	err := s.CompleteWithOptions(&generic.StoreOptions{
		RESTOptions: restOptions,
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func createRESTOptions(resource schema.GroupResource, sizes map[schema.GroupResource]int, versioner storage.Versioner, newClientFunc func() (dynamic.NamespaceableResourceInterface, error)) (*generic.RESTOptions, error) {
	ret := &generic.RESTOptions{
		StorageConfig: &storagebackend.ConfigForResource{
			Config: storagebackend.Config{
				Paging: utilfeature.DefaultFeatureGate.Enabled(features.APIListChunking),
				Codec:  unstructured.UnstructuredJSONScheme,
			},
			GroupResource: resource,
		},
		ResourcePrefix: resource.Group + "/" + resource.Resource,
	}
	size, ok := sizes[resource]
	if ok && size > 0 {
		klog.Warningf("Dropping watch-cache-size for %v - watchCache size is now dynamic", resource)
	}
	if ok && size <= 0 {
		ret.Decorator = undecoratedStorage(newClientFunc, versioner)
	} else {
		ret.Decorator = storageWithCacher(newClientFunc, versioner)
	}
	return ret, nil
}
