package configmanager

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
)

// CustomConfiguration provides base information about custom interpreter configuration
type CustomConfiguration interface {
	GetConfigurationName() string
	GetConfigurationTargetGVK() schema.GroupVersionKind
}

// LuaScriptAccessor provides a common interface to get custom interpreter lua script
type LuaScriptAccessor interface {
	CustomConfiguration

	GetRetentionLuaScript() string
	GetReplicaResourceLuaScript() string
	GetReplicaRevisionLuaScript() string
	GetStatusReflectionLuaScript() string
	GetStatusAggregationLuaScript() string
	GetHealthInterpretationLuaScript() string
	GetDependencyInterpretationLuaScript() string
}

// CustomAccessor provides a common interface to get custom interpreter configuration.
type CustomAccessor interface {
	LuaScriptAccessor
}

type resourceCustomAccessor struct {
	retention                *configv1alpha1.LocalValueRetention
	replicaResource          *configv1alpha1.ReplicaResourceRequirement
	replicaRevision          *configv1alpha1.ReplicaRevision
	statusReflection         *configv1alpha1.StatusReflection
	statusAggregation        *configv1alpha1.StatusAggregation
	healthInterpretation     *configv1alpha1.HealthInterpretation
	dependencyInterpretation *configv1alpha1.DependencyInterpretation
	configurationName        string
	configurationTargetGVK   schema.GroupVersionKind
}

// NewResourceCustomAccessorAccessor create an accessor for resource interpreter customization
func NewResourceCustomAccessorAccessor(customization *configv1alpha1.ResourceInterpreterCustomization) CustomAccessor {
	return &resourceCustomAccessor{
		retention:                customization.Spec.Customizations.Retention,
		replicaResource:          customization.Spec.Customizations.ReplicaResource,
		replicaRevision:          customization.Spec.Customizations.ReplicaRevision,
		statusReflection:         customization.Spec.Customizations.StatusReflection,
		statusAggregation:        customization.Spec.Customizations.StatusAggregation,
		healthInterpretation:     customization.Spec.Customizations.HealthInterpretation,
		dependencyInterpretation: customization.Spec.Customizations.DependencyInterpretation,
		configurationName:        customization.Name,
		configurationTargetGVK: schema.GroupVersionKind{
			Version: customization.Spec.Target.APIVersion,
			Kind:    customization.Spec.Target.Kind,
		},
	}
}

func (a *resourceCustomAccessor) GetRetentionLuaScript() string {
	return a.retention.LuaScript
}

func (a *resourceCustomAccessor) GetReplicaResourceLuaScript() string {
	return a.replicaRevision.LuaScript
}

func (a *resourceCustomAccessor) GetReplicaRevisionLuaScript() string {
	return a.replicaResource.LuaScript
}

func (a *resourceCustomAccessor) GetStatusReflectionLuaScript() string {
	return a.statusReflection.LuaScript
}

func (a *resourceCustomAccessor) GetStatusAggregationLuaScript() string {
	return a.statusAggregation.LuaScript
}

func (a *resourceCustomAccessor) GetHealthInterpretationLuaScript() string {
	return a.healthInterpretation.LuaScript
}

func (a *resourceCustomAccessor) GetDependencyInterpretationLuaScript() string {
	return a.healthInterpretation.LuaScript
}

func (a *resourceCustomAccessor) GetConfigurationName() string {
	return a.configurationName
}

func (a *resourceCustomAccessor) GetConfigurationTargetGVK() schema.GroupVersionKind {
	return a.configurationTargetGVK
}
