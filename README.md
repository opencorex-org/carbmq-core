# CrabMQ Core

CrabMQ Core is a QUIC-based messaging broker implementing QTT (QUIC Telemetry Transport), a modern alternative to MQTT designed for secure, low-latency IoT communication without certificate complexity.

This MVP keeps the QTT protocol inside the main repository while structuring the code like a production broker so the protocol layer can be extracted later with minimal churn.

## Architecture

- `cmd/crabmqd` runs the broker daemon in `broker`, `api`, or `all` mode.
- `internal/transport/quic` provides the QUIC transport with auto-generated TLS for encrypted-by-default sessions.
- `internal/broker/*` contains session tracking, topic subscriptions, routing, rate limiting, and offline delivery.
- `internal/auth/*` validates JWTs and enforces topic ACLs so devices only access their own namespaces.
- `pkg/qtt/*` is the public QTT package used by the Go CLI, simulator, and API bridge.
- `api/*` exposes device registration, token minting, telemetry queries, command dispatch, and WebSocket streaming.
- `web/dashboard` is a React + Tailwind operations console for fleet health, telemetry, commands, and metrics.
- `rust/crabmq-parser` and `rust/crabmq-cli` stay network-only and do not link directly against the Go code.

## Repository Layout

```text
crabmq-core/
  cmd/
    crabmqd/
    crabmq-cli/
    crabmq-sim/
  internal/
    broker/
    transport/
    protocol/
    auth/
    queue/
    metrics/
    config/
  pkg/
    qtt/
  rust/
    crabmq-cli/
    crabmq-parser/
  api/
  web/
    dashboard/
  configs/
  docs/
  examples/
  deployments/
```

## Setup

### Local prerequisites

- Go 1.25+
- Node.js 22+
- Docker Desktop or compatible Docker Engine
- PostgreSQL 16+ if you are not using Compose

### Environment

Copy the example environment file and adjust secrets and addresses as needed:

```bash
cp configs/crabmq.env.example .env
```

Important variables:

- `CRABMQ_JWT_SECRET`
- `CRABMQ_DATABASE_URL`
- `CRABMQ_BROKER_ADDR`
- `CRABMQ_API_ADDR`
- `CRABMQ_BROKER_METRICS_URL`

### Run with Docker Compose

```bash
docker compose up --build
```

Services:

- Dashboard: `http://localhost:3000`
- API: `http://localhost:8080`
- Broker QUIC listener: `udp://localhost:1884`
- Broker metrics: `http://localhost:9100/metrics`
- PostgreSQL: `postgres://postgres:postgres@localhost:5432/crabmq`

### Run locally without Docker

Start the broker and API in one process:

```bash
go run ./cmd/crabmqd all
```

Start only the broker:

```bash
go run ./cmd/crabmqd broker
```

Start only the API bridge:

```bash
go run ./cmd/crabmqd api
```

Run the simulator:

```bash
go run ./cmd/crabmq-sim
```

## Demo Steps

1. Start the stack with `docker compose up --build`.
2. Register a device:

```bash
curl -X POST http://localhost:8080/devices/register \
  -H 'Content-Type: application/json' \
  -d '{"id":"device-001","name":"Demo Device"}'
```

3. Generate a token:

```bash
curl -X POST http://localhost:8080/devices/device-001/token
```

4. Run the simulator or a CLI client:

```bash
go run ./cmd/crabmq-sim
```

5. Open the dashboard at `http://localhost:3000` and watch telemetry arrive live.
6. Send a command from the device details page.
7. Observe the simulator logging the received command and acknowledging QoS 1 delivery.

## QTT Packet Example

```json
{
  "type": "PUBLISH",
  "packetId": "f87c3b6b-3147-4129-8afd-bcb223e18d1b",
  "topic": "device/device-001/telemetry",
  "qos": 1,
  "payload": {
    "temperature": 23.6,
    "humidity": 46.4
  },
  "timestamp": "2026-04-30T10:15:30Z"
}
```

## Security Notes

- QUIC provides transport encryption by default.
- JWTs replace certificate provisioning for device identity in the MVP.
- Topic ACLs constrain device access to `device/{id}/...` namespaces.
- Broker-side rate limiting protects against burst abuse.
- QoS 1 packets are persisted for offline delivery until acknowledged.

## Documentation

- [QTT spec](docs/qtt-spec.md)
- [Architecture overview](docs/architecture.md)

## Current MVP Scope

- QUIC broker with multi-client session handling
- JSON-based QTT packets
- QoS 0 and QoS 1 publish flows
- Persistent offline queue
- JWT auth and topic ACLs
- Prometheus metrics endpoint
- Device registration and token issuance API
- Live dashboard over WebSocket
- Go simulator and Go CLI
- Rust parser and Rust CLI scaffold
