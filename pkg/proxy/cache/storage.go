package cache

import (
	"context"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/client-go/dynamic"
)

// store implements storage.Interface, Providing resource Get/Watch/List from member cluster apiserver.
type store struct {
	storage.Interface

	clientset dynamic.NamespaceableResourceInterface
	versioner storage.Versioner
	selector  *metav1.LabelSelector
	prefix    string
}

func newStore(clientset dynamic.NamespaceableResourceInterface, versioner storage.Versioner, selector *metav1.LabelSelector, prefix string) *store {
	return &store{
		clientset: clientset,
		versioner: versioner,
		selector:  selector,
		prefix:    prefix,
	}
}

// Versioner implements storage.Interface.
func (s *store) Versioner() storage.Versioner {
	return s.versioner
}

// Watch implements storage.Interface.
// TODO
func (s *store) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	key = strings.TrimPrefix(key, s.prefix)
	return s.WatchList(ctx, key, opts)
}

// WatchList implements storage.Interface.
// TODO
func (s *store) WatchList(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	key = strings.TrimPrefix(key, s.prefix)
	options := convertToMetaV1ListOptions(opts)
	options = listOptionsWithSelector(options, s.selector)

	namespace, _ := splitIntoNamespaceAndName(key)
	return s.resourceInterface(namespace).Watch(ctx, options)
}

// Get implements storage.Interface.
func (s *store) Get(ctx context.Context, key string, opts storage.GetOptions, objPtr runtime.Object) error {
	key = strings.TrimPrefix(key, s.prefix)
	namespace, name := splitIntoNamespaceAndName(key)
	obj, err := s.resourceInterface(namespace).Get(ctx, name, convertToMetaV1GetOptions(opts))
	if err != nil {
		return err
	}

	unstructuredObj := objPtr.(*unstructured.Unstructured)
	obj.DeepCopyInto(unstructuredObj)
	return nil
}

// GetList implements storage.Interface.
// TODO
func (s *store) GetList(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	key = strings.TrimPrefix(key, s.prefix)
	return s.List(ctx, key, opts, listObj)
}

// List implements storage.Interface.
// TODO
func (s *store) List(ctx context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	key = strings.TrimPrefix(key, s.prefix)
	namespace := key
	options := convertToMetaV1ListOptions(opts)
	options = listOptionsWithSelector(options, s.selector)
	objects, err := s.resourceInterface(namespace).List(ctx, options)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	objects.DeepCopyInto(listObj.(*unstructured.UnstructuredList))
	return nil
}

func (s *store) resourceInterface(namespace string) dynamic.ResourceInterface {
	if len(namespace) > 0 {
		return s.clientset.Namespace(namespace)
	}
	return s.clientset
}

// watchChan implements watch.Interface.
type watchChan struct {
	ch chan watch.Event
}

func newWatchChan() *watchChan {
	return &watchChan{
		ch: make(chan watch.Event, 1024),
	}
}

func (w *watchChan) Add(obj runtime.Object) {
	w.publish(watch.Added, obj)
}

func (w *watchChan) Update(obj runtime.Object) {
	w.publish(watch.Modified, obj)
}

func (w *watchChan) Delete(obj runtime.Object) {
	w.publish(watch.Deleted, obj)
}

func (w *watchChan) PublishEvent(event watch.Event) {
	w.ch <- event
}

func (w *watchChan) Stop() {
	close(w.ch)
}

func (w *watchChan) ResultChan() <-chan watch.Event {
	return w.ch
}

func (w *watchChan) publish(typ watch.EventType, obj runtime.Object) {
	w.PublishEvent(watch.Event{
		Type:   typ,
		Object: obj,
	})
}
