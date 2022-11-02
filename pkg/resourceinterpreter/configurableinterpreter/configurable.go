package configurableinterpreter

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine/lua"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/genericmanager"
	"github.com/karmada-io/karmada/pkg/util/helper"
)

var resourceInterpreterCustomizationsGVR = schema.GroupVersionResource{
	Group:    configv1alpha1.GroupVersion.Group,
	Version:  configv1alpha1.GroupVersion.Version,
	Resource: "resourceinterpretercustomizations",
}

// ConfigurableInterpreter interpret custom resource with resource interpreter customizations configuration
type ConfigurableInterpreter struct {
	lock         sync.RWMutex
	interpreters map[schema.GroupVersionKind]map[configv1alpha1.InterpreterOperation]engine.Engine

	configLister cache.GenericLister
}

// NewConfigurableInterpreter return a new ConfigurableInterpreter.
func NewConfigurableInterpreter(informer genericmanager.SingleClusterInformerManager) *ConfigurableInterpreter {
	c := &ConfigurableInterpreter{
		configLister: informer.Lister(resourceInterpreterCustomizationsGVR),
	}

	informer.ForResource(resourceInterpreterCustomizationsGVR, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.updateConfiguration() },
		UpdateFunc: nil,
		DeleteFunc: nil,
	})
	return c
}

// HookEnabled tells if any hook exist for specific resource gvk and operation type.
func (c *ConfigurableInterpreter) HookEnabled(kind schema.GroupVersionKind, operationType configv1alpha1.InterpreterOperation) bool {
	eng := c.getInterpreter(kind, operationType)
	return eng != nil
}

// GetReplicas returns the desired replicas of the object as well as the requirements of each replica.
func (c *ConfigurableInterpreter) GetReplicas(object *unstructured.Unstructured) (int32, *workv1alpha2.ReplicaRequirements, error) {
	eng := c.mustGetInterpreter(object.GroupVersionKind(), "GetReplicas")
	rets, err := eng.Call([]interface{}{object})
	if err != nil {
		return 0, nil, err
	}
	return rets[0].(int32), rets[1].(*workv1alpha2.ReplicaRequirements), nil
}

// ReviseReplica revises the replica of the given object.
func (c *ConfigurableInterpreter) ReviseReplica(object *unstructured.Unstructured, replica int64) (*unstructured.Unstructured, error) {
	return nil, nil
}

// Retain returns the objects that based on the "desired" object but with values retained from the "observed" object.
func (c *ConfigurableInterpreter) Retain(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, err error) {
	return nil, err
}

// AggregateStatus returns the objects that based on the 'object' but with status aggregated.
func (c *ConfigurableInterpreter) AggregateStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	return nil, nil
}

// GetDependencies returns the dependent resources of the given object.
func (c *ConfigurableInterpreter) GetDependencies(object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, err error) {
	return nil, err
}

// ReflectStatus returns the status of the object.
func (c *ConfigurableInterpreter) ReflectStatus(object *unstructured.Unstructured) (status *runtime.RawExtension, err error) {
	return nil, err
}

// InterpretHealth returns the health state of the object.
func (c *ConfigurableInterpreter) InterpretHealth(object *unstructured.Unstructured) (bool, error) {
	return false, nil
}

func (c *ConfigurableInterpreter) getInterpreter(kind schema.GroupVersionKind, operationType configv1alpha1.InterpreterOperation) engine.Engine {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.interpreters[kind][operationType]
}

func (c *ConfigurableInterpreter) mustGetInterpreter(kind schema.GroupVersionKind, operationType configv1alpha1.InterpreterOperation) engine.Engine {
	eng := c.getInterpreter(kind, operationType)
	if eng == nil {
		utilruntime.HandleError(fmt.Errorf("%v for %v is not supported", operationType, kind))
	}
	return eng
}

func (c *ConfigurableInterpreter) updateConfiguration() error {
	objs, err := c.configLister.List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error updating configuration: %v", err))
		return nil
	}
	configs := make([]*configv1alpha1.ResourceInterpreterCustomization, len(objs))
	for i, obj := range objs {
		config := &configv1alpha1.ResourceInterpreterCustomization{}
		if err = helper.ConvertToTypedObject(obj, config); err != nil {
			klog.Errorf("Failed to transform ResourceInterpreterCustomization: %w", err)
			return nil
		}
		configs[i] = config
	}

	return c.loadConfig(configs)
}

func (c *ConfigurableInterpreter) loadConfig(configs []*configv1alpha1.ResourceInterpreterCustomization) error {
	newInterpreters := map[schema.GroupVersionKind]map[configv1alpha1.InterpreterOperation]engine.Engine{}
	for _, customization := range configs {
		gv, err := schema.ParseGroupVersion(customization.Spec.Target.APIVersion)
		if err != nil {
			return err
		}
		kind := gv.WithKind(customization.Spec.Target.Kind)
		interps, ok := newInterpreters[kind]
		if !ok {
			interps = map[configv1alpha1.InterpreterOperation]engine.Engine{}
			newInterpreters[kind] = interps
		}
		if err = loadInto(customization.Spec.Customizations, interps); err != nil {
			return err
		}
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.interpreters = newInterpreters
	return nil
}

func loadInto(rulers configv1alpha1.CustomizationRules, into map[configv1alpha1.InterpreterOperation]engine.Engine) error {
	if err := loadScriptInto("Retention", rulers.Retention, into); err != nil {
		return err
	}
	if err := loadScriptInto("ReplicaResource", rulers.ReplicaResource, into); err != nil {
		return err
	}
	if err := loadScriptInto("ReplicaRevision", rulers.ReplicaRevision, into); err != nil {
		return err
	}
	if err := loadScriptInto("StatusReflection", rulers.StatusReflection, into); err != nil {
		return err
	}
	if err := loadScriptInto("StatusAggregation", rulers.StatusAggregation, into); err != nil {
		return err
	}
	if err := loadScriptInto("HealthInterpretation", rulers.HealthInterpretation, into); err != nil {
		return err
	}
	if err := loadScriptInto("DependencyInterpretation", rulers.DependencyInterpretation, into); err != nil {
		return err
	}
	return nil
}

func loadScriptInto(operation configv1alpha1.InterpreterOperation, interpretation *configv1alpha1.Interpretation, interpreters map[configv1alpha1.InterpreterOperation]engine.Engine) error {
	var err error
	switch {
	case interpretation.LuaScript != "":
		interpreters[operation], err = lua.Load(interpretation.LuaScript)
		if err != nil {
			return err
		}
		// for other scripts
	}
	return nil
}
