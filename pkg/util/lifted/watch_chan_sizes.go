package lifted

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	serveroptions "k8s.io/apiserver/pkg/server/options"
)

// +lifted:source=https://github.com/kubernetes/kubernetes/blob/release-1.25/cmd/kube-apiserver/app/server.go#L614-L626
// +lifted:changed

// CompleteWatchChanSizes add the default sizes.
func CompleteWatchChanSizes(o *serveroptions.RecommendedOptions) error {
	sizes := DefaultWatchCacheSizes()
	// Ensure that overrides parse correctly.
	userSpecified, err := serveroptions.ParseWatchCacheSizes(o.Etcd.WatchCacheSizes)
	if err != nil {
		return err
	}
	for resource, size := range userSpecified {
		sizes[resource] = size
	}
	o.Etcd.WatchCacheSizes, err = serveroptions.WriteWatchCacheSizes(sizes)
	return nil
}

// +lifted:source=https://github.com/kubernetes/kubernetes/blob/release-1.25/pkg/kubeapiserver/default_storage_factory_builder.go#L50-L57

// DefaultWatchCacheSizes defines default resources for which watchcache
// should be disabled.
func DefaultWatchCacheSizes() map[schema.GroupResource]int {
	return map[schema.GroupResource]int{
		{Resource: "events"}:                         0,
		{Group: "events.k8s.io", Resource: "events"}: 0,
	}
}
