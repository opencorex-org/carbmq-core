FROM golang:1.25-alpine AS builder
WORKDIR /workspace

COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/crabmqd ./cmd/crabmqd

FROM alpine:3.20
RUN adduser -D -g '' crabmq
USER crabmq
WORKDIR /app
COPY --from=builder /out/crabmqd /app/crabmqd
EXPOSE 1884/udp 9100
ENTRYPOINT ["/app/crabmqd"]
CMD ["broker"]
