package store

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
)

// clusterCache caches resources for single member cluster
type clusterCache struct {
	lock         sync.RWMutex
	cache        map[schema.GroupVersionResource]*resourceCache
	clusterName  string
	storeFactory StorageFactory
}

func newClusterCache(clusterName string, storeFactory StorageFactory) *clusterCache {
	return &clusterCache{
		clusterName:  clusterName,
		storeFactory: storeFactory,
		cache:        map[schema.GroupVersionResource]*resourceCache{},
	}
}

func (c *clusterCache) updateCache(resources map[schema.GroupVersionResource]struct{}) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	// remove non-exist resources
	for resource := range c.cache {
		if _, exist := resources[resource]; !exist {
			klog.Infof("Remove cache for %s %s", c.clusterName, resource.String())
			c.cache[resource].stop()
			delete(c.cache, resource)
		}
	}

	// add resource cache
	for resource := range resources {
		_, exist := c.cache[resource]
		if !exist {
			s, err := c.storeFactory.Build(c.clusterName, resource)
			if err != nil {
				return err
			}

			klog.Infof("Add cache for %s %s", c.clusterName, resource.String())
			cache, err := newResourceCache(c.clusterName, resource, s)
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
