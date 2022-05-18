package proxy

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1alpha1 "github.com/karmada-io/karmada/pkg/apis/cluster/v1alpha1"
	proxyapis "github.com/karmada-io/karmada/pkg/apis/proxy"
	proxyv1alpha1 "github.com/karmada-io/karmada/pkg/apis/proxy/v1alpha1"
	"github.com/karmada-io/karmada/pkg/proxy/cache"
	"github.com/karmada-io/karmada/pkg/proxy/scheme"
	"github.com/karmada-io/karmada/pkg/util"
	"github.com/karmada-io/karmada/pkg/util/lifted"
)

// Controller syncs Cluster and GlobalResource.
type Controller struct {
	lock sync.Mutex
	mgr  controllerruntime.Manager

	// proxy
	clusterProxy *clusterProxy
	karmadaProxy *karmadaProxy
	localStorage *LocalStorage

	// globalResource name string => MultiClusterCache
	caches     sync.Map
	restMapper meta.RESTMapper
	scheme     *scheme.Scheme
}

// NewController create a controller for proxy
func NewController(mgr controllerruntime.Manager, scheme *scheme.Scheme, minRequestTimeout time.Duration) (*Controller, error) {
	kp, err := newKarmadaProxy(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	ctl := &Controller{
		karmadaProxy: kp,
		clusterProxy: newClusterProxy(mgr.GetCache()),
		localStorage: newLocalStorage(scheme, minRequestTimeout),
		mgr:          mgr,
		restMapper:   mgr.GetRESTMapper(),
		scheme:       scheme,
	}

	err = controllerruntime.NewControllerManagedBy(ctl.mgr).For(&clusterv1alpha1.Cluster{}).Complete(ctl)
	if err != nil {
		return nil, err
	}
	err = controllerruntime.NewControllerManagedBy(ctl.mgr).For(&proxyv1alpha1.GlobalResource{}).Complete(ctl)
	if err != nil {
		return nil, err
	}

	return ctl, nil
}

// Reconcile implements reconciler.Reconciler
func (ctl *Controller) Reconcile(ctx context.Context, _ controllerruntime.Request) (controllerruntime.Result, error) {
	ctl.lock.Lock()
	defer ctl.lock.Unlock()

	globalResourceList := &proxyv1alpha1.GlobalResourceList{}
	err := ctl.mgr.GetClient().List(ctx, globalResourceList)
	if err != nil {
		return controllerruntime.Result{RequeueAfter: time.Second * 5}, err
	}

	clusterList := &clusterv1alpha1.ClusterList{}
	err = ctl.mgr.GetClient().List(ctx, clusterList)
	if err != nil {
		return controllerruntime.Result{RequeueAfter: time.Second * 5}, err
	}

	newGlobalResourceSet := sets.NewString()
	for _, globalResource := range globalResourceList.Items {
		newGlobalResourceSet.Insert(globalResource.Name)
	}

	// Remove non exist GlobalResource
	ctl.caches.Range(func(key, value interface{}) bool {
		globalResource := key.(string)
		if !newGlobalResourceSet.Has(globalResource) {
			value.(*cache.MultiClusterCache).Stop()
			ctl.caches.Delete(globalResource)
		}
		return true
	})

	// Register new resource to scheme
	for _, globalResource := range globalResourceList.Items {
		for _, selector := range globalResource.Spec.ResourceSelectors {
			gv, err := schema.ParseGroupVersion(selector.APIVersion)
			if err != nil {
				return controllerruntime.Result{}, err
			}
			gvk := gv.WithKind(selector.Kind)
			if _, err = ctl.scheme.InfoFor(gvk); err == nil {
				// Resource is registered, skipped
				continue
			}

			restMapping, err := ctl.restMapper.RESTMapping(gvk.GroupKind(), gv.Version)
			if err != nil {
				return controllerruntime.Result{}, err
			}
			ctl.scheme.AddResourceInfo(scheme.ResourceInfo{
				Group:      restMapping.GroupVersionKind.Group,
				Version:    restMapping.GroupVersionKind.Version,
				Kind:       restMapping.GroupVersionKind.Kind,
				Resource:   restMapping.Resource.Resource,
				Namespaced: restMapping.Scope.Name() == meta.RESTScopeNameNamespace,
			})
		}

		newClusterSet := sets.NewString()
		for i := range clusterList.Items {
			cluster := &clusterList.Items[i]
			// TODO: filter unhealthy cluster
			if cluster.DeletionTimestamp == nil && util.ClusterMatches(cluster, globalResource.Spec.TargetCluster) {
				newClusterSet.Insert(cluster.Name)
			}
		}

		mccObj, _ := ctl.caches.LoadOrStore(globalResource.Name, &cache.MultiClusterCache{
			Mgr:    ctl.mgr,
			Scheme: ctl.scheme,
		})
		err = mccObj.(*cache.MultiClusterCache).UpdateCluster(newClusterSet, globalResource.Spec.ResourceSelectors)
		if err != nil {
			return controllerruntime.Result{}, err
		}
	}
	return controllerruntime.Result{}, nil
}

// Connect proxy and dispatch handlers
func (ctl *Controller) Connect(ctx context.Context, id string, options *proxyapis.ProxyOptions, responder rest.Responder) (http.Handler, error) {
	mccObj, ok := ctl.caches.Load(id)
	if !ok {
		return nil, fmt.Errorf("gloabelresource %v not found", id)
	}
	mcc := mccObj.(*cache.MultiClusterCache)

	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		cloneReq := req.Clone(req.Context())
		cloneReq.URL.Path = options.Path
		requestInfo := lifted.NewRequestInfo(cloneReq)

		newCtx := request.WithRequestInfo(ctx, requestInfo)
		newCtx = request.WithNamespace(newCtx, requestInfo.Namespace)
		cloneReq = cloneReq.WithContext(newCtx)

		h, err := ctl.connect(newCtx, mcc, responder, options)
		if err != nil {
			responder.Error(err)
			return
		}
		h.ServeHTTP(rw, cloneReq)
	}), nil
}

func (ctl *Controller) connect(ctx context.Context, mcc *cache.MultiClusterCache, responder rest.Responder, options *proxyapis.ProxyOptions) (http.Handler, error) {
	requestInfo, _ := request.RequestInfoFrom(ctx)
	gvr := schema.GroupVersionResource{
		Group:    requestInfo.APIGroup,
		Version:  requestInfo.APIVersion,
		Resource: requestInfo.Resource,
	}

	switch {
	case !requestInfo.IsResourceRequest || !mcc.HasResource(gvr):
		return ctl.karmadaProxy.connect(ctx, responder, options)
	case len(requestInfo.Subresource) == 0 && (requestInfo.Verb == "get" || requestInfo.Verb == "list" || requestInfo.Verb == "watch"):
		return ctl.localStorage.connect(ctx, mcc)
	default:
		cluster, err := ctl.getClusterForResource(ctx, requestInfo, mcc)
		if err != nil {
			return nil, err
		}
		return ctl.clusterProxy.connect(ctx, cluster, responder, options)
	}
}

// getClusterForResource returns which cluster the resource belong to.
func (ctl *Controller) getClusterForResource(ctx context.Context, requestInfo *request.RequestInfo, mcc *cache.MultiClusterCache) (*clusterv1alpha1.Cluster, error) {
	gvr := schema.GroupVersionResource{
		Group:    requestInfo.APIGroup,
		Version:  requestInfo.APIVersion,
		Resource: requestInfo.Resource,
	}
	clusterName, err := mcc.ClusterFor(ctx, gvr, requestInfo.Namespace, requestInfo.Name)
	if err != nil {
		return nil, err
	}

	cluster := &clusterv1alpha1.Cluster{}
	cluster.Name = clusterName
	err = ctl.mgr.GetClient().Get(ctx, client.ObjectKeyFromObject(cluster), cluster)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}
