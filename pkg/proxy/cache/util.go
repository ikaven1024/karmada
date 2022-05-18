package cache

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"
	cacherstorage "k8s.io/apiserver/pkg/storage/cacher"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/apiserver/pkg/storage/storagebackend/factory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"github.com/karmada-io/karmada/pkg/proxy/scheme"
)

type multiClusterResourceVersionUpdater struct {
	rv multiClusterResourceVersion
	sync.Mutex
	lock bool
}

func newMultiClusterResourceVersionUpdater(lock bool) *multiClusterResourceVersionUpdater {
	return &multiClusterResourceVersionUpdater{
		rv: multiClusterResourceVersion{
			rvs: make(map[string]string),
		},
		lock: lock,
	}
}

func (m *multiClusterResourceVersionUpdater) updateObjectRV(cluster string, object runtime.Object) {
	accessor, err := meta.Accessor(object)
	if err != nil {
		return
	}

	if m.lock {
		m.Lock()
		defer m.Unlock()
	}

	m.rv.set(cluster, accessor.GetResourceVersion())
	accessor.SetResourceVersion(m.rv.String())
}

type multiClusterResourceVersion struct {
	isEmpty, isZero bool
	rvs             map[string]string
}

func newMultiClusterResourceVersion(capacity int) multiClusterResourceVersion {
	return multiClusterResourceVersion{
		rvs: make(map[string]string, capacity),
	}
}

func newMultiClusterResourceVersionFromString(rv string) multiClusterResourceVersion {
	m := newMultiClusterResourceVersion(0)
	if rv == "" {
		m.isEmpty = true
		return m
	}
	if rv == "0" {
		m.isZero = true
		return m
	}

	bs, err := base64.RawURLEncoding.DecodeString(rv)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(bs, &m.rvs)
	return m
}

func (m multiClusterResourceVersion) get(cluster string) string {
	if m.isEmpty {
		return ""
	}
	if m.isZero {
		return "0"
	}
	return m.rvs[cluster]
}

func (m multiClusterResourceVersion) set(cluster, rv string) {
	m.rvs[cluster] = rv
}

func (m multiClusterResourceVersion) String() string {
	if m.isEmpty {
		return ""
	}
	if m.isZero {
		return "0"
	}
	if len(m.rvs) == 0 {
		return ""
	}
	bs, _ := json.Marshal(&m.rvs)
	return base64.RawURLEncoding.EncodeToString(bs)
}

type multiClusterContinue struct {
	Cluster  string `json:"cluster,omitempty"`
	Continue string `json:"continue,omitempty"`
}

func newMultiClusterContinueFromString(s string) multiClusterContinue {
	var m multiClusterContinue
	if s == "" {
		return m
	}

	bs, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(bs, &m)
	return m
}

func (c *multiClusterContinue) String() string {
	if c.Cluster == "" {
		return ""
	}
	buf, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func splitIntoNamespaceAndName(s string) (string, string) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}
	return parts[0], parts[1]
}

func listOptionsWithSelector(opts metav1.ListOptions, selector *metav1.LabelSelector) metav1.ListOptions {
	if selector != nil {
		requirements, _ := labels.ParseToRequirements(opts.LabelSelector)
		s, _ := metav1.LabelSelectorAsSelector(selector)
		s.Add(requirements...)
		opts.LabelSelector = s.String()
	}
	return opts
}

func buildGetAttrsFunc(gvk schema.GroupVersionKind, sch *scheme.Scheme) func(runtime.Object) (label labels.Set, field fields.Set, err error) {
	f := sch.SelectableFieldFunc(gvk)
	return func(object runtime.Object) (label labels.Set, field fields.Set, err error) {
		accessor, err := meta.Accessor(object)
		if err != nil {
			return nil, nil, err
		}
		label = accessor.GetLabels()
		if f == nil {
			field = make(fields.Set)
		} else {
			field = f(object)
		}
		return label, field, nil
	}
}

type simpleRESTDeleteStrategy struct {
	rest.RESTDeleteStrategy
}

type simpleRESTCreateStrategy struct {
	namespaced bool
	rest.RESTCreateStrategy
}

var _ rest.RESTCreateStrategy = &simpleRESTCreateStrategy{}

func (s simpleRESTCreateStrategy) NamespaceScoped() bool {
	return s.namespaced
}

func storageWithCacher(clientset dynamic.NamespaceableResourceInterface, versioner storage.Versioner, selector *metav1.LabelSelector) generic.StorageDecorator {
	return func(
		storageConfig *storagebackend.ConfigForResource,
		resourcePrefix string,
		keyFunc func(obj runtime.Object) (string, error),
		newFunc func() runtime.Object,
		newListFunc func() runtime.Object,
		getAttrsFunc storage.AttrFunc,
		triggerFuncs storage.IndexerFuncs,
		indexers *cache.Indexers) (storage.Interface, factory.DestroyFunc, error) {
		cacherConfig := cacherstorage.Config{
			Storage:        newStore(clientset, versioner, selector, resourcePrefix),
			Versioner:      versioner,
			ResourcePrefix: resourcePrefix,
			KeyFunc:        keyFunc,
			GetAttrsFunc:   getAttrsFunc,
			Indexers:       indexers,
			NewFunc:        newFunc,
			NewListFunc:    newListFunc,
			Codec:          storageConfig.Codec,
		}
		cacher, err := cacherstorage.NewCacherFromConfig(cacherConfig)
		if err != nil {
			return nil, nil, err
		}
		return cacher, cacher.Stop, nil
	}
}
