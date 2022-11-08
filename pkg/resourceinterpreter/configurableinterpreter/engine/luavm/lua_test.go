package luavm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	lua "github.com/yuin/gopher-lua"
	luajson "layeh.com/gopher-json"

	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
)

func Test_runScript(t *testing.T) {
	type args struct {
		script string
		fnName string
		args   []interface{}
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		want    []func(engine.Value) error
	}{
		{
			name: "load error",
			args: args{
				script: `It's a bad script`,
				args:   nil,
			},
			wantErr: true,
		},
		{
			name: "function not found",
			args: args{
				script: "",
				fnName: "demo",
			},
			wantErr: true,
		},
		{
			name: "not a function",
			args: args{
				script: "demo = 1",
				fnName: "demo",
			},
			wantErr: true,
		},
		{
			name: "runtime error",
			args: args{
				script: `
function test()
x = {}
x.a.b = 1
end
`,
				fnName: "test",
				args:   nil,
			},
			wantErr: true,
		},
		{
			name: "test order of returns",
			args: args{
				script: `
function testOrder(a, b, c)
	return a, b, c
end`,
				fnName: "testOrder",
				args:   []interface{}{nil, true, int32(1)},
			},
			want: wants(wantNil, wantBool(true), wantInt32(1)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := makeTestVM()
			defer m.Close()

			nRets := len(tt.want)
			rets, err := runScript(m, tt.args.script, tt.args.fnName, nRets, tt.args.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("runScript() got error = %v, wantErr = %v", err, tt.wantErr)
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

func Test_toValue(t *testing.T) {
	l := lua.NewState()

	type args struct {
		obj interface{}
	}
	tests := []struct {
		name    string
		args    args
		want    lua.LValue
		wantErr bool
	}{
		{
			name: "nil",
			args: args{
				obj: nil,
			},
			want:    lua.LNil,
			wantErr: false,
		},
		{
			name: "[]interface",
			args: args{
				obj: []interface{}{
					nil, true, float32(1), float64(1), "1", json.Number("1"),
					1, int8(1), int16(1), int32(1), int64(1),
					uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
					func() *int { x := 1; return &x }(),
					func() *int8 { x := int8(1); return &x }(),
					func() *int16 { x := int16(1); return &x }(),
					func() *int32 { x := int32(1); return &x }(),
					func() *int64 { x := int64(1); return &x }(),
					func() *int { x := 1; return &x }(),
					func() *uint8 { x := uint8(1); return &x }(),
					func() *uint16 { x := uint16(1); return &x }(),
					func() *uint32 { x := uint32(1); return &x }(),
					func() *uint64 { x := uint64(1); return &x }(),
				},
			},
			want: func() lua.LValue {
				v := &lua.LTable{}
				v.Append(lua.LNil)
				v.Append(lua.LBool(true))
				v.Append(lua.LNumber(1))   // float32
				v.Append(lua.LNumber(1))   // float64
				v.Append(lua.LString("1")) // string
				v.Append(lua.LString("1")) // json.Number
				// int -> int64
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				// uint -> uint64
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				// *int -> *int64
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				// *uint -> *uint64
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				v.Append(lua.LNumber(1))
				return v
			}(),
			wantErr: false,
		},
		{
			name: "[]map[string]interface{}",
			args: args{
				obj: []map[string]interface{}{
					{
						"foo": 1,
					},
				},
			},
			want: func() lua.LValue {
				m := &lua.LTable{}
				m.RawSetH(lua.LString("foo"), lua.LNumber(1))

				v := &lua.LTable{}
				v.Append(m)
				return v
			}(),
			wantErr: false,
		},
		{
			name: "struct",
			args: args{
				obj: func() interface{} {
					return &struct {
						Foo string
					}{
						Foo: "foo",
					}
				}(),
			},
			want: func() lua.LValue {
				v := &lua.LTable{}
				v.RawSetH(lua.LString("Foo"), lua.LString("foo"))
				return v
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := toValue(l, tt.args.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("toValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			got, err := luajson.Encode(v)
			if err != nil {
				t.Fatal(err)
			}
			want, err := luajson.Encode(tt.want)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, want) {
				t.Errorf("toValue() got = %s, want %s", got, want)
			}
		})
	}
}

func Test_value_Into(t *testing.T) {
	type fields struct {
		v lua.LValue
	}
	type args struct {
		obj interface{}
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "not a pointer",
			fields: fields{
				v: &lua.LTable{},
			},
			args: args{
				obj: struct{}{},
			},
			wantErr: true,
		},
		{
			name: "empty table into struct",
			fields: fields{
				v: func() lua.LValue {
					v := &lua.LTable{}
					return v
				}(),
			},
			args: args{
				obj: &struct {
					Foo string
					Bar int
				}{},
			},
			wantErr: false,
		},
		{
			name: "empty table into slice",
			fields: fields{
				v: func() lua.LValue {
					v := &lua.LTable{}
					return v
				}(),
			},
			args: args{
				obj: &[]int{},
			},
			wantErr: false,
		},
		// TODO:
		// uncomment this case after fix: json: cannot unmarshal array into Go struct field
		//{
		//	name: "table has empty table into slice",
		//	fields: fields{
		//		v: func() lua.LValue {
		//			v := &lua.LTable{}
		//			v.RawSetH(lua.LString("Foo"), &lua.LTable{})
		//			return v
		//		}(),
		//	},
		//	args: args{
		//		obj: &struct {
		//			Foo struct{}
		//		}{},
		//	},
		//	wantErr: false,
		//},
		{
			name: "struct is not empty, and convert successfully",
			fields: fields{
				v: func() lua.LValue {
					v := &lua.LTable{}
					v.RawSetH(lua.LString("Foo"), lua.LString("foo"))
					v.RawSetH(lua.LString("Bar"), lua.LNumber(1))
					return v
				}(),
			},
			args: args{
				obj: &struct {
					Foo string
					Bar int
				}{
					Foo: "foo",
					Bar: 1,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := value{
				v: tt.fields.v,
			}
			if err := v.Into(tt.args.obj); (err != nil) != tt.wantErr {
				t.Errorf("Into() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Utils:

func makeTestVM() *lua.LState {
	vm, err := New(false)
	if err != nil {
		panic(err)
	}
	return vm
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
