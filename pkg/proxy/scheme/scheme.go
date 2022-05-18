package scheme

import (
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/karmada-io/karmada/pkg/printers"
)

// Scheme defines resource information
type Scheme struct {
	*runtime.Scheme
	lock sync.RWMutex

	gvrToGVK  map[schema.GroupVersionResource]schema.GroupVersionKind
	gvkToInfo map[schema.GroupVersionKind]ResourceInfo

	fieldLabelConversionFuncs map[schema.GroupKind]runtime.FieldLabelConversionFunc
	selectableFieldFuncs      map[schema.GroupKind]func(object runtime.Object) fields.Set
	tableGenerators           map[schema.GroupKind]printers.TableGenerator
}

var _ runtime.ObjectConvertor = &Scheme{}

// NewScheme returns a instance of Scheme
func NewScheme(s *runtime.Scheme) *Scheme {
	return &Scheme{
		Scheme:                    s,
		gvrToGVK:                  map[schema.GroupVersionResource]schema.GroupVersionKind{},
		gvkToInfo:                 map[schema.GroupVersionKind]ResourceInfo{},
		fieldLabelConversionFuncs: map[schema.GroupKind]runtime.FieldLabelConversionFunc{},
		selectableFieldFuncs:      map[schema.GroupKind]func(runtime.Object) fields.Set{},
		tableGenerators:           map[schema.GroupKind]printers.TableGenerator{},
	}
}

// AddResourceInfo registers resource
func (s *Scheme) AddResourceInfo(info ResourceInfo) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.gvrToGVK[info.GVR()] = info.GVK()
	s.gvkToInfo[info.GVK()] = info
}

// AddFieldLabelConversionFunc overrides runtime.Scheme#AddFieldLabelConversionFunc
func (s *Scheme) AddFieldLabelConversionFunc(gvk schema.GroupVersionKind, conversion runtime.FieldLabelConversionFunc) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.fieldLabelConversionFuncs[gvk.GroupKind()] = conversion
}

// AddSelectableFieldFunc register selectableFieldFunc for resource
func (s *Scheme) AddSelectableFieldFunc(gvk schema.GroupVersionKind, f func(runtime.Object) fields.Set) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.selectableFieldFuncs[gvk.GroupKind()] = f
}

// AddTableGenerator register TableGenerator for resource
func (s *Scheme) AddTableGenerator(gvk schema.GroupVersionKind, convertor printers.TableGenerator) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.tableGenerators[gvk.GroupKind()] = convertor
}

// ConvertFieldLabel implements runtime.ObjectConvertor
func (s *Scheme) ConvertFieldLabel(gvk schema.GroupVersionKind, label, value string) (string, string, error) {
	conversionFunc, ok := s.fieldLabelConversionFuncs[gvk.GroupKind()]
	if !ok {
		return runtime.DefaultMetaV1FieldSelectorConversion(label, value)
	}
	return conversionFunc(label, value)
}

// TableGenerator returns TableGenerator for resource. Returns nil when not registered.
func (s *Scheme) TableGenerator(gvk schema.GroupVersionKind) printers.TableGenerator {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.tableGenerators[gvk.GroupKind()]
}

// SelectableFieldFunc returns selectableFieldFunc for resource. Returns nil when not registered.
func (s *Scheme) SelectableFieldFunc(gvk schema.GroupVersionKind) func(object runtime.Object) fields.Set {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.selectableFieldFuncs[gvk.GroupKind()]
}

// KindFor returns kind for resource. Returns meta.NoResourceMatchError error when not registered.
func (s *Scheme) KindFor(gvr schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	gvk, exist := s.gvrToGVK[gvr]
	if !exist {
		return schema.GroupVersionKind{}, &meta.NoResourceMatchError{PartialResource: gvr}
	}
	return gvk, nil
}

// ResourceFor returns resource for kind. Returns meta.NoKindMatchError error when not registered.
func (s *Scheme) ResourceFor(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	info, err := s.InfoFor(gvk)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	return info.GVR(), nil
}

// InfoForResource returns resource information for resource. Returns meta.NoKindMatchError error when not registered.
func (s *Scheme) InfoForResource(gvr schema.GroupVersionResource) (ResourceInfo, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if gvk, ok := s.gvrToGVK[gvr]; ok {
		if r, ok := s.gvkToInfo[gvk]; ok {
			return r, nil
		}
	}
	return ResourceInfo{}, &meta.NoResourceMatchError{
		PartialResource: gvr,
	}
}

// InfoFor returns resource information for resource. Returns meta.NoKindMatchError error when not registered.
func (s *Scheme) InfoFor(gvk schema.GroupVersionKind) (ResourceInfo, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	info, exist := s.gvkToInfo[gvk]
	if !exist {
		return ResourceInfo{}, &meta.NoKindMatchError{
			GroupKind:        gvk.GroupKind(),
			SearchedVersions: []string{gvk.Version},
		}
	}
	return info, nil
}

// ResourceInfo is information for resource
type ResourceInfo struct {
	Group      string
	Version    string
	Kind       string
	Resource   string
	Namespaced bool
}

// GVK return GroupVersionKind for resource
func (r *ResourceInfo) GVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   r.Group,
		Version: r.Version,
		Kind:    r.Kind,
	}
}

// GVR return GroupVersionResource for resource
func (r *ResourceInfo) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    r.Group,
		Version:  r.Version,
		Resource: r.Resource,
	}
}
