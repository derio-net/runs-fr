package runs

import (
	"encoding/json"
	"strings"
	"testing"
)

// The local backend emits sparse Runs (no pod, no labels); runner tags the
// owning gateway in aggregated multi-runner lists. Both stay absent from the
// JSON when unset so kube-backend responses are unchanged.
func TestRunJSONShape(t *testing.T) {
	local, err := json.Marshal(Run{ID: "run-1", Status: "Running", Runner: "default"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(local), `"runner":"default"`) {
		t.Errorf("local run JSON missing runner tag: %s", local)
	}
	if strings.Contains(string(local), "podName") {
		t.Errorf("local run JSON must omit empty podName: %s", local)
	}

	kube, err := json.Marshal(Run{ID: "run-2", Status: "Running", PodName: "pod-a"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(kube), "runner") {
		t.Errorf("kube run JSON must omit empty runner: %s", kube)
	}
	if !strings.Contains(string(kube), `"podName":"pod-a"`) {
		t.Errorf("kube run JSON lost podName: %s", kube)
	}
}
