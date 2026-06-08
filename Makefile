.PHONY: build test lint frontend

# Build the frontend bundle, then the Go binary (frontend output is embedded).
build: frontend
	go build -o bin/runs-fr ./cmd/runs-fr

test:
	go test ./... -race

lint:
	go vet ./...
	golangci-lint run

# Bundle the xterm.js frontend into cmd/runs-fr/static/ via esbuild.
frontend:
	cd web && npm ci && npm run build
