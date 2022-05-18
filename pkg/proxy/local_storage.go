package proxy

import (
	"context"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/handlers"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/karmada-io/karmada/pkg/printers/storage"
	"github.com/karmada-io/karmada/pkg/proxy/cache"
	proxyScheme "github.com/karmada-io/karmada/pkg/proxy/scheme"
)

// LocalStorage cache resources from member clusters
type LocalStorage struct {
	scheme            *proxyScheme.Scheme
	minRequestTimeout time.Duration
}

func newLocalStorage(scheme *proxyScheme.Scheme, minRequestTimeout time.Duration) *LocalStorage {
	return &LocalStorage{
		scheme:            scheme,
		minRequestTimeout: minRequestTimeout,
	}
}

// connect to local cache
func (s *LocalStorage) connect(ctx context.Context, store *cache.MultiClusterCache) (http.Handler, error) {
	requestInfo, _ := request.RequestInfoFrom(ctx)
	gvr := schema.GroupVersionResource{
		Group:    requestInfo.APIGroup,
		Version:  requestInfo.APIVersion,
		Resource: requestInfo.Resource,
	}
	resource, err := store.Scheme.InfoForResource(gvr)
	if err != nil {
		return nil, err
	}

	r := &rester{
		store:          store,
		gvr:            gvr,
		name:           requestInfo.Name,
		tableConvertor: nil,
		newListFunc:    func() runtime.Object { return &unstructured.UnstructuredList{} },
	}

	var tableConvertor storage.TableConvertor
	if tableGenerator := s.scheme.TableGenerator(resource.GVK()); tableGenerator != nil {
		tableConvertor = storage.TableConvertor{
			TableGenerator: tableGenerator,
		}
	}

	scope := &handlers.RequestScope{
		Namer: &handlers.ContextBasedNaming{
			Namer:         meta.NewAccessor(),
			ClusterScoped: !resource.Namespaced,
		},
		Serializer:       scheme.Codecs.WithoutConversion(),
		TableConvertor:   tableConvertor,
		Resource:         gvr,
		Kind:             resource.GVK(),
		Convertor:        store.Scheme,
		Subresource:      requestInfo.Subresource,
		MetaGroupVersion: metav1.SchemeGroupVersion,
	}

	var h http.Handler
	if requestInfo.Verb == "watch" || requestInfo.Name == "" {
		// for list or watch
		h = handlers.ListResource(r, r, scope, false, s.minRequestTimeout)
	} else {
		h = handlers.GetResource(r, scope)
	}
	return h, nil
}
