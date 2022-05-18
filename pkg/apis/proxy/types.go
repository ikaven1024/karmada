package proxy

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	policyv1alpha1 "github.com/karmada-io/karmada/pkg/apis/policy/v1alpha1"
)

//revive:disable:exported

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GlobalResource represents the configuration of the proxy.
// Mainly describes which resources in which clusters should be proxied to member clusters.
type GlobalResource struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	// Spec represents the desired behavior of GlobalResource.
	// +required
	Spec GlobalResourceSpec
	// Status represents the status of GlobalResource.
	// +optional
	Status GlobalResourceStatus
}

// GlobalResourceSpec defines the desired state of GlobalResource.
type GlobalResourceSpec struct {
	// TargetCluster specifies the selected clusters for proxy.
	// If not set, all clusters will be proxied.
	// +optional
	TargetCluster policyv1alpha1.ClusterAffinity

	// ResourceSelectors specifies the resources type that should be proxied to member cluster
	// +required
	ResourceSelectors []ResourceSelector
}

// ResourceSelector specifies the resources type and its scope.
type ResourceSelector struct {
	// APIVersion represents the API version of the target resources.
	// +required
	APIVersion string

	// Kind represents the plural kind of the target resources.
	// +required
	Kind string

	// Namespace of the target resource.
	// Default is empty, which means all namespaces.
	// +optional
	Namespace string

	// Selector filters resources by label.
	// Default is empty, which means no filter.
	// +optional
	Selector *metav1.LabelSelector
}

// GlobalResourceStatus defines the observed state of GlobalResource.
type GlobalResourceStatus struct {
	// Conditions contain the different condition statuses.
	// +optional
	Conditions []metav1.Condition
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GlobalResourceList contains a list of GlobalResource
type GlobalResourceList struct {
	metav1.TypeMeta
	metav1.ListMeta

	// Items holds a list of GlobalResource.
	Items []GlobalResource
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ProxyOptions define a flag for resource proxy that do not have actual resources.
type ProxyOptions struct {
	metav1.TypeMeta

	// Path is the part of URLs used for the current proxy request.
	Path string
}
