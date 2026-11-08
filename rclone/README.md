# Rclone Home Assistant add-on

> [!WARNING]
> Do not use this yet!

Runs the latest pinned stable Rclone release as a persistent `rcd` daemon and
serves the official Rclone web UI through Home Assistant Ingress. The UI is
built with a dynamic base path so its routes and API calls stay under the
Ingress URL. The sidebar entry is available to administrators and can be
enabled from the add-on's **Show in sidebar** setting.

## Install

Add this repository to **Settings → Add-ons → Add-on store → ⋮ → Repositories**,
install **Rclone**, then start it. Without a published image, Home Assistant
builds the Docker image on the device during installation, which takes longer
than downloading a prebuilt image.

## Storage and configuration

Rclone's configuration is saved persistently at
`/root/.config/rclone/rclone.conf`. On the Home Assistant host, the file lives
under `/app_configs/<repository>_rclone/`; the repository identifier is `local`
for local installs and a generated identifier for GitHub installs. Use the web
UI to configure remotes and run Rclone operations, or edit this file directly.

Rclone's complete configuration file is `rclone.conf`, which uses Rclone's
INI-style format. A provider credential file named `config.json` is not an
Rclone configuration file; enter it through that provider's configuration
option instead.

The config contains remote credentials. Rclone obfuscates some secret values,
but that is not encryption; protect this file and any backups that contain it.

The add-on exposes these Home Assistant folders inside the container:

| Container path | Home Assistant folder | Access |
| --- | --- | --- |
| `/share` | Share | Read/write |
| `/media` | Media | Read/write |
| `/backup` | Backups | Read only |

In the web UI, use `/share`, `/media`, `/backup`, or
`/root/.config/rclone` for local paths. Paths such as `/root/Desktop` and
`/root/Downloads` do not exist in the app container.

The Rclone web UI can access any files visible to the add-on. Treat
changes, syncs, and deletes as operations performed with the permissions above.

## MQTT job control (optional)

The add-on includes a small Go bridge that runs rclone commands on request
over MQTT. Home Assistant automations publish a command to a topic; the
bridge runs it against the local rclone daemon and publishes status and
stats back to MQTT. No host ports are published, and nothing is scheduled by
the add-on itself — **when** a job runs is entirely up to your automations.

Requirements: an MQTT broker reachable from the add-on and the MQTT
integration configured in Home Assistant.

**No configuration is needed** when you use the Mosquitto broker add-on (or
any add-on that provides the MQTT service): the connection is detected
automatically through the Supervisor. The `mqtt_host` / `mqtt_port` /
`mqtt_username` / `mqtt_password` options on the **Configuration** tab are
only needed to override auto-detection, for example to use an external
broker.

Other options:

- `topic_prefix` (default `rclone`) — prefix for all MQTT topics below.
- `poll_interval_seconds` (default 30) — how often running jobs are polled
  for status/stats.
- `job_expire_seconds` (default 3600) — how long finished jobs stay
  queryable by the bridge; applied to rclone as `--rc-job-expire-duration`.

The topics below assume the default `rclone` prefix; if you change
`topic_prefix`, substitute it for `rclone`.

### Running a job from Home Assistant

Publish a JSON message to the topic `rclone/run`:

```json
{
  "name": "Documents Backup",
  "command": "sync/sync",
  "command_args": {
    "srcFs": "/share/documents",
    "dstFs": "/share/backups/documents"
  }
}
```

- `command` is any [rc method](https://rclone.org/rc/#supported-commands).
- `command_args` is that method's
  [parameters](https://rclone.org/rc/#json-input); `_async` is added
  automatically.
- `name` is optional and scopes the status/stats topics (defaults to the
  command).

In an automation, use the `mqtt.publish` action:

```yaml
automation:
  - alias: Nightly documents backup
    triggers:
      - trigger: time
        at: "03:00:00"
    actions:
      - action: mqtt.publish
        data:
          topic: rclone/run
          payload: >-
            {"name": "Documents Backup", "command": "sync/sync",
             "command_args": {"srcFs": "/share/documents",
                              "dstFs": "/share/backups/documents"}}
```

### Monitoring

The bridge publishes retained JSON messages:

- `rclone/<name>/status` — `{"state": "running"|"finished"|"error", "jobid": N, "error": "..."}`
- `rclone/<name>/stats` — `{"jobid": N, "bytes": ..., "total_bytes": ..., "speed": ..., "progress": ..., "eta": ..., "errors": ...}` while running
- `rclone/bridge/availability` — `online`/`offline`

To get notified on failure, trigger on `rclone/documents_backup/status`
containing `"state": "error"`, e.g. with an MQTT trigger:

```yaml
automation:
  - alias: Documents backup failed
    triggers:
      - trigger: mqtt
        topic: rclone/documents_backup/status
        value_template: "{{ value_json.state }}"
        payload: "error"
    actions:
      - action: notify.notify
        data:
          title: Documents backup failed
          message: "{{ trigger.payload_json.error }}"
```

More examples, including rate-limited backups and retention pruning, are in
[docs/examples.md](docs/examples.md).

## Network and security

The add-on does not publish a host port. Nginx accepts traffic only from the
Home Assistant Supervisor Ingress address, serves the UI, and forwards API
requests to Rclone on container loopback. Rclone's API has no separate login;
Home Assistant handles authentication for the Ingress session. Do not change
the Rclone API listener to `0.0.0.0` or expose port 5572; the API can execute
commands and read or write files as the add-on.

## Updating Rclone

`RCLONE_VERSION` in `Dockerfile` pins the stable Rclone release included in the
image. The build verifies the official SHA-256 checksum file before installing
the binary. Update that value and the add-on version when publishing a newer
release.
