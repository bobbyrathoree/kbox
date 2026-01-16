package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps a Kubernetes clientset with context information
type Client struct {
	Clientset     *kubernetes.Clientset
	RestConfig    *rest.Config
	Context       string
	Namespace     string
	ServerVersion string
}

// ClientOptions configures how to build the client
type ClientOptions struct {
	Context   string
	Namespace string
}

// NewClient creates a new Kubernetes client
func NewClient(opts ClientOptions) (*Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}

	if opts.Context != "" {
		configOverrides.CurrentContext = opts.Context
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	// Get the raw config to determine context and namespace
	rawConfig, err := kubeConfig.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Determine the actual context being used
	context := opts.Context
	if context == "" {
		context = rawConfig.CurrentContext
	}

	// Determine namespace
	namespace := opts.Namespace
	if namespace == "" {
		ns, _, err := kubeConfig.Namespace()
		if err != nil {
			namespace = "default"
		} else {
			namespace = ns
		}
	}

	// Build rest config
	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build rest config: %w", err)
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Get server version
	serverVersion := "unknown"
	if version, err := clientset.Discovery().ServerVersion(); err == nil {
		serverVersion = version.GitVersion
	}

	return &Client{
		Clientset:     clientset,
		RestConfig:    restConfig,
		Context:       context,
		Namespace:     namespace,
		ServerVersion: serverVersion,
	}, nil
}

// DynamicClient creates a dynamic client for CRD operations
func (c *Client) DynamicClient() (dynamic.Interface, error) {
	return dynamic.NewForConfig(c.RestConfig)
}

// KubeconfigPath returns the path to the kubeconfig file
func KubeconfigPath() string {
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		return kubeconfig
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}

// HasKubeconfig checks if a kubeconfig file exists
func HasKubeconfig() bool {
	path := KubeconfigPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
