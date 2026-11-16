# hahelper

Home Assistant ↔ HomeKit bridge using [brutella/hap](https://github.com/brutella/hap) and [go-ha-client/v2](https://github.com/mkelcik/go-ha-client).

Expose selected Home Assistant entities as bridged HomeKit accessories. Configure connection and entity exposure from a Bootstrap admin UI on your LAN (no auth — trusted home network only).

## Requirements

- Go 1.26.9+
- Home Assistant with a [long-lived access token](https://developers.home-assistant.io/docs/auth_api/#long-lived-access-token)
- macOS or Linux on the same LAN as iPhone/iPad
- Apple Home app

## Build

```bash
go build -o hahelper ./cmd/hahelper
```

## Deploy to Linux (systemd)

From your dev machine (with Go and SSH access):

```bash
chmod +x scripts/deploy.sh
./scripts/deploy.sh user@your-server
```

The script cross-compiles for the remote CPU (`amd64` / `arm64` / `armv7`), installs `/usr/local/bin/hahelper`, creates user `hahelper`, and enables `hahelper.service`.

| Path on server | Purpose |
|----------------|---------|
| `/etc/hahelper/config.json` | Main config (created from example on first deploy only) |
| `/etc/hahelper/env` | Optional `HAHELPER_HA_*` env vars (mode 600) |
| `/var/lib/hahelper/db` | HomeKit pairing + `exposed.json` |

After deploy, open `http://<server-ip>:8080`, or edit `/etc/hahelper/config.json` and run `sudo systemctl restart hahelper`.

```bash
ssh user@host 'sudo journalctl -u hahelper -f'
```

Redeploying the same command updates the binary and restarts the service; existing config and `db` are preserved.

## Configuration

vhelp loads `config.json` from `./config.json`, `~/.hahelper/config.json`, or `/etc/hahelper/config.json`.

Example:

```json
{
  "home_assistant": {
    "url": "http://homeassistant.local:8123",
    "token": "",
    "ws_reconnect": true
  },
  "bridge": {
    "name": "HA Helper",
    "pin": "00102003",
    "setup_id": "HAHP"
  },
  "ui": {
    "listen": "0.0.0.0:8080"
  },
  "hap": {
    "store_path": "./db",
    "verbose_log": false
  }
}
```

| Field | Description |
|-------|-------------|
| `home_assistant.url` / `token` | HA base URL and long-lived token |
| `bridge.name` | HomeKit bridge name |
| `bridge.pin` | 8-digit pairing code (`XXX-XX-XXX` in Home) |
| `bridge.setup_id` | 4-character ID for QR pairing (alphanumeric) |
| `ui.listen` | Admin UI bind address (default all interfaces `:8080`) |
| `hap.store_path` | Pairing keys, bridge state, and `exposed.json` |

**Environment overrides:** `HAHELPER_HA_URL`, `HAHELPER_HA_TOKEN` (token can stay out of JSON).

**Exposure list:** `{store_path}/exposed.json` — which `entity_id`s are bridged (stable HomeKit accessory IDs).

## Run

```bash
./hahelper
```

1. Open the admin UI: `http://<host>:8080` from any device on your LAN.
2. **Setup** — enter HA URL and token, test and save.
3. **Entities** — filter by domain, enable **Expose to HomeKit**, optionally override **HomeKit name**, then save (bridge reloads). Overrides are stored in `exposed.json` (`homekit_name` per entry).
4. **HomeKit** — scan QR or enter PIN; add the bridge in the Home app.

Supported HA [entity domains](https://www.home-assistant.io/docs/configuration/entities_domains/) (HomeKit mapping):

| Domain | HomeKit type |
|--------|----------------|
| `switch`, `input_boolean` | Switch (dimmer when brightness is supported) |
| `light` | Light (on/off + brightness) |
| `cover` | Window covering; garage `device_class` → garage door |
| `fan` | Fan (on/off + speed) |
| `lock` | Door (lock/unlock) |
| `climate` | Thermostat (basic heat/cool/temp) |
| `humidifier`, `dehumidifier` | Humidifier / dehumidifier |
| `water_heater` | Heater |
| `valve` | Sprinkler / irrigation valve |
| `binary_sensor` | Contact or motion sensor (door/window/motion `device_class` only) |
| `siren` | Switch |
| `alarm_control_panel` | Security system (arm/disarm; no PIN from HomeKit) |
| `button`, `input_button` | Switch (HomeKit on → HA `press`) |
| `camera` | IP camera (live view via HomeKit RTP; **ffmpeg** required on the host) |

The admin UI domain filter is loaded from `GET /api/domains` and matches this list.

### Cameras

Expose `camera.*` entities like any other device. Live view uses HomeKit **RTP** (same model as [hkcam](https://github.com/brutella/hkcam)): hahelper pulls video from Home Assistant and ffmpeg encodes it to H.264 SRTP for the Home app.

**Requirements**

- `ffmpeg` installed on the host running hahelper (deploy target must have it in `PATH`).
- HA URL + token configured (stream uses `Authorization: Bearer …` against HA’s API).
- **HomeKit / ffmpeg (default):** pulls `http://<ha>/api/camera_proxy_stream/<entity_id>` with `Authorization: Bearer …` (one hop, lower latency).
- **Local proxy (debug / fallback):** `GET http://127.0.0.1:<ui-port>/api/camera/stream/<entity_id>` still works for curl/VLC. Set `"camera_via_proxy": true` under `hap` to route ffmpeg through it instead.
- ffmpeg transcodes MJPEG to **H.264** for HomeKit.

**Optional** (`exposed.json` per camera entry):

| Field | Purpose |
|--------|---------|
| `stream_source` | RTSP URL instead of the HA proxy (e.g. `rtsp://user:pass@cam:554/...`). Uses `ffmpeg -rtsp_transport tcp` and `video_codec` copy when possible. |

**Config** (`hap.camera_multi_stream`): set `true` to allow multiple simultaneous HomeKit viewers (extra CPU; default `false`).

**iOS live view** sends encrypted **UDP** (SRTP) to the address your iPhone reports in setup (see `camera setup: controller=…` in logs). That path must work from the hahelper host — same LAN as the phone, or both on a VPN (e.g. Tailscale) that routes UDP. macOS Home on Wi‑Fi often uses a `192.168.*` controller IP while an iPhone on VPN may use `10.*` / `100.*`; if the host cannot reach that IP, you get a spinning wheel. Prefer viewing on the same network as hahelper, or use `stream_source` RTSP when the camera exposes it.

If logs show `End of file` on `camera_proxy_stream` after ~15–30s, try `"camera_via_proxy": true` (reconnecting local proxy) or an RTSP `stream_source`.

Still-image-only cameras may need a direct RTSP URL or `stream_source` in `exposed.json` until snapshot-loop mode is exposed in the UI.

## HomeKit shows “No Response” but controls work

HomeKit marks accessories unreachable when it has not heard from the bridge recently, even if taps still work (your phone opens a fresh connection to hahelper).

hahelper mitigates this by:

- **Resubscribing** to Home Assistant `state_changed` after WebSocket drops (previously the listener could stop while HA calls still worked).
- **Polling HA every 90 seconds** (`hap.state_poll_seconds`, REST `get_states`) and pushing current values to HomeKit so hubs see fresh data.

Also check: Apple Home hub and the machine running hahelper on the **same LAN/VLAN**, no firewall blocking mDNS or TCP to the HAP port, and stable IP for the host. Saving exposure or HA settings **reloads the HAP server** briefly — expect tiles to flicker for a few seconds.

Set `"state_poll_seconds": -1` to turn off polling (not recommended unless you are debugging). Use a lower value (e.g. `45`) if tiles still go stale often.

## Security

The admin UI has **no login**. Anyone on your LAN can change settings. Do **not** port-forward or expose `:8080` to the internet. Prefer `HAHELPER_HA_TOKEN` in the environment instead of storing the token in `config.json`.

## Rooms in Apple Home

**HAP cannot assign Apple Home rooms** — room layout lives in the Home app / HomeKit database, not on the accessory protocol (same as Homebridge). You assign rooms when adding accessories or later in the Home app.

hahelper helps by naming bridged accessories with room context:

| `bridge.room_naming` | Behavior |
|----------------------|----------|
| `both` (default) | HA **area** + friendly name, e.g. `Kitchen · Pendant`; if no area, infer from `entity_id` prefix (`kitchen_spot` → `Kitchen · …`) |
| `area` | Only HA area registry |
| `entity_id` | Only entity_id prefix heuristic |
| `none` | Friendly name only |

The Entities tab shows **HomeKit name** and **Area** before you expose devices. Reload the bridge (save exposure) after changing names in HA.

## HomeKit notes

- Keep `hap.store_path` across restarts to avoid re-pairing.
- After changing exposed entities, the bridge reloads; pairing data is preserved.
- Set `"hap.verbose_log": true` for low-level HAP library logs during pairing debug.

## License

Apache-2.0 (hap dependency); project license TBD.
