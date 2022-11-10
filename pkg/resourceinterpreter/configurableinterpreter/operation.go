package configurableinterpreter

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine/luavm"
)

type OperationInterprets struct {
	Retain func(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, err error)

	GetReplicas func(object *unstructured.Unstructured) (replicas int32, require *workv1alpha2.ReplicaRequirements, err error)

	ReviseReplica func(object *unstructured.Unstructured, replica int64) (obj *unstructured.Unstructured, err error)

	AggregateStatus func(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (obj *unstructured.Unstructured, err error)

	GetDependencies func(object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, err error)

	ReflectStatus func(object *unstructured.Unstructured) (status *runtime.RawExtension, err error)

	InterpretHealth func(object *unstructured.Unstructured) (healthy bool, err error)
}

func (o *OperationInterprets) IsEnabled(operation configv1alpha1.InterpreterOperation) bool {
	if o == nil {
		return false
	}
	switch operation {
	case configv1alpha1.InterpreterOperationRetain:
		return o.Retain != nil
	default:
		return false
	}
}

func (o *OperationInterprets) LoadConfig(rules configv1alpha1.CustomizationRules) error {
	if r := rules.Retention; r != nil {
		var f engine.Function
		switch {
		case r.LuaScript != "":
			f = luavm.Load(r.LuaScript, "Retain", 1)
			//case r.XxxScript != "":
			//	f = xxx.Load(r.LuaScript)
		}

		if f != nil {
			o.Retain = func(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, err error) {
				rets, err := f.Invoke(desired, observed)
				if err != nil {
					return
				}

				// retus are: [retainedObj]
				retained, err = toUnstructured(rets[0])
				return
			}
		}
	}
	return nil
}

func toUnstructured(v engine.Value) (*unstructured.Unstructured, error) {
	if v.IsNil() {
		return nil, nil
	}
	u := &unstructured.Unstructured{}
	err := v.Into(u)
	return u, err
}
