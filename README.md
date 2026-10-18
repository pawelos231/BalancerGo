# Intelligent load balancer

This repository contains both parts of the project:

- `simulator/` - Go backend, including `main.go`, `go.mod`, and all Go packages.
- `visualizer/` - React, TypeScript, and Vite frontend.

## Run with Docker Compose

Install Docker Desktop with Linux containers and start it. From the repository
root, build and start both applications:

```sh
docker compose up --build
```

Open `http://localhost:5173`. The backend is available at `http://localhost:8081`
and its WebSocket at `ws://localhost:8081/ws`. Press `Ctrl+C` to stop both applications.
Subsequent starts can reuse the built images:

```sh
docker compose up
```

There are no source bind mounts or automatic rebuilds. After changing source
files or dependencies, run `docker compose up --build` again. To rebuild only
the backend, use `docker compose up --build -d backend`. To run everything in
the background, use `docker compose up -d`; stop and remove containers with
`docker compose down`. Read logs with `docker compose logs -f`.

The frontend is built into static files and served by `local-web-server` inside
a Node.js container. Frontend and backend publish separate local ports; the
browser connects directly to the backend WebSocket and reconnects automatically
when the backend restarts. Dependencies and build tools are installed inside
the images.

To use different ports in PowerShell:

```powershell
$env:FRONTEND_PORT = '5174'
$env:BACKEND_PORT = '8082'
docker compose up --build
```

The backend WebSocket URL is passed to Vite during image build. Changing
`BACKEND_PORT` therefore requires rebuilding the frontend. Compose maps host
port 8081 to port 8080 inside the backend container by default.

## Run locally without Docker

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

The frontend connects directly to `ws://localhost:8080/ws`. The backend streams simulation
updates and stays available after the simulated streams finish. Set
`LB_HTTP_ADDR` to override the backend listen address, or `VITE_WS_URL` before
starting Vite to override the browser WebSocket URL.

## Verify the project

Run `go test ./...` from `simulator/`. From `visualizer/`, run `npm run build`
and `npm run lint`.

Dependencies, build output, runtime traces, and local repository backups are
excluded from Git by the root `.gitignore`.
