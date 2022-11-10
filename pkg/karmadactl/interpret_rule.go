package karmadactl

import (
	"fmt"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter"
)

type Rules []Rule

func (r Rules) Names() []string {
	names := make([]string, len(r))
	for i, rr := range r {
		names[i] = rr.Name()
	}
	return names
}

func (r Rules) Get(name string) Rule {
	for _, rr := range r {
		if rr.Name() == name {
			return rr
		}
	}
	return nil
}

var allRules = []Rule{
	&retentionRule{},
	&replicaResourceRule{},
	&replicaRevisionRule{},
	&statusReflectionRule{},
	&statusAggregationRule{},
	&healthInterpretationRule{},
	&dependencyInterpretationRule{},
}

type Rule interface {
	Name() string
	GetScript(*configv1alpha1.ResourceInterpreterCustomization) string
	SetScript(*configv1alpha1.ResourceInterpreterCustomization, string)
	Run(*configurableinterpreter.ConfigurableInterpreter, ruleArgs) *ruleResult
}

type retentionRule struct{}

func (r *retentionRule) Name() string {
	return "retention"
}

func (r *retentionRule) GetScript(c *configv1alpha1.ResourceInterpreterCustomization) string {
	if c.Spec.Customizations.Retention != nil {
		return c.Spec.Customizations.Retention.LuaScript
	}
	return ""
}

func (r *retentionRule) SetScript(c *configv1alpha1.ResourceInterpreterCustomization, script string) {
	if script == "" {
		c.Spec.Customizations.Retention = nil
		return
	}

	if c.Spec.Customizations.Retention == nil {
		c.Spec.Customizations.Retention = &configv1alpha1.LocalValueRetention{}
	}
	c.Spec.Customizations.Retention.LuaScript = script
}

func (r *retentionRule) Run(interpreter *configurableinterpreter.ConfigurableInterpreter, args ruleArgs) *ruleResult {
	desired, err := args.getDesiredObjectOrError()
	if err != nil {
		return newRuleResultWithError(err)
	}
	observed, err := args.getObservedObjectOrError()
	if err != nil {
		return newRuleResultWithError(err)
	}
	retained, enabled, err := interpreter.Retain(desired, observed)
	if err != nil {
		return newRuleResultWithError(err)
	}
	if !enabled {
		return newRuleResultWithError(fmt.Errorf("rule is not enabled"))
	}
	return newRuleResult().add("retained", retained)
}

type replicaResourceRule struct {
}

func (r *replicaResourceRule) Name() string {
	return "replicaResource"
}

func (r *replicaResourceRule) GetScript(c *configv1alpha1.ResourceInterpreterCustomization) string {
	if c.Spec.Customizations.ReplicaResource != nil {
		return c.Spec.Customizations.ReplicaResource.LuaScript
	}
	return ""
}

func (r *replicaResourceRule) SetScript(c *configv1alpha1.ResourceInterpreterCustomization, script string) {
	if script == "" {
		c.Spec.Customizations.ReplicaResource = nil
		return
	}

	if c.Spec.Customizations.ReplicaResource == nil {
		c.Spec.Customizations.ReplicaResource = &configv1alpha1.ReplicaResourceRequirement{}
	}
	c.Spec.Customizations.ReplicaResource.LuaScript = script
}

func (r *replicaResourceRule) Run(interpreter *configurableinterpreter.ConfigurableInterpreter, args ruleArgs) *ruleResult {
	obj, err := args.getObjectOrError()
	if err != nil {
		return newRuleResultWithError(err)
	}
	replica, requires, enabled, err := interpreter.GetReplicas(obj)
	if err != nil {
		return newRuleResultWithError(err)
	}
	if !enabled {
		return newRuleResultWithError(fmt.Errorf("rule is not enabled"))
	}
	return newRuleResult().add("replica", replica).add("requires", requires)
}

type replicaRevisionRule struct {
}

func (r *replicaRevisionRule) Name() string {
	return "replicaRevision"
}

func (r replicaRevisionRule) GetScript(c *configv1alpha1.ResourceInterpreterCustomization) string {
	if c.Spec.Customizations.ReplicaRevision != nil {
		return c.Spec.Customizations.ReplicaRevision.LuaScript
	}
	return ""
}

func (r replicaRevisionRule) SetScript(c *configv1alpha1.ResourceInterpreterCustomization, script string) {
	if script == "" {
		c.Spec.Customizations.ReplicaRevision = nil
		return
	}

	if c.Spec.Customizations.ReplicaRevision == nil {
		c.Spec.Customizations.ReplicaRevision = &configv1alpha1.ReplicaRevision{}
	}
	c.Spec.Customizations.ReplicaRevision.LuaScript = script
}

func (r replicaRevisionRule) Run(interpreter *configurableinterpreter.ConfigurableInterpreter, args ruleArgs) *ruleResult {
	obj, err := args.getObjectOrError()
	if err != nil {
		return newRuleResultWithError(err)
	}
	revised, enabled, err := interpreter.ReviseReplica(obj, args.Replica)
	if err != nil {
		return newRuleResultWithError(err)
	}
	if !enabled {
		return newRuleResultWithError(fmt.Errorf("rule is not enabled"))
	}
	return newRuleResult().add("revised", revised)
}

type statusReflectionRule struct {
}

func (s *statusReflectionRule) Name() string {
	return "statusReflection"
}

func (s statusReflectionRule) GetScript(c *configv1alpha1.ResourceInterpreterCustomization) string {
	if c.Spec.Customizations.StatusReflection != nil {
		return c.Spec.Customizations.Retention.LuaScript
	}
	return ""
}

func (s statusReflectionRule) SetScript(c *configv1alpha1.ResourceInterpreterCustomization, script string) {
	if script == "" {
		c.Spec.Customizations.StatusReflection = nil
		return
	}

	if c.Spec.Customizations.StatusReflection == nil {
		c.Spec.Customizations.StatusReflection = &configv1alpha1.StatusReflection{}
	}
	c.Spec.Customizations.StatusReflection.LuaScript = script
}

func (s statusReflectionRule) Run(interpreter *configurableinterpreter.ConfigurableInterpreter, args ruleArgs) *ruleResult {
	obj, err := args.getObjectOrError()
	if err != nil {
		return newRuleResultWithError(err)
	}
	status, enabled, err := interpreter.ReflectStatus(obj)
	if err != nil {
		return newRuleResultWithError(err)
	}
	if !enabled {
		return newRuleResultWithError(fmt.Errorf("rule is not enabled"))
	}
	return newRuleResult().add("status", status)
}

type statusAggregationRule struct {
}

func (s *statusAggregationRule) Name() string {
	return "statusAggregation"
}

func (s statusAggregationRule) GetScript(c *configv1alpha1.ResourceInterpreterCustomization) string {
	if c.Spec.Customizations.StatusAggregation != nil {
		return c.Spec.Customizations.Retention.LuaScript
	}
	return ""
}

func (s statusAggregationRule) SetScript(c *configv1alpha1.ResourceInterpreterCustomization, script string) {
	if script == "" {
		c.Spec.Customizations.StatusAggregation = nil
		return
	}

	if c.Spec.Customizations.StatusAggregation == nil {
		c.Spec.Customizations.StatusAggregation = &configv1alpha1.StatusAggregation{}
	}
	c.Spec.Customizations.StatusAggregation.LuaScript = script
}

func (s statusAggregationRule) Run(interpreter *configurableinterpreter.ConfigurableInterpreter, args ruleArgs) *ruleResult {
	obj, err := args.getObjectOrError()
	if err != nil {
		return newRuleResultWithError(err)
	}
	aggregateStatus, enabled, err := interpreter.AggregateStatus(obj, args.Status)
	if err != nil {
		return newRuleResultWithError(err)
	}
	if !enabled {
		return newRuleResultWithError(fmt.Errorf("rule is not enabled"))
	}
	return newRuleResult().add("aggregateStatus", aggregateStatus)
}

type healthInterpretationRule struct {
}

func (h *healthInterpretationRule) Name() string {
	return "healthInterpretation"
}

func (h healthInterpretationRule) GetScript(c *configv1alpha1.ResourceInterpreterCustomization) string {
	if c.Spec.Customizations.HealthInterpretation != nil {
		return c.Spec.Customizations.Retention.LuaScript
	}
	return ""
}

func (h healthInterpretationRule) SetScript(c *configv1alpha1.ResourceInterpreterCustomization, script string) {
	if script == "" {
		c.Spec.Customizations.HealthInterpretation = nil
		return
	}

	if c.Spec.Customizations.HealthInterpretation == nil {
		c.Spec.Customizations.HealthInterpretation = &configv1alpha1.HealthInterpretation{}
	}
	c.Spec.Customizations.HealthInterpretation.LuaScript = script
}

func (h healthInterpretationRule) Run(interpreter *configurableinterpreter.ConfigurableInterpreter, args ruleArgs) *ruleResult {
	obj, err := args.getObjectOrError()
	if err != nil {
		return newRuleResultWithError(err)
	}
	healthy, enabled, err := interpreter.InterpretHealth(obj)
	if err != nil {
		return newRuleResultWithError(err)
	}
	if !enabled {
		return newRuleResultWithError(fmt.Errorf("rule is not enabled"))
	}
	return newRuleResult().add("healthy", healthy)
}

type dependencyInterpretationRule struct {
}

func (d *dependencyInterpretationRule) Name() string {
	return "dependencyInterpretation"
}

func (d dependencyInterpretationRule) GetScript(c *configv1alpha1.ResourceInterpreterCustomization) string {
	if c.Spec.Customizations.DependencyInterpretation != nil {
		return c.Spec.Customizations.Retention.LuaScript
	}
	return ""
}

func (d dependencyInterpretationRule) SetScript(c *configv1alpha1.ResourceInterpreterCustomization, script string) {
	if script == "" {
		c.Spec.Customizations.DependencyInterpretation = nil
		return
	}

	if c.Spec.Customizations.DependencyInterpretation == nil {
		c.Spec.Customizations.DependencyInterpretation = &configv1alpha1.DependencyInterpretation{}
	}
	c.Spec.Customizations.DependencyInterpretation.LuaScript = script
}

func (d dependencyInterpretationRule) Run(interpreter *configurableinterpreter.ConfigurableInterpreter, args ruleArgs) *ruleResult {
	obj, err := args.getObjectOrError()
	if err != nil {
		return newRuleResultWithError(err)
	}
	dependencies, enabled, err := interpreter.GetDependencies(obj)
	if err != nil {
		return newRuleResultWithError(err)
	}
	if !enabled {
		return newRuleResultWithError(fmt.Errorf("rule is not enabled"))
	}
	return newRuleResult().add("dependencies", dependencies)
}
