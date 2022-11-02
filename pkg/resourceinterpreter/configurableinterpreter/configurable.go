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
	interpreters map[schema.GroupVersionKind]map[configv1alpha1.InterpreterOperation]engine.Function
	configLister cache.GenericLister
}

// NewConfigurableInterpreter return a new ConfigurableInterpreter.
func NewConfigurableInterpreter(informer genericmanager.SingleClusterInformerManager) *ConfigurableInterpreter {
	c := &ConfigurableInterpreter{
		configLister: informer.Lister(resourceInterpreterCustomizationsGVR),
	}

	informer.ForResource(resourceInterpreterCustomizationsGVR, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.updateConfiguration() },
		UpdateFunc: func(_, _ interface{}) { c.updateConfiguration() },
		DeleteFunc: func(_ interface{}) { c.updateConfiguration() },
	})
	return c
}

// HookEnabled tells if any hook exist for specific resource type and operation.
func (c *ConfigurableInterpreter) HookEnabled(objGVK schema.GroupVersionKind, operation configv1alpha1.InterpreterOperation) bool {
	_, enabled := c.getInterpreter(objGVK, operation)
	return enabled
}

// GetReplicas returns the desired replicas of the object as well as the requirements of each replica.
func (c *ConfigurableInterpreter) GetReplicas(object *unstructured.Unstructured) (replicas int32, require *workv1alpha2.ReplicaRequirements, err error) {
	eng, _ := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationInterpretReplica)
	rets, err := eng.Invoke(object)

	if r := rets[1]; !r.IsNil() {
		require = &workv1alpha2.ReplicaRequirements{}
		r.ConvertTo(require)
	}
	return rets[0].Int32(), require, err
}

// ReviseReplica revises the replica of the given object.
func (c *ConfigurableInterpreter) ReviseReplica(object *unstructured.Unstructured, replica int64) (*unstructured.Unstructured, error) {
	eng, _ := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationReviseReplica)
	rets, err := eng.Invoke(object, replica)
	return toUnstructured(rets[0]), err
}

// Retain returns the objects that based on the "desired" object but with values retained from the "observed" object.
func (c *ConfigurableInterpreter) Retain(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, err error) {
	eng, _ := c.getInterpreter(desired.GroupVersionKind(), configv1alpha1.InterpreterOperationRetain)
	rets, err := eng.Invoke(desired, observed)
	return toUnstructured(rets[0]), err
}

// AggregateStatus returns the objects that based on the 'object' but with status aggregated.
func (c *ConfigurableInterpreter) AggregateStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	eng, _ := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationAggregateStatus)
	rets, err := eng.Invoke(object, aggregatedStatusItems)
	return toUnstructured(rets[0]), err
}

// GetDependencies returns the dependent resources of the given object.
func (c *ConfigurableInterpreter) GetDependencies(object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, err error) {
	eng, _ := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationInterpretDependency)
	rets, err := eng.Invoke(object)

	if r := rets[0]; !r.IsNil() {
		r.ConvertTo(&dependencies)
	}
	return
}

// ReflectStatus returns the status of the object.
func (c *ConfigurableInterpreter) ReflectStatus(object *unstructured.Unstructured) (status *runtime.RawExtension, err error) {
	eng, _ := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationInterpretStatus)
	rets, err := eng.Invoke(object)

	if r := rets[0]; !r.IsNil() {
		status = &runtime.RawExtension{}
		r.ConvertTo(status)
	}
	return
}

// InterpretHealth returns the health state of the object.
func (c *ConfigurableInterpreter) InterpretHealth(object *unstructured.Unstructured) (bool, error) {
	eng, _ := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationInterpretHealth)
	rets, err := eng.Invoke(object)
	return rets[0].Bool(), err
}

// getInterpreter returns interpreter for specific resource kind and operation.
func (c *ConfigurableInterpreter) getInterpreter(kind schema.GroupVersionKind, operation configv1alpha1.InterpreterOperation) (engine.Function, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	interpreter, ok := c.interpreters[kind][operation]
	return interpreter, ok
}

func (c *ConfigurableInterpreter) updateConfiguration() {
	objs, err := c.configLister.List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error updating configuration: %v", err))
		return
	}
	configs := make([]*configv1alpha1.ResourceInterpreterCustomization, len(objs))
	for i, obj := range objs {
		config := &configv1alpha1.ResourceInterpreterCustomization{}
		if err = helper.ConvertToTypedObject(obj, config); err != nil {
			klog.Errorf("Failed to transform ResourceInterpreterCustomization: %w", err)
			return
		}
		configs[i] = config
	}

	newInterpreters, err := loadConfig(configs)
	if err != nil {
		klog.Error(err)
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.interpreters = newInterpreters
}

func loadConfig(configs []*configv1alpha1.ResourceInterpreterCustomization) (map[schema.GroupVersionKind]map[configv1alpha1.InterpreterOperation]engine.Function, error) {
	interpreters := map[schema.GroupVersionKind]map[configv1alpha1.InterpreterOperation]engine.Function{}
	for _, customization := range configs {
		gv, err := schema.ParseGroupVersion(customization.Spec.Target.APIVersion)
		if err != nil {
			return nil, err
		}
		kind := gv.WithKind(customization.Spec.Target.Kind)
		interps, ok := interpreters[kind]
		if !ok {
			interps = map[configv1alpha1.InterpreterOperation]engine.Function{}
			interpreters[kind] = interps
		}
		if err = loadRulersInto(customization.Spec.Customizations, interps); err != nil {
			return nil, err
		}
	}
	return interpreters, nil
}

//nolint:gocyclo
func loadRulersInto(rulers configv1alpha1.CustomizationRules, into map[configv1alpha1.InterpreterOperation]engine.Function) error {
	if rulers.Retention != nil && rulers.Retention.LuaScript != "" {
		f, err := lua.Load(rulers.Retention.LuaScript)
		if err != nil {
			return err
		}
		into[configv1alpha1.InterpreterOperationRetain] = f
	}

	if rulers.ReplicaResource != nil && rulers.ReplicaResource.LuaScript != "" {
		f, err := lua.Load(rulers.ReplicaResource.LuaScript)
		if err != nil {
			return err
		}
		into[configv1alpha1.InterpreterOperationInterpretReplica] = f
	}

	if rulers.ReplicaRevision != nil && rulers.ReplicaRevision.LuaScript != "" {
		f, err := lua.Load(rulers.ReplicaRevision.LuaScript)
		if err != nil {
			return err
		}
		into[configv1alpha1.InterpreterOperationReviseReplica] = f
	}

	if rulers.StatusReflection != nil && rulers.StatusReflection.LuaScript != "" {
		f, err := lua.Load(rulers.StatusReflection.LuaScript)
		if err != nil {
			return err
		}
		into[configv1alpha1.InterpreterOperationInterpretStatus] = f
	}

	if rulers.StatusAggregation != nil && rulers.StatusAggregation.LuaScript != "" {
		f, err := lua.Load(rulers.StatusAggregation.LuaScript)
		if err != nil {
			return err
		}
		into[configv1alpha1.InterpreterOperationAggregateStatus] = f
	}

	if rulers.HealthInterpretation != nil && rulers.HealthInterpretation.LuaScript != "" {
		f, err := lua.Load(rulers.HealthInterpretation.LuaScript)
		if err != nil {
			return err
		}
		into[configv1alpha1.InterpreterOperationInterpretHealth] = f
	}

	if rulers.DependencyInterpretation != nil && rulers.DependencyInterpretation.LuaScript != "" {
		f, err := lua.Load(rulers.DependencyInterpretation.LuaScript)
		if err != nil {
			return err
		}
		into[configv1alpha1.InterpreterOperationInterpretDependency] = f
	}
	return nil
}

func toUnstructured(v engine.Value) *unstructured.Unstructured {
	if v.IsNil() {
		return nil
	}
	u := &unstructured.Unstructured{}
	v.ConvertTo(u)
	return u
}
