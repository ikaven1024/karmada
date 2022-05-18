package proxy

import (
	"context"

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/rest"

	"github.com/karmada-io/karmada/pkg/proxy/cache"
)

type rester struct {
	store          *cache.MultiClusterCache
	gvr            schema.GroupVersionResource
	name           string
	newListFunc    func() runtime.Object
	tableConvertor rest.TableConvertor
}

var _ rest.Getter = &rester{}
var _ rest.Lister = &rester{}
var _ rest.Watcher = &rester{}

// Watch implements rest.Watcher interface
func (r rester) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	return r.store.Watch(ctx, r.gvr, options)
}

// NewList implements rest.Lister interface
func (r rester) NewList() runtime.Object {
	return r.newListFunc()
}

// List implements rest.Lister interface
func (r rester) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	return r.store.List(ctx, r.gvr, options)
}

// ConvertToTable implements rest.Lister interface
func (r rester) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return r.tableConvertor.ConvertToTable(ctx, object, tableOptions)
}

// Get implements rest.Getter interface
func (r rester) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, r.gvr, name, options)
}
