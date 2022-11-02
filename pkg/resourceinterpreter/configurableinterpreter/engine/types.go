package engine

// Function known how to call the script
type Function interface {
	// Invoke calls the script with the given args, and return the results.
	Invoke(args ...interface{}) (rets []Value, err error)
}

// FunctionFunc implements Function
type FunctionFunc func(args ...interface{}) (rets []Value, err error)

// Invoke implements Function.Invoke
func (f FunctionFunc) Invoke(args ...interface{}) (rets []Value, err error) {
	return f(args)
}

// Value knows how to convert value to Go type.
type Value interface {
	// IsNil tells if the value is nil
	IsNil() bool

	// Int32 convert value o int32
	Int32() int32

	// Bool convert value to bool
	Bool() bool

	// ConvertTo converts value to the given object. Given object must be a pointer, and can not be nil.
	ConvertTo(interface{})

	// add more converters ...
}
