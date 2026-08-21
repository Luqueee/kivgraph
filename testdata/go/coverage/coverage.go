package coverage

import "fmt"

// Number is a type-set constraint used by the generic container below.
type Number interface {
	~int | ~int64
}

// Box exercises a generic named type and a field reference.
type Box[T any] struct {
	Value T
}

// Identity exercises a generic function and direct calls.
func Identity[T any](value T) T { return value }

// Runner is implemented by implementation through a method.
type Runner interface {
	Run() string
}

type implementation struct{}

func (implementation) Run() string { return Platform }

// Use combines a generic instantiation, an interface implementation and
// direct method/function calls in one deterministic root function.
func Use() string {
	box := Box[int]{Value: Identity(1)}
	var runner Runner = implementation{}
	return fmt.Sprint(box.Value) + runner.Run()
}
