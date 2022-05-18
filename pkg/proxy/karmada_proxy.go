package proxy

import (
	"context"
	"net/http"
	"net/url"
	"path"

	"k8s.io/apiserver/pkg/registry/rest"
	restclient "k8s.io/client-go/rest"

	proxyapis "github.com/karmada-io/karmada/pkg/apis/proxy"
)

// karmadaProxy is proxy for karmada control panel
type karmadaProxy struct {
	proxyLocation  *url.URL
	proxyTransport http.RoundTripper
}

func newKarmadaProxy(restConfig *restclient.Config) (*karmadaProxy, error) {
	location, err := url.Parse(restConfig.Host)
	if err != nil {
		return nil, err
	}
	transport, err := restclient.TransportFor(restConfig)
	if err != nil {
		return nil, err
	}

	return &karmadaProxy{
		proxyLocation:  location,
		proxyTransport: transport,
	}, nil
}

// connect to Karmada-ApiServer directly
func (p *karmadaProxy) connect(_ context.Context, responder rest.Responder, options *proxyapis.ProxyOptions) (http.Handler, error) {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		location, transport := p.resourceLocation()
		location.Path = path.Join(location.Path, options.Path)
		location.RawQuery = req.URL.RawQuery

		handler := newThrottledUpgradeAwareProxyHandler(location, transport, true, false, responder)
		handler.ServeHTTP(rw, req)
	}), nil
}

func (p *karmadaProxy) resourceLocation() (*url.URL, http.RoundTripper) {
	location := *p.proxyLocation
	return &location, p.proxyTransport
}
