package configmanager

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine/luavm"
)

// luaScriptInterpreter interprets with given lua script.
type luaScriptInterpreter struct {
	Scripts
}

var _ InterpreterManager = (*luaScriptInterpreter)(nil)

func (l *luaScriptInterpreter) Retain(desired *unstructured.Unstructured, observed *unstructured.Unstructured) (retained *unstructured.Unstructured, enabled bool, err error) {
	if enabled = l.Scripts.Retain != ""; !enabled {
		return
	}

	f := luavm.Load(l.Scripts.Retain, "Retain", 1)
	rets, err := f.Invoke(desired, observed)
	if err != nil {
		return
	}

	// retus are: [retainedObj]
	retained, err = toUnstructured(rets[0])
	return
}

func (l *luaScriptInterpreter) GetReplicas(object *unstructured.Unstructured) (replicas int32, require *workv1alpha2.ReplicaRequirements, enabled bool, err error) {
	if enabled = l.Scripts.GetReplicas != ""; !enabled {
		return
	}

	f := luavm.Load(l.Scripts.GetReplicas, "GetReplicas", 2)
	rets, err := f.Invoke(object)
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

func (l *luaScriptInterpreter) ReviseReplica(object *unstructured.Unstructured, replica int64) (obj *unstructured.Unstructured, enabled bool, err error) {
	if enabled = l.Scripts.ReviseReplica != ""; !enabled {
		return
	}

	f := luavm.Load(l.Scripts.ReviseReplica, "ReviseReplica", 1)
	rets, err := f.Invoke(object, replica)
	if err != nil {
		return
	}

	// retus are: [revisedObj]
	obj, err = toUnstructured(rets[0])
	return
}

func (l *luaScriptInterpreter) AggregateStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (obj *unstructured.Unstructured, enabled bool, err error) {
	if enabled = l.Scripts.AggregateStatus != ""; !enabled {
		return
	}

	f := luavm.Load(l.Scripts.AggregateStatus, "AggregateStatus", 1)
	rets, err := f.Invoke(object, aggregatedStatusItems)
	if err != nil {
		return
	}

	// retus are: [aggregateObj]
	obj, err = toUnstructured(rets[0])
	return
}

func (l *luaScriptInterpreter) GetDependencies(object *unstructured.Unstructured) (dependencies []configv1alpha1.DependentObjectReference, enabled bool, err error) {
	if enabled = l.Scripts.GetDependencies != ""; !enabled {
		return
	}

	f := luavm.Load(l.Scripts.GetDependencies, "GetDependencies", 1)
	rets, err := f.Invoke(object)
	if err != nil {
		return
	}

	// retus are: [dependencies]
	if r := rets[0]; !r.IsNil() {
		err = r.Into(&dependencies)
	}
	return
}

func (l *luaScriptInterpreter) ReflectStatus(object *unstructured.Unstructured) (status *runtime.RawExtension, enabled bool, err error) {
	if enabled = l.Scripts.ReflectStatus != ""; !enabled {
		return
	}

	f := luavm.Load(l.Scripts.ReflectStatus, "ReflectStatus", 1)
	rets, err := f.Invoke(object)
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

func (l *luaScriptInterpreter) InterpretHealth(object *unstructured.Unstructured) (healthy bool, enabled bool, err error) {
	if enabled = l.Scripts.InterpretHealth != ""; !enabled {
		return
	}

	f := luavm.Load(l.Scripts.InterpretHealth, "InterpretHealth", 1)
	rets, err := f.Invoke(object)
	if err != nil {
		return
	}

	// retus are: [healthy]
	healthy, err = rets[0].Bool()
	return
}

// LoadConfig loads interpreting rule from config.
func (l *luaScriptInterpreter) LoadConfig(customizations configv1alpha1.CustomizationRules) error {
	if r := customizations.Retention; r != nil && r.LuaScript != "" {
		l.Scripts.Retain = r.LuaScript
	}
	if r := customizations.ReplicaResource; r != nil && r.LuaScript != "" {
		l.Scripts.GetReplicas = r.LuaScript
	}
	if r := customizations.ReplicaRevision; r != nil && r.LuaScript != "" {
		l.Scripts.ReviseReplica = r.LuaScript
	}
	if r := customizations.StatusAggregation; r != nil && r.LuaScript != "" {
		l.Scripts.AggregateStatus = r.LuaScript
	}
	if r := customizations.DependencyInterpretation; r != nil && r.LuaScript != "" {
		l.Scripts.GetDependencies = r.LuaScript
	}
	if r := customizations.StatusReflection; r != nil && r.LuaScript != "" {
		l.Scripts.ReflectStatus = r.LuaScript
	}
	if r := customizations.HealthInterpretation; r != nil && r.LuaScript != "" {
		l.Scripts.InterpretHealth = r.LuaScript
	}
	return nil
}
