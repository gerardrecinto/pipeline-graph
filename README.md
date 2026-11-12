# pipeline-graph

A GraphQL API over CI/CD data: services, their pipelines and every run. Written in Go, schema first. A personal project to show GraphQL API design, not a production system.

## Run it

```
docker run -p 8080:8080 ghcr.io/gerardrecinto/pipeline-graph:latest   # or: go run .
curl -s localhost:8080 -d '{"query":"{ runs(first: 2, status: FAILED) { totalCount edges { cursor node { id status pipeline { name service { name } } } } pageInfo { hasNextPage endCursor } } }"}'
```

Trigger a run (mutations need a bearer token; locally it is `dev-token`, set `API_TOKEN` to change it):

```
curl -s localhost:8080 -H 'Authorization: Bearer dev-token' \
  -d '{"query":"mutation { triggerRun(pipelineId: \"api-build\", branch: \"main\") { id status } }"}'
```

Pipeline IDs: `api-build`, `api-deploy`, `web-build`, `web-deploy`, `data-build`, `data-deploy`. Needs Go 1.23+. Data is in memory and resets on restart.

## What is in it

- **Schema first.** The whole API is in [schema.graphql](schema.graphql); resolvers are plain Go methods.
- **Cursor pagination.** `runs` returns a Relay style connection (`edges`, `cursor`, `pageInfo`, `totalCount`) with an optional `status` filter. `first` is capped at 100 and bad cursors are rejected.
- **No N+1.** `Run.pipeline` and `Pipeline.service` go through a per-request batching loader. Listing 40 runs costs one pipeline lookup, and a test counts it.
- **Query limits.** Maximum depth 6, so `service { pipelines { service { pipelines ... } } }` loops are refused. Request bodies are capped at 1 MB.
- **Authenticated mutation.** `triggerRun` needs a bearer token (constant time compare) and validates the pipeline and the branch name.
- **Operations.** `/healthz`, Prometheus `/metrics` (request counts, latency histogram, responses with errors), JSON logs, server timeouts and graceful shutdown on SIGTERM.
- **Tests.** `go test -race ./...` covers pagination, filtering, bad input, batching, the depth limit, auth, validation, metrics and shutdown. CI fails below 85% coverage.
- **Pipeline.** vet, staticcheck, govulncheck, CodeQL, a container smoke test, Dependabot. A tag push builds four binaries with SHA256SUMS signed by Sigstore, and pushes a signed distroless image to GHCR.

## Trade-offs

- `graph-gophers/graphql-go` over `gqlgen`: no code generation step and a smaller project, but resolvers are matched by reflection at startup, so a schema and method mismatch panics on boot instead of failing at compile time. I would move to gqlgen for a larger schema.
- A depth limit and page cap are cheap and enough here. A public API would also need cost-based complexity limits and persisted queries.
- In-memory store keeps the demo to one command. The loaders and resolvers would sit unchanged over a real database.

MIT license.
