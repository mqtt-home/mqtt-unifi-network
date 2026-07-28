# mqtt-unifi-network

A bridge between UniFi Network and a local MQTT broker, with a built-in web UI.

## Features

- Publishes device state to `haus/unifi-network/status` (retained)
- Publishes connection state to `haus/unifi-network/availability` (`online` / `offline`, retained)
- Accepts JSON commands on `haus/unifi-network/set`
- Web UI with live updates over Server-Sent Events
- `/api/livez` liveness endpoint so Kubernetes restarts a stuck bridge

## MQTT topics

| Topic | Direction | Payload |
|---|---|---|
| `haus/unifi-network/status` | published, retained | JSON device state |
| `haus/unifi-network/availability` | published, retained | `online` \| `offline` |
| `haus/unifi-network/set` | subscribed | `{"action": "..."}` |

## Quick start

### Docker

```bash
docker run -d \
  -v /path/to/config:/var/lib/mqtt-unifi-network \
  -p 8080:8080 \
  pharndt/mqtt-unifi-network:latest
```

### From source

```bash
cd app
make dev          # build frontend + backend, run with production/config/config.json
make dev-frontend # vite dev server on :5173 against the backend on :8080
make test
```

## Configuration

See `production/config/config.example.json`. `${VAR}` placeholders are replaced
from the environment at startup, so secrets stay out of the config file.

```json
{
  "mqtt": {
    "url": "tcp://10.10.1.3:1883",
    "topic": "haus/unifi-network",
    "qos": 2,
    "retain": true
  },
  "unifi": {
    "username": "${UNIFI_USER}",
    "password": "${UNIFI_PASSWORD}",
    "polling_interval": 30
  },
  "web": { "enabled": true, "port": 8080 },
  "loglevel": "info"
}
```

## Release

Run the **Build release** workflow (`patch` / `minor` / `major`) — it tags,
builds multi-arch images and pushes `pharndt/mqtt-unifi-network:vX.Y.Z` to Docker Hub.
Then bump `image.tag` in
`homeserver-gitops/cluster/charts/mqtt/unifi-network/chart/values.yaml` and run
`cluster/charts/mqtt/unifi-network/install.sh`.
