package luavm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	lua "github.com/yuin/gopher-lua"
	"k8s.io/apimachinery/pkg/conversion"
	luajson "layeh.com/gopher-json"

	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
	"github.com/karmada-io/karmada/pkg/util/fixedpool"
	"github.com/karmada-io/karmada/pkg/util/lifted"
)

// TODO: is the size suitable？
const poolSize = 10

var vmPool = fixedpool.New(
	func() (any, error) { return New(false) },
	func(a any) { a.(*lua.LState).Close() },
	poolSize)

// Load calls VM.Load.
func Load(script string, fnName string, nRets int) engine.Function {
	return engine.FunctionFunc(func(args ...interface{}) (rets []engine.Value, err error) {
		o, err := vmPool.Get()
		if err != nil {
			return nil, err
		}
		defer vmPool.Put(o)

		ctx, cancel := context.WithTimeout(context.TODO(), time.Second)
		defer cancel()

		vm := o.(*lua.LState)
		initVM(ctx, vm)
		return runScript(vm, script, fnName, nRets, args)
	})
}

// New create a lua vm.
// Set parameter useOpenLibs true to enable open libraries. Libraries are disabled by default while running, but enabled during testing to allow the use of print statements.
func New(useOpenLibs bool) (*lua.LState, error) {
	l := lua.NewState(lua.Options{
		SkipOpenLibs: !useOpenLibs,
	})
	// Opens table library to allow access to functions to manipulate tables
	err := setLib(l)
	if err != nil {
		return nil, err
	}
	// preload our 'safe' version of the OS library. Allows the 'local os = require("os")' to work
	l.PreloadModule(lua.OsLibName, lifted.SafeOsLoader)

	return l, err
}

func runScript(l *lua.LState, script string, fnName string, nRets int, args []interface{}) ([]engine.Value, error) {
	err := l.DoString(script)
	if err != nil {
		return nil, err
	}

	vArgs := make([]lua.LValue, len(args))
	for i, arg := range args {
		vArgs[i], err = toValue(l, arg)
		if err != nil {
			return nil, err
		}
	}

	f := l.GetGlobal(fnName)
	if f.Type() == lua.LTNil {
		return nil, fmt.Errorf("not found function %v", fnName)
	}
	if f.Type() != lua.LTFunction {
		return nil, fmt.Errorf("%s is not a function: %s", fnName, f.Type())
	}

	err = l.CallByParam(lua.P{
		Fn:      f,
		NRet:    nRets,
		Protect: true,
	}, vArgs...)
	if err != nil {
		return nil, err
	}

	// get rets from stack: [ret1, ret2, ret3 ...]
	rets := make([]engine.Value, nRets)
	for i := range rets {
		rets[i] = value{v: l.Get(i + 1)}
	}
	// pop all the values in stack
	l.Pop(l.GetTop())
	return rets, nil
}

func setLib(l *lua.LState) error {
	for _, pair := range []struct {
		n string
		f lua.LGFunction
	}{
		{lua.LoadLibName, lua.OpenPackage},
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		// load our 'safe' version of the OS library
		{lua.OsLibName, lifted.OpenSafeOs},
	} {
		if err := l.CallByParam(lua.P{
			Fn:      l.NewFunction(pair.f),
			NRet:    0,
			Protect: true,
		}, lua.LString(pair.n)); err != nil {
			return err
		}
	}
	return nil
}

func initVM(ctx context.Context, vm *lua.LState) {
	vm.Pop(vm.GetTop())
	vm.SetContext(ctx)
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

	// For example, `GetReplicas` returns requirement with empty:
	//     {
	//         nodeClaim: {},
	//         resourceRequest: {
	//             cpu: "100m"
	//         }
	//     }
	// Luajson encodes it to
	//     {"nodeClaim": [], "resourceRequest": {"cpu": "100m"}}
	//
	// While go json fails to unmarshal `[]` to ReplicaRequirements.NodeClaim object.
	// ReplicaRequirements object.
	//
	// Here we handle it as follows:
	//   1. Walk the object (lua table), delete the key with empty value (`nodeClaim` in this example):
	//     {
	//         resourceRequest: {
	//             cpu: "100m"
	//         }
	//     }
	//   2. Encode the object with luajson to be:
	//     {"resourceRequest": {"cpu": "100m"}}
	//   4. Finally, unmarshal the new json to object, get
	//     {
	//         resourceRequest: {
	//             cpu: "100m"
	//         }
	//     }

	isEmptyDic := func(v *lua.LTable) bool {
		count := 0
		v.ForEach(func(lua.LValue, lua.LValue) {
			count++
		})
		return count == 0
	}

	var walk func(v lua.LValue)
	walk = func(v lua.LValue) {
		if t, ok := v.(*lua.LTable); ok {
			t.ForEach(func(key lua.LValue, value lua.LValue) {
				if tt, ok := value.(*lua.LTable); ok {
					if isEmptyDic(tt) {
						// set nil to delete key
						t.RawSetH(key, lua.LNil)
					} else {
						walk(value)
					}
				}
			})
		}
	}

	walk(v.v)

	data, err := luajson.Encode(v.v)
	if err != nil {
		return fmt.Errorf("luajson Encode obj %#v error: %v", v.v, err)
	}

	//  If the josn is a table, and we want a object. Convert `[...]` to `{...}`
	if t.Kind() == reflect.Struct && len(data) > 1 && data[0] == '[' {
		data[0], data[len(data)-1] = '{', '}'
	}

	err = json.Unmarshal(data, obj)
	if err != nil {
		return fmt.Errorf("can not unmarshal %s to %#v: %v", data, obj, err)
	}
	return nil
}

// Took logic from the link below and added the int, int32, int64, default types since the value would have type int64
// while actually running in the controller, and it was not reproducible through testing.
// https://github.com/layeh/gopher-json/blob/97fed8db84274c421dbfffbb28ec859901556b97/json.go#L154
//nolint:gocyclo
func toValue(l *lua.LState, obj interface{}) (lua.LValue, error) {
	switch o := obj.(type) {
	case bool:
		return lua.LBool(o), nil
	case float64:
		return lua.LNumber(o), nil
	case string:
		return lua.LString(o), nil
	case json.Number:
		return lua.LString(o), nil
	case int:
		return lua.LNumber(o), nil
	case int32:
		return lua.LNumber(o), nil
	case int64:
		return lua.LNumber(o), nil
	case []interface{}:
		arr := l.CreateTable(len(o), 0)
		for _, item := range o {
			v, err := toValue(l, item)
			if err != nil {
				return nil, err
			}
			arr.Append(v)
		}
		return arr, nil
	case []map[string]interface{}:
		arr := l.CreateTable(len(o), 0)
		for _, item := range o {
			v, err := toValue(l, item)
			if err != nil {
				return nil, err
			}
			arr.Append(v)
		}
		return arr, nil
	case map[string]interface{}:
		tbl := l.CreateTable(0, len(o))
		for key, item := range o {
			v, err := toValue(l, item)
			if err != nil {
				return nil, err
			}
			tbl.RawSetString(key, v)
		}
		return tbl, nil
	case nil:
		return lua.LNil, nil
	default:
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
}
