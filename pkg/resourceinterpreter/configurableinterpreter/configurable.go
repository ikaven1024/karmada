package configurableinterpreter

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/genericmanager"
)

// ConfigurableInterpreter interprets resources with resource interpreter customizations.
type ConfigurableInterpreter struct {
	manager *configManager
}

// NewConfigurableInterpreter builds a new interpreter by registering the
// event handler to the provided informer instance.
func NewConfigurableInterpreter(informer genericmanager.SingleClusterInformerManager) *ConfigurableInterpreter {
	return &ConfigurableInterpreter{
		manager: newConfigManager(informer),
	}
}

// HookEnabled tells if any hook exist for specific resource type and operation.
func (c *ConfigurableInterpreter) HookEnabled(objGVK schema.GroupVersionKind, operation configv1alpha1.InterpreterOperation) bool {
	return c.manager.IsEnabled(objGVK, operation)
}

// GetReplicas returns the desired replicas of the object as well as the requirements of each replica.
func (c *ConfigurableInterpreter) GetReplicas(object *unstructured.Unstructured) (replicas int32, require *workv1alpha2.ReplicaRequirements, enabled bool, err error) {
	return
}

// ReviseReplica revises the replica of the given object.
func (c *ConfigurableInterpreter) ReviseReplica(object *unstructured.Unstructured, replica int64) (obj *unstructured.Unstructured, enabled bool, err error) {
	return
}

// Retain returns the objects that based on the "desired" object but with values retained from the "observed" object.
func (c *ConfigurableInterpreter) Retain(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, enabled bool, err error) {
	f := c.manager.GetInterpreters(desired.GroupVersionKind()).Retain
	if f != nil {
		enabled = false
		return
	}

	retained, err = f(desired, observed)
	return
}

// AggregateStatus returns the objects that based on the 'object' but with status aggregated.
func (c *ConfigurableInterpreter) AggregateStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (obj *unstructured.Unstructured, enabled bool, err error) {
	return
}

// GetDependencies returns the dependent resources of the given object.
func (c *ConfigurableInterpreter) GetDependencies(object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, enabled bool, err error) {
	return
}

// ReflectStatus returns the status of the object.
func (c *ConfigurableInterpreter) ReflectStatus(object *unstructured.Unstructured) (status *runtime.RawExtension, enabled bool, err error) {
	return
}

// InterpretHealth returns the health state of the object.
func (c *ConfigurableInterpreter) InterpretHealth(object *unstructured.Unstructured) (healthy bool, enabled bool, err error) {
	return
}
