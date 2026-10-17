# Intelligent load balancer

This repository contains both parts of the project:

- `simulator/` - Go backend, including `main.go`, `go.mod`, and all Go packages.
- `visualizer/` - React, TypeScript, and Vite frontend.

Requirements: Go 1.23.2 or newer, Node.js, and npm.

## Run the simulator

From the repository root:

```sh
cd simulator
go run .
```

The Go module remains named `load-balancer`, so package imports stay the same.
The simulator writes its runtime trace to `trace.out` in its working directory.

## Run the visualizer

In a separate terminal, from the repository root:

```sh
cd visualizer
npm ci
npm run dev
```

The visualizer connects to `ws://localhost:8080/ws`. In the current simulator
configuration, the calls to `setupHttpServer` and `runTicker` in
`simulator/main.go` are commented out; enable them to stream simulation updates.

## Verify the project

Run `go test ./...` from `simulator/`. From `visualizer/`, run `npm run build`
and `npm run lint`.

Dependencies, build output, runtime traces, and local repository backups are
excluded from Git by the root `.gitignore`.
