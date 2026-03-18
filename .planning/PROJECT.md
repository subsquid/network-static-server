# Static Server (Dataset Cache)

## What This Is

A lightweight caching proxy that sits between SQD dataset consumers and the upstream metadata service (`metadata.sqd-datasets.io`). It polls upstream metadata JSON files for multiple networks, automatically downloads the referenced data files (~500MB each) when they change, and serves both the metadata and data files to cluster machines over the local network — eliminating redundant public internet downloads.

## Core Value

Every machine in the cluster gets the latest dataset files without each one independently downloading ~500MB over the public internet.

## Requirements

### Validated

(None yet — ship to validate)

### Active

- [ ] Poll upstream `metadata.sqd-datasets.io/network-state-{name}.json` for configurable list of networks
- [ ] Detect when `assignment.id` changes (new data file available)
- [ ] Download the data file referenced by `fb_url_v1` when assignment changes
- [ ] Serve the metadata JSON with `fb_url_v1` rewritten to point to the cache service
- [ ] Serve the downloaded data file over HTTP
- [ ] Keep only the latest data file per network (delete previous on successful download of new one)
- [ ] Support multiple networks via configuration (CLI args or env var)
- [ ] Handle the ~20 minute upstream update cycle gracefully
- [ ] Run as a container in Kubernetes

### Out of Scope

- Authentication / access control — internal cluster service
- Persistent storage across restarts — re-downloads on startup are acceptable
- Caching multiple versions of data files — latest only
- Serving any content beyond network-state metadata and data files
- TLS termination — handled by k8s ingress/service mesh

## Context

- Upstream metadata URL pattern: `https://metadata.sqd-datasets.io/network-state-{network}.json`
- Metadata JSON structure: `{ network, assignment: { url, fb_url, fb_url_v1, id, effective_from } }`
- The `assignment.id` field uniquely identifies each data file version
- Data files are gzipped flatbuffers (~500MB), referenced by `fb_url_v1`
- Upstream updates roughly every 20 minutes
- Currently every cluster machine independently downloads these files over public internet
- Services are already configured to poll a metadata URL — just need to repoint them to the cache

## Constraints

- **Language**: Go — stdlib-only, zero external dependencies
- **Simplicity**: Minimal code (~200-300 lines), no frameworks
- **Deployment**: Kubernetes pod, small container image (scratch/distroless base)
- **Memory**: Must stream large files, not buffer them in memory

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go with stdlib only | Simplest possible implementation, no dependency management | — Pending |
| Latest-only caching | Reduces storage, simplifies logic, re-download on restart is acceptable | — Pending |
| Rewrite fb_url_v1 in metadata | Clients don't need config changes, just repoint metadata URL | — Pending |

---
*Last updated: 2026-03-18 after initialization*
