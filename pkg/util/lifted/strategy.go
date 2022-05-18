package lifted

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apiserver/pkg/registry/generic"
)

// +lifted:source=https://github.com/kubernetes/kubernetes/blob/release-1.23/pkg/registry/core/pod/strategy.go#L304-L325
// +lifted:changed

// PodToSelectableFields returns a field set that represents the object
// TODO: fields are not labels, and the validation rules for them do not apply.
func PodToSelectableFields(pod *corev1.Pod) fields.Set {
	// The purpose of allocation with a given number of elements is to reduce
	// amount of allocations needed to create the fields.Set. If you add any
	// field here or the number of object-meta related fields changes, this should
	// be adjusted.
	podSpecificFieldsSet := make(fields.Set, 9)
	podSpecificFieldsSet["spec.nodeName"] = pod.Spec.NodeName
	podSpecificFieldsSet["spec.restartPolicy"] = string(pod.Spec.RestartPolicy)
	podSpecificFieldsSet["spec.schedulerName"] = string(pod.Spec.SchedulerName)
	podSpecificFieldsSet["spec.serviceAccountName"] = string(pod.Spec.ServiceAccountName)
	podSpecificFieldsSet["status.phase"] = string(pod.Status.Phase)
	// TODO: add podIPs as a downward API value(s) with proper format
	podIP := ""
	if len(pod.Status.PodIPs) > 0 {
		podIP = string(pod.Status.PodIPs[0].IP)
	}
	podSpecificFieldsSet["status.podIP"] = podIP
	podSpecificFieldsSet["status.nominatedNodeName"] = string(pod.Status.NominatedNodeName)
	return generic.AddObjectMetaFieldsSet(podSpecificFieldsSet, &pod.ObjectMeta, true)
}

// +lifted:source=https://github.com/kubernetes/kubernetes/blob/release-1.23/pkg/registry/core/node/strategy.go#L207-L214
// +lifted:changed

// NodeToSelectableFields returns a field set that represents the object.
func NodeToSelectableFields(node *corev1.Node) fields.Set {
	objectMetaFieldsSet := generic.ObjectMetaFieldsSet(&node.ObjectMeta, false)
	specificFieldsSet := fields.Set{
		"spec.unschedulable": fmt.Sprint(node.Spec.Unschedulable),
	}
	return generic.MergeFieldsSets(objectMetaFieldsSet, specificFieldsSet)
}

// +lifted:source=https://github.com/kubernetes/kubernetes/blob/release-1.23/pkg/registry/core/event/strategy.go#L112-L133
// +lifted:changed

// EventToSelectableFields returns a field set that represents the object.
func EventToSelectableFields(event *corev1.Event) fields.Set {
	objectMetaFieldsSet := generic.ObjectMetaFieldsSet(&event.ObjectMeta, true)
	source := event.Source.Component
	if source == "" {
		source = event.ReportingController
	}
	specificFieldsSet := fields.Set{
		"involvedObject.kind":            event.InvolvedObject.Kind,
		"involvedObject.namespace":       event.InvolvedObject.Namespace,
		"involvedObject.name":            event.InvolvedObject.Name,
		"involvedObject.uid":             string(event.InvolvedObject.UID),
		"involvedObject.apiVersion":      event.InvolvedObject.APIVersion,
		"involvedObject.resourceVersion": event.InvolvedObject.ResourceVersion,
		"involvedObject.fieldPath":       event.InvolvedObject.FieldPath,
		"reason":                         event.Reason,
		"reportingComponent":             event.ReportingController, // use the core/v1 field name
		"source":                         source,
		"type":                           event.Type,
	}
	return generic.MergeFieldsSets(specificFieldsSet, objectMetaFieldsSet)
}

// +lifted:source=https://github.com/kubernetes/kubernetes/blob/release-1.23/pkg/apis/core/v1/conversion.go#L35-L56
// +lifted:changed

// PodFieldLabelConversion convert pod field labels to internal version.
func PodFieldLabelConversion(label, value string) (string, string, error) {
	switch label {
	case "metadata.name",
		"metadata.namespace",
		"spec.nodeName",
		"spec.restartPolicy",
		"spec.schedulerName",
		"spec.serviceAccountName",
		"status.phase",
		"status.podIP",
		"status.podIPs",
		"status.nominatedNodeName":
		return label, value, nil
	// This is for backwards compatibility with old v1 clients which send spec.host
	case "spec.host":
		return "spec.nodeName", value, nil
	default:
		return "", "", fmt.Errorf("field label not supported: %s", label)
	}
}

// +lifted:source=https://github.com/kubernetes/kubernetes/blob/release-1.23/pkg/apis/core/v1/conversion.go#L60-L70
// +lifted:changed

// NodeFieldLabelConversion convert pod field labels to internal version.
func NodeFieldLabelConversion(label, value string) (string, string, error) {
	switch label {
	case "metadata.name":
		return label, value, nil
	case "spec.unschedulable":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("field label not supported: %s", label)
	}
}

// +lifted:source=https://github.com/kubernetes/kubernetes/blob/release-1.23/pkg/apis/core/v1/conversion.go#L426-L448
// +lifted:changed

// EventFieldLabelConversion convert pod field labels to internal version.
func EventFieldLabelConversion(label, value string) (string, string, error) {
	switch label {
	case "involvedObject.kind",
		"involvedObject.namespace",
		"involvedObject.name",
		"involvedObject.uid",
		"involvedObject.apiVersion",
		"involvedObject.resourceVersion",
		"involvedObject.fieldPath",
		"reason",
		"reportingComponent",
		"source",
		"type",
		"metadata.namespace",
		"metadata.name":
		return label, value, nil
	default:
		return "", "", fmt.Errorf("field label not supported: %s", label)
	}
}
