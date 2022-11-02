package lua

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
)

// Load loads the script.
func Load(script string) (engine.Function, error) {
	return engine.FunctionFunc(func(args ...interface{}) (rets []engine.Value, err error) {
		return nil, nil
	}), nil
}

type value struct {
}

func (v value) IsNil() bool {
	// TODO implement me
	panic("implement me")
}

var _ engine.Value = (*value)(nil)

func (v value) Int32() int32 {
	// TODO implement me
	panic("implement me")
}

func (v value) Bool() bool {
	// TODO implement me
	panic("implement me")
}

func (v value) Unstructured() *unstructured.Unstructured {
	// TODO implement me
	panic("implement me")
}

func (v value) ConvertTo(i interface{}) {
	// TODO implement me
	panic("implement me")
}
