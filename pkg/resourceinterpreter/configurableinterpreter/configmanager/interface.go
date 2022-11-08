package configmanager

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
)

// InterpreterManager is an executor for a specified kind of resource. It provides LoadConfig interface
// to accept the rules, and expose methods to do the interpreter work.
type InterpreterManager interface {
	LoadConfig(customizations configv1alpha1.CustomizationRules) error

	Interpreter
}

// Interpreter is an executor for a specified kind of resource. Expose methods to interpreting works.
type Interpreter interface {
	// IsEnabled tells if any interpreter exist for specific operation.
	IsEnabled(operation configv1alpha1.InterpreterOperation) bool

	// Retain returns the objects that based on the "desired" object but with values retained from the "observed" object.
	// If not enabled, return enabled with false.
	Retain(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, enabled bool, err error)

	// GetReplicas returns the desired replicas of the object as well as the requirements of each replica.
	// If not enabled, return enabled with false.
	GetReplicas(object *unstructured.Unstructured) (replicas int32, require *workv1alpha2.ReplicaRequirements, enabled bool, err error)

	// ReviseReplica revises the replica of the given object.
	// If not enabled, return enabled with false.
	ReviseReplica(object *unstructured.Unstructured, replica int64) (obj *unstructured.Unstructured, enabled bool, err error)

	// AggregateStatus returns the objects that based on the 'object' but with status aggregated.
	// If not enabled, return enabled with false.
	AggregateStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (obj *unstructured.Unstructured, enabled bool, err error)

	// GetDependencies returns the dependent resources of the given object.
	// If not enabled, return enabled with false.
	GetDependencies(object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, enabled bool, err error)

	// ReflectStatus returns the status of the object.
	// If not enabled, return enabled with false.
	ReflectStatus(object *unstructured.Unstructured) (status *runtime.RawExtension, enabled bool, err error)

	// InterpretHealth returns the health state of the object.
	// If not enabled, return enabled with false.
	InterpretHealth(object *unstructured.Unstructured) (healthy bool, enabled bool, err error)
}
