package lua

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
)

func TestScript(t *testing.T) {
	type args struct {
		script string
		fnName string
		args   []interface{}
	}
	tests := []struct {
		name        string
		args        args
		loadError   bool
		invokeError bool
		want        []func(engine.Value) error
	}{
		{
			name: "load error",
			args: args{
				script: `It's a bad script`,
				args:   nil,
			},
			loadError: true,
		},
		{
			name: "function not found",
			args: args{
				script: "",
				fnName: "demo",
			},
			invokeError: true,
		},
		{
			name: "not a function",
			args: args{
				script: "demo = 1",
				fnName: "demo",
			},
			invokeError: true,
		},
		{
			name: "add",
			args: args{
				script: `
function add(a, b)
	return a + b
end`,
				fnName: "add",
				args:   []interface{}{1, 2},
			},
			want: wants(wantInt32(3)),
		},
		{
			name: "order of returns",
			args: args{
				script: `
function testOrder(a, b, c)
	return a, b, c
end`,
				fnName: "testOrder",
				args:   []interface{}{int32(1), int32(2), int32(3)},
			},
			want: wants(wantInt32(1), wantInt32(2), wantInt32(3)),
		},
		{
			name: "empty to int slice",
			args: args{
				script: `
function empty()
	return {}
end`,
				fnName: "empty",
				args:   []interface{}{},
			},
			want: wants(wantObject(&[]int{}, &[]int{})),
		},
		{
			name: "empty to string slice",
			args: args{
				script: `
function returnEmpty()
	return {}
end`,
				fnName: "returnEmpty",
				args:   []interface{}{},
			},
			want: wants(wantObject(&[]string{}, &[]string{})),
		},
		{
			name: "empty to struct",
			args: args{
				script: `
function returnEmpty()
	return {}
end`,
				fnName: "returnEmpty",
				args:   []interface{}{},
			},
			want: wants(wantObject(&testStruct{}, &testStruct{})),
		},
		{
			name: "want nil",
			args: args{
				script: identityScript,
				fnName: identityFuncName,
				args:   []interface{}{nil},
			},
			want: wants(wantNil),
		},

		{
			name: "want bool",
			args: args{
				script: identityScript,
				fnName: identityFuncName,
				args:   []interface{}{true},
			},
			want: wants(wantBool(true)),
		},
		{
			name: "test struct",
			args: args{
				script: identityScript,
				fnName: identityFuncName,
				args:   []interface{}{makeTestStruct()},
			},
			want: wants(wantTestStruct(makeTestStruct())),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &vm{l: lua.NewState()}
			defer m.l.Close()

			nRets := len(tt.want)
			f, err := load(m, tt.args.script, tt.args.fnName, nRets)
			if (err != nil) != tt.loadError {
				t.Errorf("Load() got error = %v, wantErr = %v", err, tt.loadError)
				return
			}
			if err != nil {
				return
			}

			rets, err := f.Invoke(tt.args.args...)
			if (err != nil) != tt.invokeError {
				t.Errorf("Invoke() got error = %v, wantErr = %v", err, tt.invokeError)
				return
			}
			if err != nil {
				return
			}

			if len(rets) != nRets {
				t.Errorf("got rets %v, want %v", len(rets), nRets)
				return
			}

			for i, assert := range tt.want {
				if err = assert(rets[i]); err != nil {
					t.Errorf("rets[%v] %v", i, err)
				}
			}
		})
	}
}

//
// func Test_value(t *testing.T) {
// 	tests := []struct {
// 		name string
// 		v    value
// 		want func(engine.Value) error
// 	}{
// 		{
// 			name: "nil",
// 			v:    value{v: lua.LNil},
// 			want: wantNil,
// 		},
// 		{
// 			name: "int32",
// 			v:    value{v: lua.LNumber(1)},
// 			want: wantInt32(1),
// 		},
// 		{
// 			name: "bool",
// 			v:    value{v: lua.LBool(true)},
// 			want: wantBool(true),
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			if err := tt.want(tt.v); err != nil {
// 				t.Error(err)
// 			}
// 		})
// 	}
// }

// Utils:

const (
	identityScript = `
function identity(a)
	return a
end`
	identityFuncName = "identity"
)

var (
	testTime = time.Date(1, 2, 3, 4, 5, 6, 0, time.Local)
)

type innerStruct struct {
	A string
}

type testStruct struct {
	Int    int
	Int8   int8
	Int16  int16
	Int32  int32
	Int64  int64
	UInt   uint
	UInt8  uint8
	UInt16 uint16
	UInt32 uint32
	UInt64 uint64

	PInt    *int
	PInt8   *int8
	PInt16  *int16
	PInt32  *int32
	PInt64  *int64
	PUInt   *uint
	PUInt8  *uint8
	PUInt16 *uint16
	PUInt32 *uint32
	PUInt64 *uint64

	String  string
	PString *string
	Bytes   []byte
	Map     map[string]string
	Slice   []string

	Struct  innerStruct
	PStruct *innerStruct

	// types in kubernetes
	UID       types.UID
	MetaTime  metav1.Time
	PMetaTime *metav1.Time
}

func makeTestStruct() *testStruct {
	return &testStruct{
		Int:       1,
		Int8:      1,
		Int16:     1,
		Int32:     1,
		Int64:     1,
		UInt:      1,
		UInt8:     1,
		UInt16:    1,
		UInt32:    1,
		UInt64:    1,
		PInt:      func() *int { x := 1; return &x }(),
		PInt8:     func() *int8 { x := int8(1); return &x }(),
		PInt16:    func() *int16 { x := int16(1); return &x }(),
		PInt32:    func() *int32 { x := int32(1); return &x }(),
		PInt64:    func() *int64 { x := int64(1); return &x }(),
		PUInt:     func() *uint { x := uint(1); return &x }(),
		PUInt8:    func() *uint8 { x := uint8(1); return &x }(),
		PUInt16:   func() *uint16 { x := uint16(1); return &x }(),
		PUInt32:   func() *uint32 { x := uint32(1); return &x }(),
		PUInt64:   func() *uint64 { x := uint64(1); return &x }(),
		String:    "text",
		PString:   pointer.String("text"),
		Bytes:     []byte("text"),
		Map:       map[string]string{"text": "text"},
		Slice:     []string{"text", "text"},
		Struct:    innerStruct{A: "text"},
		PStruct:   &innerStruct{A: "text"},
		UID:       "123",
		MetaTime:  metav1.NewTime(testTime),
		PMetaTime: func() *metav1.Time { x := metav1.NewTime(testTime); return &x }(),
	}
}

func wants(wants ...func(engine.Value) error) []func(engine.Value) error {
	return append([]func(engine.Value) error{}, wants...)
}

func wantNil(v engine.Value) error {
	if !v.IsNil() {
		return fmt.Errorf("got %#v, want nil", v)
	}
	return nil
}

func wantInt32(want int32) func(engine.Value) error {
	return func(v engine.Value) error {
		got, err := v.Int32()
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("got %#v, want %#v", got, want)
		}
		return nil
	}
}

func wantBool(want bool) func(engine.Value) error {
	return func(v engine.Value) error {
		got, err := v.Bool()
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("got %#v, want %#v", got, want)
		}
		return nil
	}
}

func wantTestStruct(want interface{}) func(engine.Value) error {
	return wantObject(&testStruct{}, want)
}

func wantObject(obj interface{}, want interface{}) func(engine.Value) error {
	return func(v engine.Value) error {
		if err := v.Into(obj); err != nil {
			return err
		}
		if got := obj; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("got %#v, want %#v", got, want)
		}
		return nil
	}
}
