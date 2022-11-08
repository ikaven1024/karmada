package configmanager

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
)

func newInterpreterManager() InterpreterManager {
	return interpreterManagers{
		interpreters: []InterpreterManager{
			&luaScriptInterpreter{},
		},
	}
}

// interpreterManagers containers multiple interpreters for specified resource.
// It will find the enabled interpreters to do interpreting work.
type interpreterManagers struct {
	interpreters []InterpreterManager
}

var _ Interpreter = (*interpreterManagers)(nil)

func (i interpreterManagers) LoadConfig(customizations configv1alpha1.CustomizationRules) error {
	for _, interpreter := range i.interpreters {
		err := interpreter.LoadConfig(customizations)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i interpreterManagers) IsEnabled(operation configv1alpha1.InterpreterOperation) bool {
	for _, interpreter := range i.interpreters {
		if interpreter.IsEnabled(operation) {
			return true
		}
	}
	return false
}

func (i interpreterManagers) Retain(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, handled bool, err error) {
	for _, interpreter := range i.interpreters {
		retained, handled, err = interpreter.Retain(desired, observed)
		if err != nil || handled {
			return
		}
	}
	return
}

func (i interpreterManagers) GetReplicas(object *unstructured.Unstructured) (replicas int32, require *workv1alpha2.ReplicaRequirements, handled bool, err error) {
	for _, interpreter := range i.interpreters {
		replicas, require, handled, err = interpreter.GetReplicas(object)
		if err != nil || handled {
			return
		}
	}
	return
}

func (i interpreterManagers) ReviseReplica(object *unstructured.Unstructured, replica int64) (obj *unstructured.Unstructured, handled bool, err error) {
	for _, interpreter := range i.interpreters {
		obj, handled, err = interpreter.ReviseReplica(object, replica)
		if err != nil || handled {
			return
		}
	}
	return
}

func (i interpreterManagers) AggregateStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (obj *unstructured.Unstructured, handled bool, err error) {
	for _, interpreter := range i.interpreters {
		obj, handled, err = interpreter.AggregateStatus(object, aggregatedStatusItems)
		if err != nil || handled {
			return
		}
	}
	return
}

func (i interpreterManagers) GetDependencies(object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, handled bool, err error) {
	for _, interpreter := range i.interpreters {
		dependencies, handled, err = interpreter.GetDependencies(object)
		if err != nil || handled {
			return
		}
	}
	return
}

func (i interpreterManagers) ReflectStatus(object *unstructured.Unstructured) (status *runtime.RawExtension, handled bool, err error) {
	for _, interpreter := range i.interpreters {
		status, handled, err = interpreter.ReflectStatus(object)
		if err != nil || handled {
			return
		}
	}
	return
}

func (i interpreterManagers) InterpretHealth(object *unstructured.Unstructured) (healthy bool, handled bool, err error) {
	for _, interpreter := range i.interpreters {
		healthy, handled, err = interpreter.InterpretHealth(object)
		if err != nil || handled {
			return
		}
	}
	return
}

func toUnstructured(v engine.Value) (*unstructured.Unstructured, error) {
	if v.IsNil() {
		return nil, fmt.Errorf("script returns nil value")
	}
	u := &unstructured.Unstructured{}
	err := v.Into(u)
	return u, err
}
