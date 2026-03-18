# Requirements: Static Server (Dataset Cache)

**Defined:** 2026-03-18
**Core Value:** Every machine in the cluster gets the latest dataset files without each one independently downloading ~500MB over the public internet.

## v1 Requirements

### Polling & Sync

- [x] **POLL-01**: Service polls upstream `network-state-{name}.json` for each configured network on a configurable interval
- [x] **POLL-02**: Service detects new assignments by comparing `assignment.id` against the cached version
- [x] **POLL-03**: Service downloads the data file from `fb_url_v1` when a new assignment is detected
- [x] **POLL-04**: Service atomically switches to the new data file after successful download, then deletes the previous one

### Serving

- [x] **SERV-01**: Service serves `network-state-{name}.json` with `fb_url_v1` rewritten to point to the cache
- [x] **SERV-02**: Service serves the cached data file over HTTP, streaming without buffering in memory
- [x] **SERV-03**: Service proxies upstream metadata unchanged until the local data file is ready

### Configuration

- [x] **CONF-01**: Networks to cache are configurable (env var or CLI args)
- [x] **CONF-02**: Listen address/port is configurable
- [x] **CONF-03**: Poll interval is configurable with a sensible default

### Operations

- [x] **OPS-01**: Service logs polling activity, downloads, and errors to stdout
- [x] **OPS-02**: Dockerfile produces a minimal container image
- [x] **OPS-03**: Service handles graceful shutdown (finish in-flight downloads)

## v2 Requirements

### Observability

- **OBS-01**: Health check endpoint for k8s liveness/readiness probes
- **OBS-02**: Metrics endpoint (Prometheus) for download counts, cache hit rates, file sizes

### Resilience

- **RES-01**: Retry failed downloads with backoff
- **RES-02**: Optionally persist cached files to disk volume across restarts

## Out of Scope

| Feature | Reason |
|---------|--------|
| Authentication / access control | Internal cluster service, no external exposure |
| TLS termination | Handled by k8s ingress/service mesh |
| Multi-version caching | Simplicity — latest only, re-download on restart |
| Serving content beyond dataset files | Purpose-built cache, not a general proxy |
| Persistent storage | Re-download on startup is acceptable for v1 |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| POLL-01 | Phase 1 | Complete |
| POLL-02 | Phase 1 | Complete |
| POLL-03 | Phase 1 | Complete |
| POLL-04 | Phase 1 | Complete |
| SERV-01 | Phase 2 | Complete |
| SERV-02 | Phase 2 | Complete |
| SERV-03 | Phase 2 | Complete |
| CONF-01 | Phase 1 | Complete |
| CONF-02 | Phase 2 | Complete |
| CONF-03 | Phase 1 | Complete |
| OPS-01 | Phase 1 | Complete |
| OPS-02 | Phase 3 | Complete |
| OPS-03 | Phase 3 | Complete |

**Coverage:**
- v1 requirements: 13 total
- Mapped to phases: 13
- Unmapped: 0

---
*Requirements defined: 2026-03-18*
*Last updated: 2026-03-18 after roadmap creation*
