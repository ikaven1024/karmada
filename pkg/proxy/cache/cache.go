package cache

import (
	"context"
	"reflect"
	"sort"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/etcd3"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	controllerruntime "sigs.k8s.io/controller-runtime"

	proxyv1alpha1 "github.com/karmada-io/karmada/pkg/apis/proxy/v1alpha1"
	printerstorage "github.com/karmada-io/karmada/pkg/printers/storage"
	"github.com/karmada-io/karmada/pkg/proxy/scheme"
	"github.com/karmada-io/karmada/pkg/util"
)

// MultiClusterCache caches resource from multi member clusters
type MultiClusterCache struct {
	// clusterName string => *singleClusterCache
	cache sync.Map

	Mgr    controllerruntime.Manager
	Scheme *scheme.Scheme
}

// Get returns the special object
func (c *MultiClusterCache) Get(ctx context.Context, gvr schema.GroupVersionResource, name string, options *metav1.GetOptions) (runtime.Object, error) {
	cluster, cache, err := c.findCacheWithResource(ctx, gvr, request.NamespaceValue(ctx), name)
	if err != nil {
		return nil, err
	}
	obj, err := cache.Get(ctx, name, options)
	if err != nil {
		return nil, err
	}
	rv := newMultiClusterResourceVersionUpdater(false)
	rv.updateObjectRV(cluster, obj)
	return obj, nil
}

// List returns the special object list
// nolint:gocyclo
func (c *MultiClusterCache) List(ctx context.Context, gvr schema.GroupVersionResource, options *metainternalversion.ListOptions) (runtime.Object, error) {
	var resultObject runtime.Object
	items := make([]runtime.Object, 0, options.Limit)

	requestResourceVersion := newMultiClusterResourceVersionFromString(options.ResourceVersion)
	requestContinue := newMultiClusterContinueFromString(options.Continue)

	clusters := c.getClusters()
	sort.Strings(clusters)
	responseResourceVersion := newMultiClusterResourceVersion(len(clusters))
	responseContinue := multiClusterContinue{}

	listFunc := func(cluster string) (int, string, error) {
		if requestContinue.Cluster != "" && requestContinue.Cluster != cluster {
			return 0, "", nil
		}
		responseContinue.Cluster = ""

		options.Continue = requestContinue.Continue
		requestContinue.Continue = ""

		scc := c.singleClusterCache(cluster, gvr)
		if scc == nil {
			return 0, "", nil
		}

		options.ResourceVersion = requestResourceVersion.get(cluster)
		obj, err := scc.List(ctx, options)
		if err != nil {
			return 0, "", err
		}

		list, err := meta.ListAccessor(obj)
		if err != nil {
			return 0, "", err
		}

		if resultObject == nil {
			resultObject = obj
		}
		extractList, err := meta.ExtractList(obj)
		if err != nil {
			return 0, "", err
		}
		items = append(items, extractList...)
		responseResourceVersion.set(cluster, list.GetResourceVersion())
		return len(extractList), list.GetContinue(), nil
	}

	if options.Limit == 0 {
		for _, cluster := range clusters {
			_, _, err := listFunc(cluster)
			if err != nil {
				return nil, err
			}
		}
	} else {
		for clusterIdx, cluster := range clusters {
			n, _continue, err := listFunc(cluster)
			if err != nil {
				return nil, err
			}

			options.Limit -= int64(n)

			if options.Limit <= 0 {
				if _continue != "" {
					// Current cluster has remaining items, return this cluster name and continue for next list.
					responseContinue.Cluster = cluster
					responseContinue.Continue = _continue
				} else if (clusterIdx + 1) < len(clusters) {
					// Current cluster has no remaining items. But we don't known whether next cluster has.
					// So return the next cluster name for continue listing.
					responseContinue.Cluster = clusters[clusterIdx+1]
				} // No more items remains. Break the chuck list.
				break
			}
		}
	}

	if resultObject == nil {
		resultObject = &metav1.List{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "List",
			},
			ListMeta: metav1.ListMeta{},
			Items:    []runtime.RawExtension{},
		}
	}

	accessor, err := meta.ListAccessor(resultObject)
	if err != nil {
		return nil, err
	}
	accessor.SetResourceVersion(responseResourceVersion.String())
	accessor.SetContinue(responseContinue.String())
	return resultObject, nil
}

// Watch watches the special resource
func (c *MultiClusterCache) Watch(ctx context.Context, gvr schema.GroupVersionResource, options *metainternalversion.ListOptions) (watch.Interface, error) {
	resourceVersion := newMultiClusterResourceVersionFromString(options.ResourceVersion)
	clusters := c.getClusters()

	wc := newWatchChan()
	mcrv := newMultiClusterResourceVersionUpdater(true)

	for i := range clusters {
		cluster := clusters[i]
		options.ResourceVersion = resourceVersion.get(cluster)
		scc := c.singleClusterCache(cluster, gvr)
		w, err := scc.Watch(ctx, options)
		if err != nil {
			return nil, err
		}

		go func() {
			for {
				select {
				case <-ctx.Done():
					w.Stop()
					return
				case e, ok := <-w.ResultChan():
					if !ok {
						w.Stop()
						return
					}
					mcrv.updateObjectRV(cluster, e.Object)
					wc.PublishEvent(e)
				}
			}
		}()
	}
	return wc, nil
}

// UpdateCluster updates cache with new configuration。
func (c *MultiClusterCache) UpdateCluster(clusters sets.String, selectors []proxyv1alpha1.ResourceSelector) error {
	// Remove non exist cluster cache
	c.cache.Range(func(key, value interface{}) bool {
		cluster := key.(string)
		if !clusters.Has(cluster) {
			value.(*singleClusterCache).Stop()
			c.cache.Delete(cluster)
		}
		return true
	})

	for cluster := range clusters {
		clusterClient, err := util.NewClusterDynamicClientSet(cluster, c.Mgr.GetClient())
		if err != nil {
			return err
		}

		scc, _ := c.cache.LoadOrStore(cluster, &singleClusterCache{
			cluster:       cluster,
			clusterClient: clusterClient,
			scheme:        c.Scheme,
		})
		err = scc.(*singleClusterCache).UpdateCache(selectors)
		if err != nil {
			return err
		}
	}
	return nil
}

// Stop stop the cache
func (c *MultiClusterCache) Stop() {
	c.cache.Range(func(_, value interface{}) bool {
		value.(*singleClusterCache).Stop()
		return true
	})
}

// getClusters return the name of cached member cluster
func (c *MultiClusterCache) getClusters() []string {
	var clusters []string
	c.cache.Range(func(key, value interface{}) bool {
		clusters = append(clusters, key.(string))
		return true
	})
	return clusters
}

// getSingleClusterCache returns singleClusterCache for cluster.
// If cluster cache exists, true is returns, otherwise false.
func (c *MultiClusterCache) getSingleClusterCache(cluster string) (*singleClusterCache, bool) {
	value, exist := c.cache.Load(cluster)
	if !exist {
		return nil, false
	}
	return value.(*singleClusterCache), true
}

// ClusterFor return which cluster the resource locations.
func (c *MultiClusterCache) ClusterFor(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (string, error) {
	cluster, _, err := c.findCacheWithResource(ctx, gvr, namespace, name)
	return cluster, err
}

// findCacheWithResource return which cluster the resource locations.
func (c *MultiClusterCache) findCacheWithResource(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (string, *clusterResourceCache, error) {
	// Set ResourceVersion=0, find any resource in watchCache.
	options := metav1.GetOptions{ResourceVersion: "0"}
	clusters := c.getClusters()
	for _, cluster := range clusters {
		scc := c.singleClusterCache(cluster, gvr)
		if scc == nil {
			continue
		}

		_, err := scc.Get(request.WithNamespace(ctx, namespace), name, &options)
		if err == nil {
			return cluster, scc, nil
		}
		if !apierrors.IsNotFound(err) {
			klog.Warningf("Fail to get %v %v from %v: %v", namespace, name, cluster, err)
		}
	}
	return "", nil, apierrors.NewNotFound(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
}

// HasResource return whether resource is cached.
func (c *MultiClusterCache) HasResource(gvr schema.GroupVersionResource) bool {
	var ok bool
	c.cache.Range(func(_, value interface{}) bool {
		_, exist := value.(*singleClusterCache).Get(gvr)
		if exist {
			ok = true
		}
		return false
	})
	return ok
}

func (c *MultiClusterCache) singleClusterCache(cluster string, gvr schema.GroupVersionResource) *clusterResourceCache {
	scc, _ := c.getSingleClusterCache(cluster)
	if scc == nil {
		return nil
	}
	crs, _ := scc.Get(gvr)
	return crs
}

// singleClusterCache caches resources for single member cluster
type singleClusterCache struct {
	cluster string
	// schema.GroupVersionResource => *clusterResourceCache
	cache sync.Map

	scheme        *scheme.Scheme
	clusterClient *util.DynamicClusterClient
}

func (c *singleClusterCache) UpdateCache(selectors []proxyv1alpha1.ResourceSelector) error {
	resourceSet := map[schema.GroupVersionResource]proxyv1alpha1.ResourceSelector{}
	for _, selector := range selectors {
		gv, err := schema.ParseGroupVersion(selector.APIVersion)
		if err != nil {
			return err
		}
		gvr, err := c.scheme.ResourceFor(gv.WithKind(selector.Kind))
		if err != nil {
			return err
		}
		resourceSet[gvr] = selector
	}

	c.cache.Range(func(key, value interface{}) bool {
		gvr := key.(schema.GroupVersionResource)
		if _, exist := resourceSet[gvr]; !exist {
			value.(*clusterResourceCache).Stop()
			c.cache.Delete(gvr)
		}
		return true
	})

	for gvr, selector := range resourceSet {
		crc, _ := c.cache.LoadOrStore(gvr, &clusterResourceCache{
			gvr:       gvr,
			gvk:       gvr.GroupVersion().WithKind(selector.Kind),
			scheme:    c.scheme,
			cluster:   c.clusterClient.ClusterName,
			clientset: c.clusterClient.DynamicClientSet.Resource(gvr),
			versioner: etcd3.APIObjectVersioner{},
		})
		err := crc.(*clusterResourceCache).UpdateCache(selector)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *singleClusterCache) Get(gvr schema.GroupVersionResource) (*clusterResourceCache, bool) {
	value, ok := c.cache.Load(gvr)
	if !ok {
		return nil, false
	}
	return value.(*clusterResourceCache), true
}

func (c *singleClusterCache) Stop() {
	c.cache.Range(func(key, value interface{}) bool {
		value.(*clusterResourceCache).Stop()
		return true
	})
}

// clusterResourceCache cache one kind resource for single member cluster
type clusterResourceCache struct {
	*genericregistry.Store

	lock      sync.Mutex
	selector  proxyv1alpha1.ResourceSelector
	scheme    *scheme.Scheme
	cluster   string
	gvk       schema.GroupVersionKind
	gvr       schema.GroupVersionResource
	clientset dynamic.NamespaceableResourceInterface
	versioner storage.Versioner
}

func (c *clusterResourceCache) UpdateCache(selector proxyv1alpha1.ResourceSelector) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.Store != nil && reflect.DeepEqual(c.selector, selector) {
		return nil
	}
	c.stopLocked()

	klog.Infof("Start store for %s %s", c.cluster, c.gvr)

	resource, err := c.scheme.InfoForResource(c.gvr)
	if err != nil {
		return err
	}

	getAttrsFunc := buildGetAttrsFunc(c.gvk, c.scheme)

	s := &genericregistry.Store{
		DefaultQualifiedResource: c.gvr.GroupResource(),
		NewFunc: func() runtime.Object {
			o := &unstructured.Unstructured{}
			o.SetAPIVersion(resource.GVK().GroupVersion().String())
			o.SetKind(resource.Kind)
			return o
		},
		NewListFunc: func() runtime.Object {
			o := &unstructured.UnstructuredList{}
			o.SetAPIVersion(resource.GVK().GroupVersion().String())
			o.SetKind(resource.Kind + "List")
			return o
		},
		TableConvertor: printerstorage.TableConvertor{TableGenerator: c.scheme.TableGenerator(resource.GVK())},
		CreateStrategy: &simpleRESTCreateStrategy{namespaced: resource.Namespaced}, // CreateStrategy tells whether the resource is namespaced.
		DeleteStrategy: &simpleRESTDeleteStrategy{},
	}

	err = s.CompleteWithOptions(&generic.StoreOptions{
		RESTOptions: &generic.RESTOptions{
			StorageConfig: &storagebackend.ConfigForResource{
				Config: storagebackend.Config{
					Paging: utilfeature.DefaultFeatureGate.Enabled(features.APIListChunking),
					Codec:  unstructured.UnstructuredJSONScheme,
				},
				GroupResource: resource.GVR().GroupResource(),
			},
			ResourcePrefix: resource.Group + "/" + resource.Resource,
			Decorator:      storageWithCacher(c.clientset, c.versioner, selector.Selector),
		},
		AttrFunc: getAttrsFunc,
	})
	if err != nil {
		return err
	}

	c.selector = selector
	c.Store = s
	return nil
}

func (c *clusterResourceCache) Stop() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.stopLocked()
}

func (c *clusterResourceCache) stopLocked() {
	if c.Store != nil {
		klog.Infof("Stop store for %s %s", c.cluster, c.gvr)
		c.Store.DestroyFunc()
	}
}
