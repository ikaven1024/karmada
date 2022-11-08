package configmanager

import (
	"fmt"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	"github.com/karmada-io/karmada/pkg/util/fedinformer"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/genericmanager"
	"github.com/karmada-io/karmada/pkg/util/helper"
)

var resourceInterpreterCustomizationsGVR = schema.GroupVersionResource{
	Group:    configv1alpha1.GroupVersion.Group,
	Version:  configv1alpha1.GroupVersion.Version,
	Resource: "resourceinterpretercustomizations",
}

// ConfigManager can list custom resource interpreter.
type ConfigManager interface {
	// IsEnabled tells if any interpreter exist for specific resource type and operation.
	IsEnabled(schema.GroupVersionKind, configv1alpha1.InterpreterOperation) bool

	// GetInterpreters returns interpreters for specific resource type.
	GetInterpreters(kind schema.GroupVersionKind) InterpreterManager
}

// interpreterConfigManager collects the resource interpreter customization.
type interpreterConfigManager struct {
	lister        cache.GenericLister
	configuration *atomic.Value
}

// NewInterpreterConfigManager watches ResourceInterpreterCustomization and organizes
// the configurations in the cache.
func NewInterpreterConfigManager(inform genericmanager.SingleClusterInformerManager) ConfigManager {
	m := &interpreterConfigManager{
		lister:        inform.Lister(resourceInterpreterCustomizationsGVR),
		configuration: &atomic.Value{},
	}

	configHandlers := fedinformer.NewHandlerOnEvents(
		func(_ interface{}) { m.updateConfiguration() },
		func(_, _ interface{}) { m.updateConfiguration() },
		func(_ interface{}) { m.updateConfiguration() })
	inform.ForResource(resourceInterpreterCustomizationsGVR, configHandlers)
	return m
}

func (m *interpreterConfigManager) IsEnabled(kind schema.GroupVersionKind, operation configv1alpha1.InterpreterOperation) bool {
	interpreters := m.GetInterpreters(kind)
	return interpreters != nil && interpreters.IsEnabled(operation)
}

func (m *interpreterConfigManager) GetInterpreters(kind schema.GroupVersionKind) InterpreterManager {
	v := m.configuration.Load()
	if v != nil {
		configs := v.(map[schema.GroupVersionKind]InterpreterManager)
		return configs[kind]
	}
	return nil
}

func (m *interpreterConfigManager) updateConfiguration() {
	objs, err := m.lister.List(labels.Everything())
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("error updating configuration: %v", err))
		return
	}

	configs, err := convertToCustomization(objs)
	if err != nil {
		klog.Error(err)
		return
	}

	interpreters, err := loadConfig(configs)
	if err != nil {
		klog.Error(err)
		return
	}

	m.configuration.Store(interpreters)
}

// loadConfig load customization rules.
func loadConfig(configs []*configv1alpha1.ResourceInterpreterCustomization) (map[schema.GroupVersionKind]InterpreterManager, error) {
	allInterpreters := map[schema.GroupVersionKind]InterpreterManager{}
	for _, customization := range configs {
		kind := schema.FromAPIVersionAndKind(customization.Spec.Target.APIVersion, customization.Spec.Target.Kind)

		interprets, ok := allInterpreters[kind]
		if !ok {
			interprets = newInterpreterManager()
			allInterpreters[kind] = interprets
		}

		err := interprets.LoadConfig(customization.Spec.Customizations)
		if err != nil {
			return nil, err
		}
	}

	return allInterpreters, nil
}

func convertToCustomization(objs []runtime.Object) ([]*configv1alpha1.ResourceInterpreterCustomization, error) {
	configs := make([]*configv1alpha1.ResourceInterpreterCustomization, len(objs))
	for i, obj := range objs {
		config := &configv1alpha1.ResourceInterpreterCustomization{}
		if err := helper.ConvertToTypedObject(obj, config); err != nil {
			return nil, fmt.Errorf("failed to transform ResourceInterpreterCustomization: %v", err)
		}
		configs[i] = config
	}
	return configs, nil
}
