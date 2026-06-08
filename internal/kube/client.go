// Package kube builds the Kubernetes client surface the gateway needs: a
// *rest.Config that works both in-cluster and from a kubeconfig, a typed
// clientset, and the label selector that identifies run pods.
package kube

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/derio-net/runs-fr/internal/config"
)

// BuildSelector parses the configured LABEL_SELECTOR into a labels.Selector.
// A bare key such as "fr.run/id" matches any pod carrying that label.
func BuildSelector(cfg *config.Config) (labels.Selector, error) {
	sel, err := labels.Parse(cfg.LabelSelector)
	if err != nil {
		return nil, fmt.Errorf("kube: parse label selector %q: %w", cfg.LabelSelector, err)
	}
	return sel, nil
}

// RestConfig resolves a *rest.Config. It prefers the in-cluster config (the
// production path) and falls back to a kubeconfig — cfg.Kubeconfig if set,
// otherwise the standard chain ($KUBECONFIG, then ~/.kube/config) — so the
// gateway is runnable locally against any cluster.
func RestConfig(cfg *config.Config) (*rest.Config, error) {
	rc, err := rest.InClusterConfig()
	switch {
	case err == nil:
		return rc, nil
	case errors.Is(err, rest.ErrNotInCluster):
		// Expected when running outside a pod; fall through to kubeconfig.
	default:
		return nil, fmt.Errorf("kube: in-cluster config: %w", err)
	}

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if cfg.Kubeconfig != "" {
		rules.ExplicitPath = cfg.Kubeconfig
	}
	rc, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("kube: load kubeconfig: %w", err)
	}
	return rc, nil
}

// NewClientset builds a typed Kubernetes clientset from the resolved config.
func NewClientset(cfg *config.Config) (*kubernetes.Clientset, error) {
	rc, err := RestConfig(cfg)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("kube: build clientset: %w", err)
	}
	return cs, nil
}

// Readiness returns a check that lists one pod in the namespace. It surfaces a
// broken ServiceAccount/RBAC (or an unreachable API server) as an error so the
// /readyz probe can pull the pod from Service endpoints. The selector is not
// applied: readiness probes API/RBAC reachability, not whether any run exists.
func Readiness(cs kubernetes.Interface, namespace string) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			return fmt.Errorf("kube: readiness pods.list: %w", err)
		}
		return nil
	}
}
