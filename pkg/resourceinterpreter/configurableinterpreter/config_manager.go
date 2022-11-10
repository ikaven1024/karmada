package configurableinterpreter

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/genericmanager"
	"github.com/karmada-io/karmada/pkg/util/helper"
)

var resourceInterpreterCustomizationsGVR = schema.GroupVersionResource{
	Group:    configv1alpha1.GroupVersion.Group,
	Version:  configv1alpha1.GroupVersion.Version,
	Resource: "resourceinterpretercustomizations",
}

type configManager struct {
	lock sync.RWMutex

	informer     genericmanager.SingleClusterInformerManager
	configLister cache.GenericLister
	interpreters map[schema.GroupVersionKind]OperationInterprets
}

func newConfigManager(informer genericmanager.SingleClusterInformerManager) *configManager {
	c := &configManager{
		configLister: informer.Lister(resourceInterpreterCustomizationsGVR),
	}

	informer.ForResource(resourceInterpreterCustomizationsGVR, cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.updateConfiguration() },
		UpdateFunc: func(_, _ interface{}) { c.updateConfiguration() },
		DeleteFunc: func(_ interface{}) { c.updateConfiguration() },
	})

	return c
}

func (c *configManager) IsEnabled(kind schema.GroupVersionKind, operation configv1alpha1.InterpreterOperation) bool {
	c.lock.RLock()
	defer c.lock.RUnlock()

	interpreters := c.interpreters[kind]
	return interpreters.IsEnabled(operation)
}

func (c *configManager) GetInterpreters(kind schema.GroupVersionKind) OperationInterprets {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.interpreters[kind]
}

func (c *configManager) updateConfiguration() {
	objs, err := c.configLister.List(labels.Everything())
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

	c.lock.Lock()
	defer c.lock.Unlock()
	c.interpreters = interpreters
}

// loadConfig load customization rules.
func loadConfig(configs []*configv1alpha1.ResourceInterpreterCustomization) (map[schema.GroupVersionKind]OperationInterprets, error) {
	allInterpreters := map[schema.GroupVersionKind]OperationInterprets{}
	for _, customization := range configs {
		kind := schema.FromAPIVersionAndKind(customization.Spec.Target.APIVersion, customization.Spec.Target.Kind)

		interprets := allInterpreters[kind]
		err := interprets.LoadConfig(customization.Spec.Customizations)
		if err != nil {
			return nil, err
		}
		allInterpreters[kind] = interprets
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
