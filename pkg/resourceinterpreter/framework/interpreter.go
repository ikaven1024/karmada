package framework

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/customizedinterpreter"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/defaultinterpreter"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/genericmanager"
)

// ResourceInterpreterManager manages both default and customized webhooks to interpret custom resource structure.
type ResourceInterpreterManager interface {
	// Start starts running the component and will never stop running until the context is closed or an error occurs.
	Start(ctx context.Context) (err error)

	resourceinterpreter.ResourceInterpreter
}

// NewResourceInterpreter builds a new ResourceInterpreter object.
func NewResourceInterpreter(informer genericmanager.SingleClusterInformerManager) ResourceInterpreterManager {
	return &customResourceInterpreterImpl{
		informer: informer,
	}
}

type customResourceInterpreterImpl struct {
	informer genericmanager.SingleClusterInformerManager

	interpreters []resourceinterpreter.ResourceInterpreter
}

// Start starts running the component and will never stop running until the context is closed or an error occurs.
func (i *customResourceInterpreterImpl) Start(ctx context.Context) (err error) {
	klog.Infof("Starting custom resource interpreter.")

	interpreterFactories := []func(genericmanager.SingleClusterInformerManager) (resourceinterpreter.ResourceInterpreter, error){
		customizedinterpreter.New,
		defaultinterpreter.New,
	}

	for _, factory := range interpreterFactories {
		interpreter, err := factory(i.informer)
		if err != nil {
			return err
		}
		i.interpreters = append(i.interpreters, interpreter)
	}

	i.informer.Start()
	<-ctx.Done()
	klog.Infof("Stopped as stopCh closed.")
	return nil
}

// GetReplicas returns the desired replicas of the object as well as the requirements of each replica.
func (i *customResourceInterpreterImpl) GetReplicas(ctx context.Context, object *unstructured.Unstructured) (replica int32, requires *workv1alpha2.ReplicaRequirements, handled bool, err error) {
	klog.V(4).Infof("Begin to get replicas for request object: %v %s/%s.", object.GroupVersionKind(), object.GetNamespace(), object.GetName())

	for _, interpreter := range i.interpreters {
		replica, requires, handled, err = interpreter.GetReplicas(ctx, object)
		if err != nil || handled {
			return
		}
	}
	return
}

// ReviseReplica revises the replica of the given object.
func (i *customResourceInterpreterImpl) ReviseReplica(ctx context.Context, object *unstructured.Unstructured, replica int64) (obj *unstructured.Unstructured, handled bool, err error) {
	klog.V(4).Infof("Begin to revise replicas for object: %v %s/%s.", object.GroupVersionKind(), object.GetNamespace(), object.GetName())

	for _, interpreter := range i.interpreters {
		obj, handled, err = interpreter.ReviseReplica(ctx, object, replica)
		if err != nil || handled {
			return
		}
	}
	return
}

// Retain returns the objects that based on the "desired" object but with values retained from the "observed" object.
func (i *customResourceInterpreterImpl) Retain(ctx context.Context, desired *unstructured.Unstructured, observed *unstructured.Unstructured) (obj *unstructured.Unstructured, handled bool, err error) {
	klog.V(4).Infof("Begin to retain object: %v %s/%s.", desired.GroupVersionKind(), desired.GetNamespace(), desired.GetName())

	for _, interpreter := range i.interpreters {
		obj, handled, err = interpreter.Retain(ctx, desired, observed)
		if err != nil || handled {
			return
		}
	}
	return
}

// AggregateStatus returns the objects that based on the 'object' but with status aggregated.
func (i *customResourceInterpreterImpl) AggregateStatus(ctx context.Context, object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (obj *unstructured.Unstructured, handled bool, err error) {
	klog.V(4).Infof("Begin to aggregate status for object: %v %s/%s.", object.GroupVersionKind(), object.GetNamespace(), object.GetName())

	for _, interpreter := range i.interpreters {
		obj, handled, err = interpreter.AggregateStatus(ctx, object, aggregatedStatusItems)
		if err != nil || handled {
			return
		}
	}
	return
}

// GetDependencies returns the dependent resources of the given object.
func (i *customResourceInterpreterImpl) GetDependencies(ctx context.Context, object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, handled bool, err error) {
	klog.V(4).Infof("Begin to get dependencies for object: %v %s/%s.", object.GroupVersionKind(), object.GetNamespace(), object.GetName())

	for _, interpreter := range i.interpreters {
		dependencies, handled, err = interpreter.GetDependencies(ctx, object)
		if err != nil || handled {
			return
		}
	}
	return
}

// ReflectStatus returns the status of the object.
func (i *customResourceInterpreterImpl) ReflectStatus(ctx context.Context, object *unstructured.Unstructured) (status *runtime.RawExtension, handled bool, err error) {
	klog.V(4).Infof("Begin to grab status for object: %v %s/%s.", object.GroupVersionKind(), object.GetNamespace(), object.GetName())

	for _, interpreter := range i.interpreters {
		status, handled, err = interpreter.ReflectStatus(ctx, object)
		if err != nil || handled {
			return
		}
	}
	return
}

// InterpretHealth returns the health state of the object.
func (i *customResourceInterpreterImpl) InterpretHealth(ctx context.Context, object *unstructured.Unstructured) (healthy bool, handled bool, err error) {
	klog.V(4).Infof("Begin to check health for object: %v %s/%s.", object.GroupVersionKind(), object.GetNamespace(), object.GetName())

	for _, interpreter := range i.interpreters {
		healthy, handled, err = interpreter.InterpretHealth(ctx, object)
		if err != nil || handled {
			return
		}
	}
	return
}
