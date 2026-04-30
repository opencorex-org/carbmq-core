FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/crabmq-sim ./cmd/crabmq-sim

FROM alpine:3.20
RUN adduser -D -g '' crabmq
USER crabmq
WORKDIR /app
COPY --from=builder /out/crabmq-sim /app/crabmq-sim
ENTRYPOINT ["/app/crabmq-sim"]
