package defaultinterpreter

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/genericmanager"
)

// DefaultInterpreter contains all default operation interpreter factory
// for interpreting common resource.
type DefaultInterpreter struct {
	replicaHandlers         map[schema.GroupVersionKind]replicaInterpreter
	reviseReplicaHandlers   map[schema.GroupVersionKind]reviseReplicaInterpreter
	retentionHandlers       map[schema.GroupVersionKind]retentionInterpreter
	aggregateStatusHandlers map[schema.GroupVersionKind]aggregateStatusInterpreter
	dependenciesHandlers    map[schema.GroupVersionKind]dependenciesInterpreter
	reflectStatusHandlers   map[schema.GroupVersionKind]reflectStatusInterpreter
	healthHandlers          map[schema.GroupVersionKind]healthInterpreter
}

// New return a new DefaultInterpreter.
func New(_ genericmanager.SingleClusterInformerManager) (resourceinterpreter.ResourceInterpreter, error) {
	return &DefaultInterpreter{
		replicaHandlers:         getAllDefaultReplicaInterpreter(),
		reviseReplicaHandlers:   getAllDefaultReviseReplicaInterpreter(),
		retentionHandlers:       getAllDefaultRetentionInterpreter(),
		aggregateStatusHandlers: getAllDefaultAggregateStatusInterpreter(),
		dependenciesHandlers:    getAllDefaultDependenciesInterpreter(),
		reflectStatusHandlers:   getAllDefaultReflectStatusInterpreter(),
		healthHandlers:          getAllDefaultHealthInterpreter(),
	}, nil
}

// HookEnabled tells if any hook exist for specific resource type and operation type.
func (e *DefaultInterpreter) HookEnabled(kind schema.GroupVersionKind, operationType configv1alpha1.InterpreterOperation) bool {
	switch operationType {
	case configv1alpha1.InterpreterOperationInterpretReplica:
		if _, exist := e.replicaHandlers[kind]; exist {
			return true
		}
	case configv1alpha1.InterpreterOperationReviseReplica:
		if _, exist := e.reviseReplicaHandlers[kind]; exist {
			return true
		}
	case configv1alpha1.InterpreterOperationRetain:
		if _, exist := e.retentionHandlers[kind]; exist {
			return true
		}
	case configv1alpha1.InterpreterOperationAggregateStatus:
		if _, exist := e.aggregateStatusHandlers[kind]; exist {
			return true
		}
	case configv1alpha1.InterpreterOperationInterpretDependency:
		if _, exist := e.dependenciesHandlers[kind]; exist {
			return true
		}
	case configv1alpha1.InterpreterOperationInterpretStatus:
		return true
	case configv1alpha1.InterpreterOperationInterpretHealth:
		if _, exist := e.healthHandlers[kind]; exist {
			return true
		}
		// TODO(RainbowMango): more cases should be added here
	}

	klog.V(4).Infof("Default interpreter is not enabled for kind %q with operation %q.", kind, operationType)
	return false
}

// GetReplicas returns the desired replicas of the object as well as the requirements of each replica.
func (e *DefaultInterpreter) GetReplicas(_ context.Context, object *unstructured.Unstructured) (int32, *workv1alpha2.ReplicaRequirements, bool, error) {
	handler, exist := e.replicaHandlers[object.GroupVersionKind()]
	if !exist {
		return 0, nil, false, nil
	}
	replicas, requirements, err := handler(object)
	return replicas, requirements, true, err
}

// ReviseReplica revises the replica of the given object.
func (e *DefaultInterpreter) ReviseReplica(_ context.Context, object *unstructured.Unstructured, replica int64) (*unstructured.Unstructured, bool, error) {
	handler, exist := e.reviseReplicaHandlers[object.GroupVersionKind()]
	if !exist {
		return nil, false, nil
	}
	obj, err := handler(object, replica)
	return obj, true, err
}

// Retain returns the objects that based on the "desired" object but with values retained from the "observed" object.
func (e *DefaultInterpreter) Retain(_ context.Context, desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, handled bool, err error) {
	handler, exist := e.retentionHandlers[desired.GroupVersionKind()]
	if !exist {
		return nil, false, nil
	}
	handled = true
	retained, err = handler(desired, observed)
	return
}

// AggregateStatus returns the objects that based on the 'object' but with status aggregated.
func (e *DefaultInterpreter) AggregateStatus(_ context.Context, object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, bool, error) {
	handler, exist := e.aggregateStatusHandlers[object.GroupVersionKind()]
	if !exist {
		return nil, false, nil
	}
	obj, err := handler(object, aggregatedStatusItems)
	return obj, true, err
}

// GetDependencies returns the dependent resources of the given object.
func (e *DefaultInterpreter) GetDependencies(_ context.Context, object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, handled bool, err error) {
	handler, exist := e.dependenciesHandlers[object.GroupVersionKind()]
	if !exist {
		return nil, false, nil
	}
	handled = true
	dependencies, err = handler(object)
	return
}

// ReflectStatus returns the status of the object.
func (e *DefaultInterpreter) ReflectStatus(_ context.Context, object *unstructured.Unstructured) (status *runtime.RawExtension, handled bool, err error) {
	handler, exist := e.reflectStatusHandlers[object.GroupVersionKind()]
	if exist {
		status, err = handler(object)
		return status, true, err
	}

	// for resource types that don't have a build-in handler, try to collect the whole status from '.status' filed.
	status, err = reflectWholeStatus(object)
	return status, true, err
}

// InterpretHealth returns the health state of the object.
func (e *DefaultInterpreter) InterpretHealth(_ context.Context, object *unstructured.Unstructured) (bool, bool, error) {
	handler, exist := e.healthHandlers[object.GroupVersionKind()]
	if !exist {
		return false, false, nil
	}

	healthy, err := handler(object)
	return healthy, true, err
}
