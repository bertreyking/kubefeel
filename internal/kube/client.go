package kube

import (
	"context"
	"fmt"
	"sync"
	"time"

	"multikube-manager/internal/model"
	"multikube-manager/internal/security"

	discovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Runtime struct {
	Config    *rest.Config
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
	Clientset kubernetes.Interface
}

type Factory struct {
	cipher *security.Cipher
	cache  sync.Map
}

func NewFactory(cipher *security.Cipher) *Factory {
	return &Factory{cipher: cipher}
}

func (f *Factory) Runtime(cluster *model.Cluster) (*Runtime, error) {
	if cached, ok := f.cache.Load(cluster.ID); ok {
		return cached.(*Runtime), nil
	}

	kubeconfig, err := f.cipher.Decrypt(cluster.KubeconfigEncrypted)
	if err != nil {
		return nil, err
	}

	runtime, err := BuildRuntimeFromKubeconfig([]byte(kubeconfig))
	if err != nil {
		return nil, err
	}

	f.cache.Store(cluster.ID, runtime)
	return runtime, nil
}

func (f *Factory) Invalidate(clusterID uint) {
	f.cache.Delete(clusterID)
}

func (f *Factory) Encrypt(value string) (string, error) {
	return f.cipher.Encrypt(value)
}

func (f *Factory) Decrypt(value string) (string, error) {
	return f.cipher.Decrypt(value)
}

func BuildRuntimeFromKubeconfig(kubeconfig []byte) (*Runtime, error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	config.QPS = 30
	config.Burst = 60
	config.Timeout = 15 * time.Second

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		Config:    config,
		Dynamic:   dynamicClient,
		Discovery: discoveryClient,
		Clientset: clientset,
	}, nil
}

type ProbeResult struct {
	Server         string
	CurrentContext string
	Version        string
	CRIVersion     string
}

func Probe(kubeconfig string) (ProbeResult, error) {
	loaded, err := clientcmd.Load([]byte(kubeconfig))
	if err != nil {
		return ProbeResult{}, err
	}

	currentContext := loaded.CurrentContext
	contextConfig, ok := loaded.Contexts[currentContext]
	if !ok {
		return ProbeResult{}, fmt.Errorf("current context %q not found", currentContext)
	}

	clusterConfig, ok := loaded.Clusters[contextConfig.Cluster]
	if !ok {
		return ProbeResult{}, fmt.Errorf("cluster config %q not found", contextConfig.Cluster)
	}

	server := clusterConfig.Server
	runtime, err := BuildRuntimeFromKubeconfig([]byte(kubeconfig))
	if err != nil {
		return ProbeResult{}, err
	}

	serverVersion, err := runtime.Discovery.ServerVersion()
	if err != nil {
		return ProbeResult{}, err
	}

	criVersion, _ := DetectContainerRuntimeVersion(context.Background(), runtime)

	return ProbeResult{
		Server:         server,
		CurrentContext: currentContext,
		Version:        serverVersion.String(),
		CRIVersion:     criVersion,
	}, nil
}

func DefaultKubeconfigPath() string {
	if home := homedir.HomeDir(); home != "" {
		return home + "/.kube/config"
	}

	return ""
}
