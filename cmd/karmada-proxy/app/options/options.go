package options

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"
	controllerruntime "sigs.k8s.io/controller-runtime"

	proxyv1alpha1 "github.com/karmada-io/karmada/pkg/apis/proxy/v1alpha1"
	generatedopenapi "github.com/karmada-io/karmada/pkg/generated/openapi"
	"github.com/karmada-io/karmada/pkg/proxy"
	"github.com/karmada-io/karmada/pkg/proxy/scheme"
	"github.com/karmada-io/karmada/pkg/util/gclient"
	"github.com/karmada-io/karmada/pkg/util/lifted"
)

const defaultEtcdPathPrefix = "/registry"

// Options contains everything necessary to create and run karmada-proxy.
type Options struct {
	RecommendedOptions *genericoptions.RecommendedOptions

	// KubeAPIQPS is the QPS to use while talking with karmada-proxy.
	KubeAPIQPS float32
	// KubeAPIBurst is the burst to allow while talking with karmada-proxy.
	KubeAPIBurst int
}

// NewOptions returns a new Options.
func NewOptions() *Options {
	o := &Options{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			defaultEtcdPathPrefix,
			proxy.Codecs.LegacyCodec(proxyv1alpha1.SchemeGroupVersion)),
	}
	o.RecommendedOptions.Etcd.StorageConfig.EncodeVersioner = runtime.NewMultiGroupVersioner(proxyv1alpha1.SchemeGroupVersion,
		schema.GroupKind{Group: proxyv1alpha1.GroupName})
	o.RecommendedOptions.Etcd.StorageConfig.Paging = utilfeature.DefaultFeatureGate.Enabled(features.APIListChunking)
	return o
}

// AddFlags adds flags to the specified FlagSet.
func (o *Options) AddFlags(flags *pflag.FlagSet) {
	o.RecommendedOptions.AddFlags(flags)
	flags.Lookup("kubeconfig").Usage = "Path to karmada control plane kubeconfig file."

	flags.Float32Var(&o.KubeAPIQPS, "kube-api-qps", 40.0, "QPS to use while talking with karmada-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	flags.IntVar(&o.KubeAPIBurst, "kube-api-burst", 60, "Burst to use while talking with karmada-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.")
	utilfeature.DefaultMutableFeatureGate.AddFlag(flags)
}

// Complete fills in fields required to have valid data.
func (o *Options) Complete() error {
	return nil
}

// Validate validates Options.
func (o *Options) Validate() error {
	var errs []error
	errs = append(errs, o.RecommendedOptions.Validate()...)
	return utilerrors.NewAggregate(errs)
}

// Run runs the aggregated-apiserver with options. This should never exit.
func (o *Options) Run(ctx context.Context) error {
	config, err := o.Config()
	if err != nil {
		return err
	}

	restConfig := config.GenericConfig.ClientConfig
	restConfig.QPS, restConfig.Burst = o.KubeAPIQPS, o.KubeAPIBurst

	proxyScheme := scheme.NewScheme(runtime.NewScheme())
	scheme.Install(proxyScheme)

	mgr, err := controllerruntime.NewManager(restConfig, controllerruntime.Options{
		Logger: klog.Background(),
		Scheme: gclient.NewSchema(),
	})
	if err != nil {
		return err
	}

	ctl, err := proxy.NewController(mgr, proxyScheme, time.Second*time.Duration(config.GenericConfig.MinRequestTimeout))
	if err != nil {
		return err
	}

	server, err := config.Complete().New(ctl)
	if err != nil {
		return err
	}

	server.GenericAPIServer.AddPostStartHookOrDie("start-karmada-proxy-informers", func(context genericapiserver.PostStartHookContext) error {
		config.GenericConfig.SharedInformerFactory.Start(context.StopCh)
		return nil
	})

	server.GenericAPIServer.AddPostStartHookOrDie("start-karmada-proxy-controller", func(context genericapiserver.PostStartHookContext) error {
		return mgr.Start(ctx)
	})
	return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
}

// Config returns config for the api server given Options
func (o *Options) Config() (*proxy.Config, error) {
	// TODO have a "real" external address
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	serverConfig := genericapiserver.NewRecommendedConfig(proxy.Codecs)
	serverConfig.LongRunningFunc = customLongRunningRequestCheck(
		sets.NewString("watch", "proxy"),
		sets.NewString("attach", "exec", "proxy", "log", "portforward"))
	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(generatedopenapi.GetOpenAPIDefinitions, openapi.NewDefinitionNamer(proxy.Scheme))
	serverConfig.OpenAPIConfig.Info.Title = "karmada-proxy"
	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, err
	}

	config := &proxy.Config{
		GenericConfig: serverConfig,
		ExtraConfig:   proxy.ExtraConfig{},
	}
	return config, nil
}

func customLongRunningRequestCheck(longRunningVerbs, longRunningSubresources sets.String) request.LongRunningRequestCheck {
	return func(r *http.Request, requestInfo *request.RequestInfo) bool {
		if requestInfo.APIGroup == "proxy.karmada.io" && requestInfo.Subresource == "proxy" {
			reqClone := r.Clone(context.TODO())
			// requestInfo.Parts: /globalresources/xx/proxy/...
			reqClone.URL.Path = "/" + path.Join(requestInfo.Parts[3:]...)
			requestInfo = lifted.NewRequestInfo(reqClone)
		}
		return genericfilters.BasicLongRunningRequestCheck(longRunningVerbs, longRunningSubresources)(r, requestInfo)
	}
}
