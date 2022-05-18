package scheme

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	"github.com/karmada-io/karmada/pkg/printers"
	"github.com/karmada-io/karmada/pkg/printers/storage"
	"github.com/karmada-io/karmada/pkg/util/lifted"
)

// Install installs default resource to scheme
func Install(scheme *Scheme) {
	kinds := []struct {
		kind                     schema.GroupVersionKind
		newFunc                  func() runtime.Object
		columnDefinitions        []metav1.TableColumnDefinition
		printFunc                interface{}
		toSelectableFieldsFunc   interface{}
		fieldLabelConversionFunc runtime.FieldLabelConversionFunc
	}{
		{
			kind:                     corev1.SchemeGroupVersion.WithKind("Pod"),
			newFunc:                  func() runtime.Object { return &corev1.Pod{} },
			columnDefinitions:        lifted.PodColumnDefinitions,
			printFunc:                lifted.PrintPod,
			toSelectableFieldsFunc:   lifted.PodToSelectableFields,
			fieldLabelConversionFunc: lifted.PodFieldLabelConversion,
		},
		{
			kind:              corev1.SchemeGroupVersion.WithKind("Endpoints"),
			newFunc:           func() runtime.Object { return &corev1.Endpoints{} },
			columnDefinitions: lifted.EndpointSliceColumnDefinitions,
			printFunc:         lifted.PrintEndpoints,
		},
		{
			kind:              discoveryv1.SchemeGroupVersion.WithKind("EndpointSlice"),
			newFunc:           func() runtime.Object { return &discoveryv1.EndpointSlice{} },
			columnDefinitions: lifted.EndpointSliceColumnDefinitions,
			printFunc:         lifted.PrintEndpointSlice,
		},
		{
			kind:                     corev1.SchemeGroupVersion.WithKind("Node"),
			newFunc:                  func() runtime.Object { return &corev1.Node{} },
			columnDefinitions:        lifted.NodeColumnDefinitions,
			printFunc:                lifted.PrintNode,
			toSelectableFieldsFunc:   lifted.NodeToSelectableFields,
			fieldLabelConversionFunc: lifted.NodeFieldLabelConversion,
		},
		{
			kind:                     corev1.SchemeGroupVersion.WithKind("Event"),
			newFunc:                  func() runtime.Object { return &corev1.Event{} },
			columnDefinitions:        lifted.EventColumnDefinitions,
			printFunc:                lifted.PrintEvent,
			toSelectableFieldsFunc:   lifted.EventToSelectableFields,
			fieldLabelConversionFunc: lifted.EventFieldLabelConversion,
		},
	}

	for _, k := range kinds {
		if k.printFunc != nil {
			tableGenerator := NewTableGenerator(k.newFunc, k.columnDefinitions, k.printFunc, true)
			scheme.AddTableGenerator(k.kind, tableGenerator)
		}
		if k.toSelectableFieldsFunc != nil {
			selectableFieldFunc := NewSelectableFieldFunc(k.newFunc, k.toSelectableFieldsFunc)
			scheme.AddSelectableFieldFunc(k.kind, selectableFieldFunc)
		}
		if k.fieldLabelConversionFunc != nil {
			scheme.AddFieldLabelConversionFunc(k.kind, k.fieldLabelConversionFunc)
		}
	}
}

// NewSelectableFieldFunc build selectableFieldFunc
func NewSelectableFieldFunc(newFunc func() runtime.Object, selectableFieldFunc interface{}) func(runtime.Object) fields.Set {
	method := reflect.ValueOf(selectableFieldFunc)
	if err := validateSelectableFieldFunc(method); err != nil {
		klog.Exitf("selectableFieldFunc is invalid: %v", err)
	}
	return func(obj runtime.Object) fields.Set {
		if unstructuredObj, ok := obj.(*unstructured.Unstructured); ok {
			obj = newFunc()
			_ = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, obj)
		}

		rets := method.Call([]reflect.Value{reflect.ValueOf(obj)})
		return rets[0].Interface().(fields.Set)
	}
}

// NewTableGenerator build TableGenerator
func NewTableGenerator(newFunc func() runtime.Object, columnDefinitions []metav1.TableColumnDefinition, printFunc interface{}, hasList bool) printers.TableGenerator {
	printMethod := reflect.ValueOf(printFunc)
	if err := printers.ValidateRowPrintHandlerFunc(printMethod); err != nil {
		klog.Exitf("printFunc is invalid: %v", err)
	}

	printer := func(unstructuredObj *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
		// TODO: print from unstructured.Unstructured directly, avoid performance loss due json marshal & unmarshal
		obj := newFunc()
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, obj)
		if err != nil {
			return nil, err
		}

		// printMethod has been checked, so call it safely
		rets := printMethod.Call([]reflect.Value{reflect.ValueOf(obj), reflect.ValueOf(options)})
		if !rets[1].IsNil() {
			return nil, rets[1].Interface().(error)
		}
		return rets[0].Interface().([]metav1.TableRow), nil
	}

	var listPrinter func(list *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error)
	if hasList {
		listPrinter = func(list *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
			rows := make([]metav1.TableRow, 0, len(list.Items))
			for i := range list.Items {
				r, err := printer(&list.Items[i], options)
				if err != nil {
					return nil, err
				}
				rows = append(rows, r...)
			}
			return rows, nil
		}
	}

	return &storage.TableConvertor{
		TableGenerator: printers.NewTableGenerator().With(func(h printers.PrintHandler) {
			_ = h.TableHandler(columnDefinitions, printer)
			if hasList {
				_ = h.TableHandler(columnDefinitions, listPrinter)
			}
		}),
	}
}

func validateSelectableFieldFunc(f reflect.Value) error {
	if f.Kind() != reflect.Func {
		return fmt.Errorf("invalid SelectableFieldFunc. %#v is not a function", f)
	}
	funcType := f.Type()
	if funcType.NumIn() != 1 || funcType.NumOut() != 1 {
		return fmt.Errorf("invalid print handler. Must accept 1 parameters and return 1 value")
	}
	if funcType.Out(0) != reflect.TypeOf((*fields.Set)(nil)).Elem() {
		return fmt.Errorf("invalid print handler. The expected signature is: "+
			"func ToSelectableFields(%v) fields.Set ", funcType.In(0))
	}
	return nil
}
