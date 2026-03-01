package collectors

import (
	"fmt"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var (
	sharedClient dynamic.Interface
	clientOnce   sync.Once
	clientErr    error
)

// Client returns a shared dynamic Kubernetes client. The client is created
// once (using in-cluster config) and reused for all subsequent calls.
func Client() (dynamic.Interface, error) {
	clientOnce.Do(func() {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			clientErr = fmt.Errorf("in-cluster config: %w", err)
			return
		}
		sharedClient, clientErr = dynamic.NewForConfig(cfg)
		if clientErr != nil {
			clientErr = fmt.Errorf("dynamic client: %w", clientErr)
		}
	})
	return sharedClient, clientErr
}
