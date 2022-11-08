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
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine/luavm"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/genericmanager"
	"github.com/karmada-io/karmada/pkg/util/helper"
)

var resourceInterpreterCustomizationsGVR = schema.GroupVersionResource{
	Group:    configv1alpha1.GroupVersion.Group,
	Version:  configv1alpha1.GroupVersion.Version,
	Resource: "resourceinterpretercustomizations",
}

// ConfigurableInterpreter interprets resources with resource interpreter customizations.
type ConfigurableInterpreter struct {
	lock         sync.RWMutex
	configLister cache.GenericLister
	interpreters map[schema.GroupVersionKind]interpretersByOperations
	ruleLoader   ruleLoader
}

type interpretersByOperations map[configv1alpha1.InterpreterOperation]engine.Function

// NewConfigurableInterpreter builds a new interpreter by registering the
// event handler to the provided informer instance.
func NewConfigurableInterpreter(informer genericmanager.SingleClusterInformerManager) *ConfigurableInterpreter {
	c := &ConfigurableInterpreter{
		configLister: informer.Lister(resourceInterpreterCustomizationsGVR),
		ruleLoader:   defaultRuleLoader,
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
func (c *ConfigurableInterpreter) GetReplicas(object *unstructured.Unstructured) (replicas int32, require *workv1alpha2.ReplicaRequirements, enabled bool, err error) {
	eng, enabled := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationInterpretReplica)
	if !enabled {
		return
	}

	rets, err := eng.Invoke(object)
	if err != nil {
		return
	}

	// retus are: [replicas, requirements]
	replicas, err = rets[0].Int32()
	if err != nil {
		return
	}

	if r := rets[1]; !r.IsNil() {
		require = &workv1alpha2.ReplicaRequirements{}
		err = r.Into(require)
	}
	return
}

// ReviseReplica revises the replica of the given object.
func (c *ConfigurableInterpreter) ReviseReplica(object *unstructured.Unstructured, replica int64) (obj *unstructured.Unstructured, enabled bool, err error) {
	eng, enabled := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationReviseReplica)
	if !enabled {
		return
	}

	rets, err := eng.Invoke(object, replica)
	if err != nil {
		return
	}

	// retus are: [revisedObj]
	obj, err = toUnstructured(rets[0])
	return
}

// Retain returns the objects that based on the "desired" object but with values retained from the "observed" object.
func (c *ConfigurableInterpreter) Retain(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, enabled bool, err error) {
	eng, enabled := c.getInterpreter(desired.GroupVersionKind(), configv1alpha1.InterpreterOperationRetain)
	if !enabled {
		return
	}

	rets, err := eng.Invoke(desired, observed)
	if err != nil {
		return
	}
	// retus are: [retainedObj]
	retained, err = toUnstructured(rets[0])
	return
}

// AggregateStatus returns the objects that based on the 'object' but with status aggregated.
func (c *ConfigurableInterpreter) AggregateStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (obj *unstructured.Unstructured, enabled bool, err error) {
	eng, enabled := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationAggregateStatus)
	if !enabled {
		return
	}

	rets, err := eng.Invoke(object, aggregatedStatusItems)
	if err != nil {
		return
	}

	// retus are: [aggregateObj]
	obj, err = toUnstructured(rets[0])
	return
}

// GetDependencies returns the dependent resources of the given object.
func (c *ConfigurableInterpreter) GetDependencies(object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, enabled bool, err error) {
	eng, enabled := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationInterpretDependency)
	if !enabled {
		return
	}

	rets, err := eng.Invoke(object)
	if err != nil {
		return
	}

	// retus are: [dependencies]
	if r := rets[0]; !r.IsNil() {
		err = r.Into(&dependencies)
	}
	return
}

// ReflectStatus returns the status of the object.
func (c *ConfigurableInterpreter) ReflectStatus(object *unstructured.Unstructured) (status *runtime.RawExtension, enabled bool, err error) {
	eng, enabled := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationInterpretStatus)
	if !enabled {
		return
	}

	rets, err := eng.Invoke(object)
	if err != nil {
		return
	}

	// retus are: [reflectedStatus]
	if r := rets[0]; !r.IsNil() {
		status = &runtime.RawExtension{}
		err = r.Into(status)
	}
	return
}

// InterpretHealth returns the health state of the object.
func (c *ConfigurableInterpreter) InterpretHealth(object *unstructured.Unstructured) (healthy bool, enabled bool, err error) {
	eng, enabled := c.getInterpreter(object.GroupVersionKind(), configv1alpha1.InterpreterOperationInterpretHealth)
	if !enabled {
		return
	}

	rets, err := eng.Invoke(object)
	if err != nil {
		return
	}

	// retus are: [healthy]
	healthy, err = rets[0].Bool()
	return
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
			klog.Errorf("Failed to transform ResourceInterpreterCustomization: %v", err)
			return
		}
		configs[i] = config
	}

	err = c.LoadConfig(configs)
	if err != nil {
		klog.Error(err)
	}
}

// LoadConfig load customization rules.
func (c *ConfigurableInterpreter) LoadConfig(configs []*configv1alpha1.ResourceInterpreterCustomization) error {
	interpreters := map[schema.GroupVersionKind]interpretersByOperations{}
	for _, customization := range configs {
		kind := schema.FromAPIVersionAndKind(customization.Spec.Target.APIVersion, customization.Spec.Target.Kind)
		byOperations, ok := interpreters[kind]
		if !ok {
			byOperations = make(interpretersByOperations)
			interpreters[kind] = byOperations
		}
		if err := c.ruleLoader.Load(customization.Spec.Customizations, byOperations); err != nil {
			return err
		}
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.interpreters = interpreters
	return nil
}

type ruleLoader []struct {
	operation configv1alpha1.InterpreterOperation
	load      func(rulers configv1alpha1.CustomizationRules) (engine.Function, error)
}

func (r ruleLoader) Load(rules configv1alpha1.CustomizationRules, into interpretersByOperations) error {
	for _, rr := range r {
		f, err := rr.load(rules)
		if err != nil {
			return err
		}
		if f != nil {
			into[rr.operation] = f
		}
	}
	return nil
}

var defaultRuleLoader = ruleLoader{
	{
		operation: configv1alpha1.InterpreterOperationRetain,
		load: func(rulers configv1alpha1.CustomizationRules) (engine.Function, error) {
			if rulers.Retention != nil && rulers.Retention.LuaScript != "" {
				return luavm.Load(rulers.Retention.LuaScript, "Retain", 1), nil
			}
			return nil, nil
		},
	},
	{
		operation: configv1alpha1.InterpreterOperationInterpretReplica,
		load: func(rulers configv1alpha1.CustomizationRules) (engine.Function, error) {
			if rulers.ReplicaResource != nil && rulers.ReplicaResource.LuaScript != "" {
				return luavm.Load(rulers.ReplicaResource.LuaScript, "GetReplicas", 2), nil
			}
			return nil, nil
		},
	},
	{
		operation: configv1alpha1.InterpreterOperationReviseReplica,
		load: func(rulers configv1alpha1.CustomizationRules) (engine.Function, error) {
			if rulers.ReplicaRevision != nil && rulers.ReplicaRevision.LuaScript != "" {
				return luavm.Load(rulers.ReplicaRevision.LuaScript, "ReviseReplica", 1), nil
			}
			return nil, nil
		},
	},
	{
		operation: configv1alpha1.InterpreterOperationInterpretStatus,
		load: func(rulers configv1alpha1.CustomizationRules) (engine.Function, error) {
			if rulers.StatusReflection != nil && rulers.StatusReflection.LuaScript != "" {
				return luavm.Load(rulers.StatusReflection.LuaScript, "ReflectStatus", 1), nil
			}
			return nil, nil
		},
	},
	{
		operation: configv1alpha1.InterpreterOperationAggregateStatus,
		load: func(rulers configv1alpha1.CustomizationRules) (engine.Function, error) {
			if rulers.StatusAggregation != nil && rulers.StatusAggregation.LuaScript != "" {
				return luavm.Load(rulers.StatusAggregation.LuaScript, "AggregateStatus", 1), nil
			}
			return nil, nil
		},
	},
	{
		operation: configv1alpha1.InterpreterOperationInterpretHealth,
		load: func(rulers configv1alpha1.CustomizationRules) (engine.Function, error) {
			if rulers.HealthInterpretation != nil && rulers.HealthInterpretation.LuaScript != "" {
				return luavm.Load(rulers.HealthInterpretation.LuaScript, "InterpretHealth", 1), nil
			}
			return nil, nil
		},
	},
	{
		operation: configv1alpha1.InterpreterOperationInterpretDependency,
		load: func(rulers configv1alpha1.CustomizationRules) (engine.Function, error) {
			if rulers.DependencyInterpretation != nil && rulers.DependencyInterpretation.LuaScript != "" {
				return luavm.Load(rulers.DependencyInterpretation.LuaScript, "GetDependencies", 1), nil
			}
			return nil, nil
		},
	},
}

func toUnstructured(v engine.Value) (*unstructured.Unstructured, error) {
	if v.IsNil() {
		return nil, nil
	}
	u := &unstructured.Unstructured{}
	err := v.Into(u)
	return u, err
}
