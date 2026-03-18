# Roadmap: Static Server (Dataset Cache)

## Overview

A three-phase build for a lightweight Go caching proxy. Phase 1 builds the polling engine that detects upstream changes, downloads data files, and manages atomic file swaps. Phase 2 adds the HTTP server that rewrites metadata and streams cached data files to cluster machines. Phase 3 wraps the service for production with a minimal container image and graceful shutdown.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Polling Engine** - Config parsing, upstream polling, data file download with atomic swap
- [x] **Phase 2: HTTP Serving** - Serve rewritten metadata and stream cached data files (completed 2026-03-18)
- [ ] **Phase 3: Production Readiness** - Dockerfile, graceful shutdown, end-to-end verification

## Phase Details

### Phase 1: Polling Engine
**Goal**: Service starts, polls upstream for configured networks, and maintains the latest data file locally
**Depends on**: Nothing (first phase)
**Requirements**: CONF-01, CONF-03, POLL-01, POLL-02, POLL-03, POLL-04, OPS-01
**Success Criteria** (what must be TRUE):
  1. Running the binary with a list of networks starts a polling loop that logs activity to stdout
  2. The service fetches upstream metadata for each configured network at the configured interval
  3. When `assignment.id` changes, the service downloads the new data file, atomically switches to it, and deletes the previous one
  4. Poll interval defaults to a sensible value and is overridable via flag or env var
**Plans:** 2/2 plans complete

Plans:
- [x] 01-01-PLAN.md — Go module init + download engine with idle-timeout reader and atomic file swap
- [x] 01-02-PLAN.md — Config parsing, per-network polling loop, main entry point with structured logging

### Phase 2: HTTP Serving
**Goal**: Cluster machines can point their metadata URL at this service and transparently get cached data files
**Depends on**: Phase 1
**Requirements**: SERV-01, SERV-02, SERV-03, CONF-02
**Success Criteria** (what must be TRUE):
  1. Requesting `/network-state-{name}.json` returns metadata with `fb_url_v1` rewritten to point at the cache service
  2. Requesting the data file URL streams the cached file without buffering it entirely in memory
  3. Before the local data file is ready, requesting metadata proxies the upstream response unchanged so clients are never broken
  4. Listen address and port are configurable
**Plans:** 2/2 plans complete

Plans:
- [ ] 02-01-PLAN.md — NetworkCache shared state with RWMutex + HTTP server handlers (metadata rewrite, data file streaming, upstream proxy)
- [ ] 02-02-PLAN.md — Wire cache into poller, add --listen-addr config, start HTTP server in main

### Phase 3: Production Readiness
**Goal**: Service is deployable as a minimal container in Kubernetes with clean lifecycle management
**Depends on**: Phase 2
**Requirements**: OPS-02, OPS-03
**Success Criteria** (what must be TRUE):
  1. `docker build` produces a small image (scratch or distroless base) with only the static binary
  2. Sending SIGTERM to the service finishes in-flight downloads before exiting
  3. The service runs end-to-end in a container: polls, downloads, serves rewritten metadata and data files
**Plans:** 1 plan

Plans:
- [ ] 03-01-PLAN.md — Graceful shutdown (signal handling + drain timeout) and Dockerfile (multi-stage scratch image)

## Progress

**Execution Order:**
Phases execute in numeric order: 1 -> 2 -> 3

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Polling Engine | 2/2 | Complete    | 2026-03-18 |
| 2. HTTP Serving | 0/2 | Complete    | 2026-03-18 |
| 3. Production Readiness | 0/1 | Not started | - |
