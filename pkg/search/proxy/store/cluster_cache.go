package store

import (
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// clusterCache caches resources for single member cluster
type clusterCache struct {
	lock        sync.RWMutex
	cache       map[schema.GroupVersionResource]*resourceCache
	clusterName string
	restMapper  meta.RESTMapper
	// newClientFunc returns a dynamic client for member cluster apiserver
	configGetter func() (*restclient.Config, error)
}

func newClusterCache(clusterName string, configGetter func() (*restclient.Config, error), restMapper meta.RESTMapper) *clusterCache {
	return &clusterCache{
		clusterName:  clusterName,
		configGetter: configGetter,
		restMapper:   restMapper,
		cache:        map[schema.GroupVersionResource]*resourceCache{},
	}
}

func (c *clusterCache) updateCache(resources map[schema.GroupVersionResource]*MultiNamespace) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	// remove non-exist resources
	for resource, cache := range c.cache {
		if multiNS, exist := resources[resource]; !exist || !multiNS.Equal(cache.multiNS) {
			klog.Infof("Remove cache for %s %s", c.clusterName, resource.String())
			c.cache[resource].stop()
			delete(c.cache, resource)
		}
	}

	// add resource cache
	for resource, multiNS := range resources {
		_, exist := c.cache[resource]
		if !exist {
			kind, err := c.restMapper.KindFor(resource)
			if err != nil {
				return err
			}
			mapping, err := c.restMapper.RESTMapping(kind.GroupKind(), kind.Version)
			if err != nil {
				return err
			}
			namespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace

			if !namespaced && !multiNS.allNamespaces {
				klog.Warningf("Namespace is invalid for %v, skip it.", kind.String())
				multiNS.Add(metav1.NamespaceAll)
			}

			singularName, err := c.restMapper.ResourceSingularizer(resource.Resource)
			if err != nil {
				klog.Warningf("Failed to get singular name for resource: %s", resource.String())
				return err
			}

			klog.Infof("Add cache for %s %s", c.clusterName, resource.String())
			cache, err := newResourceCache(c.clusterName, resource, kind, singularName, namespaced, multiNS, c.configGetter)
			if err != nil {
				return err
			}
			c.cache[resource] = cache
		}
	}
	return nil
}

func (c *clusterCache) stop() {
	c.lock.RLock()
	defer c.lock.RUnlock()

	for _, cache := range c.cache {
		cache.stop()
	}
}

func (c *clusterCache) cacheForResource(gvr schema.GroupVersionResource) *resourceCache {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.cache[gvr]
}
