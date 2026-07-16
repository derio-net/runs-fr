package config

import "testing"

func TestLoad(t *testing.T) {
	// envVars not listed in a case's env map are cleared for that case via
	// t.Setenv(key, ""), so defaults are exercised deterministically.
	allKeys := []string{"ADDR", "NAMESPACE", "LABEL_SELECTOR", "AUTH_HEADER", "AUTH_ROLE_HEADER", "KUBECONFIG", "BACKEND", "TMUX_SOCKET", "RUNNER_NAME"}

	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		want    Config
	}{
		{
			name:    "namespace required",
			env:     map[string]string{}, // NAMESPACE unset, BACKEND defaults kube
			wantErr: true,
		},
		{
			name: "defaults applied when only namespace set",
			env:  map[string]string{"NAMESPACE": "agents"},
			want: Config{
				Addr:           ":8080",
				Namespace:      "agents",
				LabelSelector:  "fr.run/id",
				AuthHeader:     "X-Forwarded-User",
				AuthRoleHeader: "X-Forwarded-Role",
				Kubeconfig:     "",
				Backend:        "kube",
			},
		},
		{
			name: "local backend needs no namespace",
			env:  map[string]string{"BACKEND": "local"},
			want: Config{
				Addr:           ":8080",
				LabelSelector:  "fr.run/id",
				AuthHeader:     "X-Forwarded-User",
				AuthRoleHeader: "X-Forwarded-Role",
				Backend:        "local",
			},
		},
		{
			name: "local backend loads tmux socket and runner name",
			env: map[string]string{
				"BACKEND":     "local",
				"TMUX_SOCKET": "/run/tmux/cnc.sock",
				"RUNNER_NAME": "default",
			},
			want: Config{
				Addr:           ":8080",
				LabelSelector:  "fr.run/id",
				AuthHeader:     "X-Forwarded-User",
				AuthRoleHeader: "X-Forwarded-Role",
				Backend:        "local",
				TmuxSocket:     "/run/tmux/cnc.sock",
				RunnerName:     "default",
			},
		},
		{
			name:    "unknown backend rejected",
			env:     map[string]string{"BACKEND": "docker", "NAMESPACE": "agents"},
			wantErr: true,
		},
		{
			name: "all overrides honored",
			env: map[string]string{
				"ADDR":           ":9090",
				"NAMESPACE":      "runs",
				"LABEL_SELECTOR": "fr.run/owner",
				"AUTH_HEADER":    "X-Auth-Request-User",
				"KUBECONFIG":     "/home/user/.kube/config",
			},
			want: Config{
				Addr:           ":9090",
				Namespace:      "runs",
				LabelSelector:  "fr.run/owner",
				AuthHeader:     "X-Auth-Request-User",
				AuthRoleHeader: "X-Forwarded-Role",
				Kubeconfig:     "/home/user/.kube/config",
				Backend:        "kube",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range allKeys {
				t.Setenv(k, tt.env[k])
			}

			got, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() expected error, got nil (cfg=%+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if *got != tt.want {
				t.Errorf("Load() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}
