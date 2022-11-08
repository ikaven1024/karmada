package resourceinterpreter

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
)

type ResourceInterpreter interface {
	// GetReplicas returns the desired replicas of the object as well as the requirements of each replica.
	GetReplicas(ctx context.Context, object *unstructured.Unstructured) (replica int32, replicaRequires *workv1alpha2.ReplicaRequirements, handled bool, err error)

	// ReviseReplica revises the replica of the given object.
	ReviseReplica(ctx context.Context, object *unstructured.Unstructured, replica int64) (*unstructured.Unstructured, bool, error)

	// Retain returns the objects that based on the "desired" object but with values retained from the "observed" object.
	Retain(ctx context.Context, desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, handled bool, err error)

	// AggregateStatus returns the objects that based on the 'object' but with status aggregated.
	AggregateStatus(ctx context.Context, object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, bool, error)

	// GetDependencies returns the dependent resources of the given object.
	GetDependencies(ctx context.Context, object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, handled bool, err error)

	// ReflectStatus returns the status of the object.
	ReflectStatus(ctx context.Context, object *unstructured.Unstructured) (status *runtime.RawExtension, handled bool, err error)

	// InterpretHealth returns the health state of the object.
	InterpretHealth(ctx context.Context, object *unstructured.Unstructured) (healthy bool, handled bool, err error)

	// other common method
}
