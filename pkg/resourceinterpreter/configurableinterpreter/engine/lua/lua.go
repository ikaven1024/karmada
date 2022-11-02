package lua

import (
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter/engine"
)

func Load(script string) (engine.Engine, error) {
	return &lua{}, nil
}

type lua struct {
}

func (l *lua) Call(args ...interface{}) (rets []interface{}, err error) {
	// TODO implement me
	panic("implement me")
}
