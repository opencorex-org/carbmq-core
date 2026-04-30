#!/usr/bin/env sh
set -eu

curl -X POST http://localhost:8080/devices/register \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "device-001",
    "name": "Demo Device",
    "metadata": {
      "site": "lab"
    }
  }'
