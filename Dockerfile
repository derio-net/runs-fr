# syntax=docker/dockerfile:1

# Multi-stage build for the runs-fr gateway.
#
# Stage 1 bundles the xterm.js frontend with esbuild; stage 2 compiles a static
# Go binary with the bundle embedded (//go:embed static/*); the final image is
# distroless-static running as nonroot. The layout mirrors `make frontend` +
# `go build` so the image and a local checkout build the same artifact.

# ---- Stage 1: frontend bundle -------------------------------------------------
# Node 22 matches the CI toolchain. build.mjs writes bundle.js, xterm.css and
# index.html into ../cmd/runs-fr/static (the only dir the embed directive can
# reach), so the output lands at /src/cmd/runs-fr/static.
FROM node:22-alpine AS frontend
WORKDIR /src/web
# Install deps from the lockfile first so this layer caches across source edits.
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Stage 2: static Go binary ------------------------------------------------
# go.mod pins go 1.23. CGO_ENABLED=0 yields a fully static binary that runs on
# distroless-static with no libc.
FROM golang:1.23-alpine AS build
WORKDIR /src
# Download modules first for layer caching.
COPY go.mod go.sum ./
RUN go mod download
# Copy the source, then overlay the freshly-built frontend so the embed glob
# picks up bundle.js (which is git-ignored and absent from the source copy).
COPY . .
COPY --from=frontend /src/cmd/runs-fr/static/ ./cmd/runs-fr/static/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/runs-fr ./cmd/runs-fr

# ---- Final image --------------------------------------------------------------
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/runs-fr /runs-fr
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/runs-fr"]
