# QTT Protocol Specification

QTT (QUIC Telemetry Transport) is a lightweight messaging protocol carried over QUIC. The MVP uses newline-delimited JSON packets on a single bidirectional QUIC stream per client session.

## Transport

- Transport: QUIC over UDP
- Encryption: TLS 1.3 provided by QUIC
- Authentication: JWT passed in the `CONNECT` packet
- Session mode: persistent subscriptions with offline QoS 1 queueing

## Packet Types

- `CONNECT`
- `CONNACK`
- `PUBLISH`
- `PUBACK`
- `SUBSCRIBE`
- `SUBACK`
- `PING`
- `PONG`
- `DISCONNECT`
- `ERROR`

## Canonical Packet Shape

```json
{
  "type": "PUBLISH",
  "packetId": "uuid",
  "clientId": "device-001",
  "topic": "device/device-001/telemetry",
  "qos": 1,
  "payload": {
    "temperature": 21.4
  },
  "timestamp": "2026-04-30T10:15:30Z"
}
```

## Packet Semantics

### CONNECT

- Sent first on every session
- Carries `clientId` and `token`
- Broker validates JWT and establishes a session

### CONNACK

- Sent after successful authentication
- `status` is `accepted`
- `sessionPresent` indicates whether previous subscriptions were restored

### PUBLISH

- Delivers telemetry, commands, and other topic payloads
- `qos: 0` is fire-and-forget
- `qos: 1` requires `PUBACK`

### PUBACK

- Acknowledges receipt of a QoS 1 message
- Broker uses it to clear offline queue entries and mark messages delivered

### SUBSCRIBE

- Registers one or more topic filters
- Each subscription includes `topic` and `qos`

### SUBACK

- Confirms subscription registration

### PING / PONG

- Lightweight keepalive mechanism

### DISCONNECT

- Graceful session teardown

### ERROR

- Broker-initiated rejection or protocol failure
- Includes `code` and `error`

## Topic Model

- Telemetry publish: `device/{deviceId}/telemetry`
- Command subscribe: `device/{deviceId}/command`
- Admin bridge wildcard: `device/+/telemetry`

The broker supports `+` and `#` wildcard matching for subscriptions.

## Authorization Rules

- `admin` role can publish or subscribe anywhere
- `device` role can only publish under `device/{id}/...`
- `device` role can only subscribe to `device/{id}/command`

## Offline Delivery

- QoS 1 messages for offline subscribers are stored in the persistent queue
- Pending packets are replayed when the client reconnects
- Queue entries are removed only after `PUBACK`
