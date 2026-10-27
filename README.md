# Location SDK for Go

A Go client for a separately distributed local location and alarm engine. This repository contains the public protocol, process management, generated code, examples and transport tests. It has no dependency on a private source repository.

Requires Go 1.27. Install with `go get github.com/nangongwentian-fe/location-sdk-go@v0.1.0`. The initial release is under acceptance; use only after the SDK tag and matching engine artifacts are published.

## Usage

Download the engine for your platform at build/deployment time. Pin its version and SHA-256 in your application configuration. Call `Open(ctx, Options{Path: absolutePath, Version: version, SHA256: checksum})` once, share the client, and `Close` after workers finish. The context passed to Open owns the client lifetime. No runtime download or automatic update occurs.

`Info`, `ValidateConfig`, `EvaluateLocations`, and `EvaluateAlarms` use types in `protocol`. RPC details and process restarts are managed by the client. Default startup timeout is 10 seconds and RPC timeout is 5 seconds. Requests accept at most 100 items; partition by device in order and pass the returned state to the next request. Calls fail during restarts; they are not automatically retried. `ErrUnavailable` identifies timeout or transport failure and `ErrClosed` identifies a closed client.

The host provides spatial facts, previous state, evaluation time (Unix milliseconds) and new episode IDs. Reuse the same input including time and episode ID when retrying a computation. Persist events/results and state atomically; the engine performs no database or delivery I/O. State payloads are opaque. Projection fields may be stored in existing database columns.

Canonical alarm facts use `online`/`offline`, and `bluetooth`, `satellite`, `wifi-beacon`, `wifi`, `cellular`, or `unknown` location types. Translate vendor-specific values at your application boundary. Optional fields preserve missing values; identifiers are integer/string, never floating point. Location actions include `accept`, `hold`, `reject`, `snap` and any supported rule's documented output; do not silently reinterpret an unknown action.

Run `go run ./examples/smoke /absolute/path/location-engine` to execute synthetic location and alarm requests. The sample reads the adjacent manifest; a production application must verify that manifest against its independently pinned release metadata.

## Build and test

`go test -race ./...` exercises real local gRPC transport, timeout, crash/restart, normal shutdown, and abrupt host termination using a synthetic test server. It contains no production algorithm. Protocol generation uses protoc 36.2, protoc-gen-go v1.36.12 and protoc-gen-go-grpc v1.6.2:

```sh
protoc -I proto --go_out=. --go_opt=module=github.com/nangongwentian-fe/location-sdk-go --go-grpc_out=. --go-grpc_opt=module=github.com/nangongwentian-fe/location-sdk-go proto/location/v1/engine.proto
```

SDK tags use `vX.Y.Z`; binary attachments use separate `engine-vX.Y.Z` releases so engine publication is not treated as a Go module version. The private build produces archives, SHA256SUMS and compatibility.json. Linux amd64/arm64, macOS arm64 and Windows amd64 are target platforms; platform support is accepted only after actual runtime checks.
