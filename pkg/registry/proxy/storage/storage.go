package storage

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"

	proxyapis "github.com/karmada-io/karmada/pkg/apis/proxy"
	"github.com/karmada-io/karmada/pkg/printers"
	printersinternal "github.com/karmada-io/karmada/pkg/printers/internalversion"
	printerstorage "github.com/karmada-io/karmada/pkg/printers/storage"
	proxyregistry "github.com/karmada-io/karmada/pkg/registry/proxy"
)

// GlobalResourceStorage includes storage for GlobalResource and for all the subresources.
type GlobalResourceStorage struct {
	GlobalResource *REST
	Status         *StatusREST
	Proxy          *ProxyREST
}

// NewStorage returns a GlobalResourceStorage object that will work against globalResources.
func NewStorage(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter, connector Connector) (*GlobalResourceStorage, error) {
	strategy := proxyregistry.NewStrategy(scheme)

	store := &genericregistry.Store{
		NewFunc:                  func() runtime.Object { return &proxyapis.GlobalResource{} },
		NewListFunc:              func() runtime.Object { return &proxyapis.GlobalResourceList{} },
		DefaultQualifiedResource: proxyapis.Resource("globalresources"),

		CreateStrategy: strategy,
		UpdateStrategy: strategy,
		DeleteStrategy: strategy,

		TableConvertor: printerstorage.TableConvertor{TableGenerator: printers.NewTableGenerator().With(printersinternal.AddHandlers)},
	}

	options := &generic.StoreOptions{RESTOptions: optsGetter}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, err
	}

	statusStore := *store

	return &GlobalResourceStorage{
		GlobalResource: &REST{Store: store},
		Status:         &StatusREST{store: &statusStore},
		Proxy:          &ProxyREST{connector: connector},
	}, nil
}

// REST implements a RESTStorage for GlobalResource.
type REST struct {
	*genericregistry.Store
}

// StatusREST implements the REST endpoint for changing the status of a globalResource.
type StatusREST struct {
	store *genericregistry.Store
}
