package configmanager

import (
	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
)

// Scripts contains configured scripts for interpreting special resource.
type Scripts struct {
	Retain          string
	GetReplicas     string
	ReviseReplica   string
	AggregateStatus string
	GetDependencies string
	ReflectStatus   string
	InterpretHealth string
}

// IsEnabled tells if interpreter is configured for given operation.
func (s Scripts) IsEnabled(operation configv1alpha1.InterpreterOperation) bool {
	switch operation {
	case configv1alpha1.InterpreterOperationRetain:
		return s.Retain != ""
	case configv1alpha1.InterpreterOperationInterpretReplica:
		return s.GetReplicas != ""
	case configv1alpha1.InterpreterOperationReviseReplica:
		return s.ReviseReplica != ""
	case configv1alpha1.InterpreterOperationAggregateStatus:
		return s.AggregateStatus != ""
	case configv1alpha1.InterpreterOperationInterpretDependency:
		return s.GetDependencies != ""
	case configv1alpha1.InterpreterOperationInterpretStatus:
		return s.ReflectStatus != ""
	case configv1alpha1.InterpreterOperationInterpretHealth:
		return s.InterpretHealth != ""
	default:
		return false
	}
}
