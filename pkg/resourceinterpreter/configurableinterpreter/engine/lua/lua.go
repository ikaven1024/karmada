package lua

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	lua "github.com/yuin/gopher-lua"
	"k8s.io/apimachinery/pkg/conversion"
	luajson "layeh.com/gopher-json"

	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
)

type vm struct {
	lock sync.Mutex
	l    *lua.LState
}

var eng = &vm{l: lua.NewState()}

// Load loads the script.
func Load(script string, fnName string, nRets int) (engine.Function, error) {
	return load(eng, script, fnName, nRets)
}

func load(vm *vm, script string, fnName string, nRets int) (engine.Function, error) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	err := vm.l.DoString(script)
	if err != nil {
		return nil, err
	}

	return engine.FunctionFunc(func(args ...interface{}) (rets []engine.Value, err error) {
		vm.lock.Lock()
		defer vm.lock.Unlock()

		vArgs := make([]lua.LValue, len(args))
		for i, arg := range args {
			vArgs[i], err = toValue(vm.l, arg)
			if err != nil {
				return nil, err
			}
		}

		f := vm.l.GetGlobal(fnName)
		if f.Type() == lua.LTNil {
			return nil, fmt.Errorf("not found function %v", fnName)
		}
		if f.Type() != lua.LTFunction {
			return nil, fmt.Errorf("not a function: %v", f.Type())
		}

		err = vm.l.CallByParam(lua.P{
			Fn:      f,
			NRet:    nRets,
			Protect: true,
		}, vArgs...)
		if err != nil {
			return nil, err
		}

		// get rets from stack: [ret1, ret2, ret3 ...]
		rets = make([]engine.Value, nRets)
		for i := range rets {
			rets[i] = value{v: vm.l.Get(i + 1)}
		}
		vm.l.Pop(nRets)
		return
	}), nil
}

type value struct {
	v lua.LValue
}

var _ engine.Value = (*value)(nil)

func (v value) IsNil() bool {
	return v.v.Type() == lua.LTNil
}

func (v value) Int32() (int32, error) {
	if v.v.Type() != lua.LTNumber {
		return 0, fmt.Errorf("%#v is not number", v.v)
	}
	return int32(v.v.(lua.LNumber)), nil
}

func (v value) Bool() (bool, error) {
	if v.v.Type() != lua.LTBool {
		return false, fmt.Errorf("%#v is not bool", v.v)
	}
	return bool(v.v.(lua.LBool)), nil
}

func (v value) Into(obj interface{}) error {
	t, err := conversion.EnforcePtr(obj)
	if err != nil {
		return fmt.Errorf("%#v is not pointer", v.v)
	}

	data, err := luajson.Encode(v.v)
	if err != nil {
		return fmt.Errorf("luajson Encode obj %#v error: %v", v.v, err)
	}

	// In lua, `{}` indicates array and table. Luajson encodes it to `[]`.
	// So before converting to struct, convert it to '{}'.
	if t.Kind() == reflect.Struct && len(data) > 1 && data[0] == '[' {
		data[0], data[len(data)-1] = '{', '}'
	}

	err = json.Unmarshal(data, obj)
	if err != nil {
		return fmt.Errorf("can not unmarshal %v to %#v", string(data), obj)
	}
	return nil
}

func toValue(l *lua.LState, obj interface{}) (lua.LValue, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("json Marshal obj %#v error: %v", obj, err)
	}

	v, err := luajson.Decode(l, data)
	if err != nil {
		return nil, fmt.Errorf("lua Decode obj %#v error: %v", obj, err)
	}
	return v, nil
}
