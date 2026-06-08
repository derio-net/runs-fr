// Package runs lists live agent-run pods and maps their fr.run/* labels into
// Run records the gateway serves and attaches to.
package runs

// Run is a single live agent run, projected from a pod's fr.run/* labels and
// its status. Field tags drive the JSON the gateway serves at /api/runs.
type Run struct {
	ID      string `json:"id"`
	Plan    string `json:"plan"`
	Phase   string `json:"phase"`
	Branch  string `json:"branch"`
	Harness string `json:"harness"`
	Status  string `json:"status"`
	PodName string `json:"podName"`
}
