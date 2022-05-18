package storage

import (
	"context"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"

	proxyapis "github.com/karmada-io/karmada/pkg/apis/proxy"
)

// Connector provide connect handle to request
type Connector interface {
	Connect(ctx context.Context, id string, options *proxyapis.ProxyOptions, responder rest.Responder) (http.Handler, error)
}

var proxyMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// ProxyREST implements a RESTStorage for proxy resource.
type ProxyREST struct {
	connector Connector
}

var _ rest.Scoper = &ProxyREST{}
var _ rest.Storage = &ProxyREST{}
var _ rest.Connecter = &ProxyREST{}

// New return empty ProxyOptions object.
func (r *ProxyREST) New() runtime.Object {
	return &proxyapis.ProxyOptions{}
}

// NamespaceScoped returns false because ProxyOptions is not namespaced.
func (r *ProxyREST) NamespaceScoped() bool {
	return false
}

// ConnectMethods returns the list of HTTP methods handled by Connect.
func (r *ProxyREST) ConnectMethods() []string {
	return proxyMethods
}

// NewConnectOptions returns an empty options object that will be used to pass options to the Connect method.
func (r *ProxyREST) NewConnectOptions() (runtime.Object, bool, string) {
	return &proxyapis.ProxyOptions{}, true, "path"
}

// Connect returns a handler for the proxy.
func (r *ProxyREST) Connect(ctx context.Context, id string, options runtime.Object, responder rest.Responder) (http.Handler, error) {
	proxyOptions, ok := options.(*proxyapis.ProxyOptions)
	if !ok {
		return nil, fmt.Errorf("invalid options object: %#v", options)
	}
	return r.connector.Connect(ctx, id, proxyOptions, responder)
}
