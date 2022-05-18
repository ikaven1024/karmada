// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/karmada-io/karmada/pkg/apis/proxy/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// GlobalResourceLister helps list GlobalResources.
// All objects returned here must be treated as read-only.
type GlobalResourceLister interface {
	// List lists all GlobalResources in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.GlobalResource, err error)
	// Get retrieves the GlobalResource from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.GlobalResource, error)
	GlobalResourceListerExpansion
}

// globalResourceLister implements the GlobalResourceLister interface.
type globalResourceLister struct {
	indexer cache.Indexer
}

// NewGlobalResourceLister returns a new GlobalResourceLister.
func NewGlobalResourceLister(indexer cache.Indexer) GlobalResourceLister {
	return &globalResourceLister{indexer: indexer}
}

// List lists all GlobalResources in the indexer.
func (s *globalResourceLister) List(selector labels.Selector) (ret []*v1alpha1.GlobalResource, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.GlobalResource))
	})
	return ret, err
}

// Get retrieves the GlobalResource from the index for a given name.
func (s *globalResourceLister) Get(name string) (*v1alpha1.GlobalResource, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("globalresource"), name)
	}
	return obj.(*v1alpha1.GlobalResource), nil
}
