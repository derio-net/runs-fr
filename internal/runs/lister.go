package runs

import (
	"context"
	"errors"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// ErrNotFound reports that no live pod carries fr.run/id=<id>. The attach
// handler maps it to a 404.
var ErrNotFound = errors.New("runs: no pod for id")

// Label keys carried by run pods, projected into a Run.
const (
	labelID      = "fr.run/id"
	labelPlan    = "fr.run/plan"
	labelPhase   = "fr.run/phase"
	labelBranch  = "fr.run/branch"
	labelHarness = "fr.run/harness"
)

// Lister lists run pods in a namespace and maps them into Runs.
type Lister struct {
	client    kubernetes.Interface
	namespace string
	selector  labels.Selector
}

// NewLister builds a Lister scoped to a namespace and label selector.
func NewLister(client kubernetes.Interface, namespace string, selector labels.Selector) *Lister {
	return &Lister{client: client, namespace: namespace, selector: selector}
}

// List returns the live runs, one per matching pod, sorted by id.
func (l *Lister) List(ctx context.Context) ([]Run, error) {
	pods, err := l.client.CoreV1().Pods(l.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: l.selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("runs: list pods: %w", err)
	}

	out := make([]Run, 0, len(pods.Items))
	for i := range pods.Items {
		out = append(out, runFromPod(&pods.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Resolve returns the name of the live pod carrying fr.run/id=<id>. It queries
// by that exact label (independent of the gateway's configured list selector)
// so the attach target is the run the caller named. ErrNotFound is returned
// when no pod matches; if several do, the lowest pod name is chosen
// deterministically so repeated attaches land on the same pod.
func (l *Lister) Resolve(ctx context.Context, id string) (string, error) {
	pods, err := l.client.CoreV1().Pods(l.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelID + "=" + id,
	})
	if err != nil {
		return "", fmt.Errorf("runs: resolve pod for id %q: %w", id, err)
	}
	if len(pods.Items) == 0 {
		return "", ErrNotFound
	}
	name := pods.Items[0].Name
	for i := range pods.Items {
		if pods.Items[i].Name < name {
			name = pods.Items[i].Name
		}
	}
	return name, nil
}

// runFromPod projects a pod's fr.run/* labels and status phase into a Run.
func runFromPod(pod *corev1.Pod) Run {
	return Run{
		ID:      pod.Labels[labelID],
		Plan:    pod.Labels[labelPlan],
		Phase:   pod.Labels[labelPhase],
		Branch:  pod.Labels[labelBranch],
		Harness: pod.Labels[labelHarness],
		Status:  string(pod.Status.Phase),
		PodName: pod.Name,
	}
}
