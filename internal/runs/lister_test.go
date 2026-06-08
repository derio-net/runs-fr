package runs

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"
)

// runPod builds a pod carrying the fr.run/* labels the gateway maps into a Run.
func runPod(name, id, plan, phase, branch, harness string, status corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "agents",
			Labels: map[string]string{
				"fr.run/id":      id,
				"fr.run/plan":    plan,
				"fr.run/phase":   phase,
				"fr.run/branch":  branch,
				"fr.run/harness": harness,
			},
		},
		Status: corev1.PodStatus{Phase: status},
	}
}

func TestListerList(t *testing.T) {
	// Seeded out of id order to prove List sorts by id.
	cs := fake.NewSimpleClientset(
		runPod("pod-zed", "run-2", "gateway", "5", "main", "claude", corev1.PodRunning),
		runPod("pod-abc", "run-1", "gateway", "2", "feature", "codex", corev1.PodPending),
		// A non-run pod (no fr.run/id) must be excluded by the selector.
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "noise", Namespace: "agents"}},
	)

	sel, err := labels.Parse("fr.run/id")
	if err != nil {
		t.Fatalf("parse selector: %v", err)
	}

	l := NewLister(cs, "agents", sel)
	got, err := l.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	want := []Run{
		{ID: "run-1", Plan: "gateway", Phase: "2", Branch: "feature", Harness: "codex", Status: "Pending", PodName: "pod-abc"},
		{ID: "run-2", Plan: "gateway", Phase: "5", Branch: "main", Harness: "claude", Status: "Running", PodName: "pod-zed"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %#v\nwant %#v", got, want)
	}
}
