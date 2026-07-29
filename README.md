# mqtt-unifi-network

WiFi presence detection from the UniFi Network controller, bridged to MQTT.
Tracks a configured set of devices (phones), reports whether each is home, which
floor it is on, and whether anyone is home at all.

## Features

- Per-device presence on `haus/unifi-network/clients/<slug>` (retained)
- Floor occupancy on `haus/unifi-network/floors/<floor>` (retained)
- `haus/unifi-network/anyone_home` as a raw `true` / `false`
- Away delay that survives phones sleeping their WiFi radio
- Floor derived from the access point a client is associated with
- Web UI with live updates over Server-Sent Events
- `/api/livez` liveness endpoint so Kubernetes restarts a stuck bridge

## MQTT topics

| Topic | Retained | Payload |
|---|---|---|
| `haus/unifi-network/clients/<slug>` | yes | `{"name":"Philipp","online":true,"floor":"og","ap":"OG-Flur","rssi":-55,…}` |
| `haus/unifi-network/floors/<floor>` | yes | `{"count":2,"clients":["philipp","anna"]}` |
| `haus/unifi-network/anyone_home` | yes | `true` / `false` |
| `haus/unifi-network/availability` | yes | `online` / `offline` |
| `haus/unifi-network/bridge/state` | yes | `online` / `offline` (last will, from mqtt-gateway) |

`availability` says the bridge can reach the **controller**; `bridge/state` says
the bridge **process** is alive. A client's `online` is presence; `associated` is
whether it is on the air right this second.

## The away delay

A phone that idles powers its radio down and disappears from the controller's
client list for minutes at a time. Publishing that verbatim fires a false "left
the house" every night, so:

- appearing in the client list marks a device **online immediately**;
- disappearing marks it offline only after `away_delay` seconds (default 300),
  and every fresh sighting restarts that countdown.

A failed poll never changes presence — the controller being unreachable is not
evidence that anyone left.

## Floors

UniFi does not know what floor a client is on; it knows which access point the
client is associated with. `floors` maps an AP to a label, keyed by either the
AP's MAC or its name in the controller:

```json
"floors": { "aa:bb:cc:00:00:01": "eg", "OG-Flur": "og" }
```

WiFi bleeds between floors, so a client near a ceiling or a stairwell can
associate with the AP above or below and be reported on the wrong floor. The
published `signal` is the tell — the web UI flags anything weaker than about
-75 dBm.

Mind the two signal fields, they are not the same thing:

| Field | Meaning |
|---|---|
| `signal` | received power in **dBm**, negative — use this to judge quality |
| `rssi` | UniFi's signal-above-noise figure, **positive**, higher is better |

## Private WiFi addresses

An iPhone with *Private Wi-Fi Address* set to **Rotating** changes its MAC
periodically, which silently breaks presence for that device: it simply stops
being seen and goes away after the away delay. For any tracked phone, set that
option to **Fixed** (or off) for this network. A MAC whose second hex digit is
`2`, `6`, `A` or `E` is a randomized one.

## Finding MACs and AP names

Start with `devices` empty. After the first successful poll the service logs
every client and access point it can see, once, at info level. Copy the MACs you
want into `devices` and the APs into `floors`.

## Configuration

See `production/config/config.example.json`. `${VAR}` placeholders are replaced
from the environment at startup, so secrets stay out of the config file. The
substitution is textual and runs before the JSON is parsed, so a placeholder can
stand in for a whole value — in the cluster the tracked people are one encrypted
`UNIFI_DEVICES` blob:

```json
"devices": ${UNIFI_DEVICES}
```

Personal MACs are personal data, so they live in the sops-encrypted
`values/secrets.yaml` rather than in the chart's plaintext values.

The UniFi account needs **Network → View Only** on the site. The local account
used by the sibling `unifi-access` service works if it has that role.

## Quick start

```bash
cd app
make dev          # build frontend + backend, run with production/config/config.json
make dev-frontend # vite dev server on :5173 against the backend on :8080
make test
```

## Release

Run the **Build release** workflow (`patch` / `minor` / `major`) — it tags,
builds multi-arch images and pushes `pharndt/mqtt-unifi-network:vX.Y.Z`. Then
bump `image.tag` in
`homeserver-gitops/cluster/charts/mqtt/unifi-network/chart/values.yaml` and run
`cluster/charts/mqtt/unifi-network/install.sh`.
