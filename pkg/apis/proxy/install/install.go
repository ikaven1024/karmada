package install

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/karmada-io/karmada/pkg/apis/proxy"
	proxyv1alpha1 "github.com/karmada-io/karmada/pkg/apis/proxy/v1alpha1"
)

// Install registers the API group and adds types to a scheme.
func Install(scheme *runtime.Scheme) {
	utilruntime.Must(proxy.AddToScheme(scheme))
	utilruntime.Must(proxyv1alpha1.AddToScheme(scheme))
	utilruntime.Must(scheme.SetVersionPriority(proxyv1alpha1.SchemeGroupVersion))
}
