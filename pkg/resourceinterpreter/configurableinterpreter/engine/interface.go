package engine

type Engine interface {
	Call(args ...interface{}) (rets []interface{}, err error)
}
