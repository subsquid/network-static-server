# Static data server for SQD Network

Downloads assignment files from S3 and re-serves them on the localhost.
It allows N Workers/Portals to download it over the LAN instead of N direct
downloads from the S3 upstream.

## How it works

For each configured network the service:

1. Polls the upstream `network-state-<network>.json` every `poll-interval`.
2. When `assignment.id` changes, downloads the file at `assignment.fb_url_v1`
   into `<cache-dir>/<network>/<id>.fb.1.gz` and deletes the previous file.
3. Serves the metadata at `/network-state-<network>.json` with `fb_url_v1`
   rewritten to `http://<host>/data/<network>/<id>.fb.1.gz`.
4. Streams the cached data file at `/data/<network>/<id>.fb.1.gz` via
   `http.ServeFile`.

If a client requests metadata for a network whose data file has not finished
downloading yet, the request is transparently proxied to the upstream so the
client still gets a usable response.

`GET /ready` returns 200 only once every configured network has its initial
data file on disk; otherwise 503. Use it as the Kubernetes readiness probe.

## Building

Install Go and run

```bash
go build
```

## Running

```bash
static-server --networks mainnet,tethys --cache-dir /var/cache/static-server
```

Or with docker:

```bash
docker run -d --name static-server \
  -p 8080:8080 \
  -v static-server-cache:/tmp/cache \
  subsquid/network-static-server:latest \
  --networks mainnet,tethys
```

All flags can also be set via environment variables:

| Flag | Env | Default | Description |
| ---- | --- | ------- | ----------- |
| `--networks` | `NETWORKS` | *(required)* | Comma-separated network names. |
| `--poll-interval` | `POLL_INTERVAL` | `60s` | Interval of checking for upstream updates. |
| `--cache-dir` | `CACHE_DIR` | `/tmp/cache` | Directory for downloaded files. |
| `--listen-addr` | `LISTEN_ADDR` | `:8080` | HTTP listen address. |

A sample Docker image is built from the included `Dockerfile`. Mount a
persistent volume at `cache-dir` to avoid re-downloading on pod restart.

## Using from the Portal

In the Portal config (`portal/src/config.rs`), `assignments_url` is the **base
URL** — the portal appends `/network-state-<network>.json` itself. Point it at
the static-server's cluster-local address:

```yaml
hostname: ...
sqd_network:
  datasets: ...
assignments_url: http://static-server:8080
...
```

## Using from the Worker

In the worker, `--assignment-url` / `ASSIGNMENT_URL` is the **full URL** including
the file name. Set it per network:

```yaml
# mainnet
ASSIGNMENT_URL: http://static-server:8080/network-state-mainnet.json

# tethys
ASSIGNMENT_URL: http://static-server:8080/network-state-tethys.json
```
