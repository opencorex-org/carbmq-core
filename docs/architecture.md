# CrabMQ Core Architecture

## Runtime Topology

```text
Simulator / Go CLI / Rust CLI
            |
            | QUIC + QTT
            v
       crabmqd broker  ----->  /metrics
            |
            | PostgreSQL
            v
            db

       crabmqd api  <----- QTT bridge subscription
            |
            +---- HTTP REST
            +---- WebSocket telemetry stream
            |
            v
       React dashboard
```

## Broker Layers

### `pkg/qtt`

Public protocol package containing packet models, topic helpers, and the Go client.

### `internal/protocol`

Internal adapter layer for packet encoding and decoding. It currently wraps the public QTT packet types so the broker and external tools share the same wire schema without duplicating logic.

### `internal/transport/quic`

Owns QUIC listener startup, TLS configuration, stream acceptance, and connection adaptation into packet-aware interfaces.

### `internal/broker`

- `server`: session lifecycle, packet dispatch, QoS handling, metrics, rate limiting
- `session`: connected/offline session state
- `subscription`: topic filter registry
- `router`: publish fan-out and offline queue fallback

### `internal/auth`

- `jwt`: token issuance and verification
- `acl`: topic-level authorization rules

### `internal/queue`

- `memory`: simple in-memory queue for tests or future ephemeral mode
- `persistent`: PostgreSQL-backed offline queue for QoS 1 delivery

### `api`

The API process is a separate runtime that bridges into the broker using the public Go QTT client. That keeps the architecture scalable and preserves a clean network boundary.

### `web/dashboard`

Reads devices, telemetry history, and metrics through the API while consuming live telemetry over WebSocket.

## Design Choices

- QUIC instead of TCP: encryption and low-latency stream handling are built in.
- JWT instead of client certificates: simpler device onboarding for the MVP.
- PostgreSQL persistence: one durable store for device registry, telemetry history, and offline queue state.
- Public `pkg/qtt`: prepares the protocol for future extraction into a dedicated repository without forcing that split now.
