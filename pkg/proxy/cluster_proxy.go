package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterapis "github.com/karmada-io/karmada/pkg/apis/cluster"
	"github.com/karmada-io/karmada/pkg/apis/cluster/v1alpha1"
	proxyapis "github.com/karmada-io/karmada/pkg/apis/proxy"
)

// clusterProxy proxy to member clusters
type clusterProxy struct {
	resourceReader client.Reader
}

func newClusterProxy(resourceReader client.Reader) *clusterProxy {
	return &clusterProxy{
		resourceReader: resourceReader,
	}
}

func (c *clusterProxy) connect(ctx context.Context, cluster *v1alpha1.Cluster, responder rest.Responder, options *proxyapis.ProxyOptions) (http.Handler, error) {
	location, transport, err := c.ResourceLocation(cluster)
	if err != nil {
		return nil, err
	}
	location.Path = path.Join(location.Path, options.Path)

	impersonateToken, err := c.getImpersonateToken(ctx, cluster)
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requester, exist := request.UserFrom(req.Context())
		if !exist {
			responsewriters.InternalError(rw, req, errors.New("no user found for request"))
			return
		}
		req.Header.Set(authenticationv1.ImpersonateUserHeader, requester.GetName())
		for _, group := range requester.GetGroups() {
			if !skipGroup(group) {
				req.Header.Add(authenticationv1.ImpersonateGroupHeader, group)
			}
		}

		req.Header.Set("Authorization", fmt.Sprintf("bearer %s", impersonateToken))
		location.RawQuery = req.URL.RawQuery

		handler := newThrottledUpgradeAwareProxyHandler(location, transport, true, false, responder)
		handler.ServeHTTP(rw, req)
	}), nil
}

// ResourceLocation returns a URL to which one can send traffic for the specified Cluster.
func (c *clusterProxy) ResourceLocation(cluster *v1alpha1.Cluster) (*url.URL, http.RoundTripper, error) {
	location, err := constructLocation(cluster)
	if err != nil {
		return nil, nil, err
	}

	transport, err := createProxyTransport(cluster)
	if err != nil {
		return nil, nil, err
	}

	return location, transport, nil
}

func (c *clusterProxy) getImpersonateToken(ctx context.Context, cluster *v1alpha1.Cluster) (string, error) {
	if cluster.Spec.ImpersonatorSecretRef == nil {
		return "", fmt.Errorf("the impersonatorSecretRef of Cluster %s is nil", cluster.Name)
	}

	var secret corev1.Secret
	secret.Name = cluster.Spec.ImpersonatorSecretRef.Name
	secret.Namespace = cluster.Spec.ImpersonatorSecretRef.Namespace
	err := c.resourceReader.Get(ctx, client.ObjectKeyFromObject(&secret), &secret)
	if err != nil {
		return "", err
	}

	token, found := secret.Data[clusterapis.SecretTokenKey]
	if !found {
		return "", fmt.Errorf("the impresonate token of Cluster %s is empty", cluster.Name)
	}
	return string(token), nil
}

func constructLocation(cluster *v1alpha1.Cluster) (*url.URL, error) {
	if cluster.Spec.APIEndpoint == "" {
		return nil, fmt.Errorf("API endpoint of Cluster %s should not be empty", cluster.Name)
	}

	uri, err := url.Parse(cluster.Spec.APIEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parseResourceDefinition api endpoint %s: %v", cluster.Spec.APIEndpoint, err)
	}
	return uri, nil
}

func createProxyTransport(cluster *v1alpha1.Cluster) (*http.Transport, error) {
	trans := utilnet.SetTransportDefaults(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	})

	if cluster.Spec.ProxyURL != "" {
		proxy, err := url.Parse(cluster.Spec.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parseResourceDefinition url of proxy url %s: %v", cluster.Spec.ProxyURL, err)
		}
		trans.Proxy = http.ProxyURL(proxy)
	}
	return trans, nil
}

func skipGroup(group string) bool {
	switch group {
	case user.AllAuthenticated, user.AllUnauthenticated:
		return true
	default:
		return false
	}
}
