# MQTT examples

Example Home Assistant automations that run rclone jobs through the add-on's
MQTT bridge. Publish a JSON message to `rclone/run`; the bridge runs the
command and publishes status/stats back. See the
[add-on README](../README.md#mqtt-job-control-optional) for the topic
reference.

## Stuff backup with rate limiting

Adapted from a traditional `rclone copy` shell script. All rclone flags become
keys in `command_args` (camelCase, without the leading `--`):

```yaml
automation:
  - alias: Nightly stuff backup
    triggers:
      - trigger: time
        at: "03:30:00"
    actions:
      - action: mqtt.publish
        data:
          topic: rclone/run
          payload: >-
            {"name": "Stuff Backup", "command": "sync/copy",
             "command_args": {"srcFs": "/share/my-stuff",
                              "dstFs": "my-share:/backup/my-stuff",
                              "minAge": "8h", "transfers": 2, "checkers": 4,
                              "tpslimit": 2, "retries": 5,
                              "lowLevelRetries": 10, "exclude": ["**/.*"]}}
```

Notes:

- `sync/copy` never deletes from the destination, matching `rclone copy`.
  Use `sync/sync` if the destination should exactly mirror the source.
- `exclude` takes a list of patterns (equivalent to repeated `--exclude`
  flags).

## Retention: pruning old backups

The original script keeps the newest 7 timestamped backup folders and purges
the rest. The rc API runs **one command per request** — there is no
equivalent of a shell loop — so express retention with rclone's built-in
time-based flags instead:

```yaml
automation:
  - alias: Prune stuff backups
    triggers:
      - trigger: time
        at: "04:30:00"
    actions:
      - action: mqtt.publish
        data:
          topic: rclone/run
          payload: >-
            {"name": "Stuff Backup Prune", "command": "operations/delete",
             "command_args": {"fs": "my-share:/backup/my-stuff",
                              "maxAge": "168h", "rmdirs": true}}
```

This deletes files older than 7 days (`168h`) and removes the emptied
directories. Schedule it to run after the backup job.

If you need exact "keep N newest folders" semantics, keep that part as a
script outside the add-on, since it requires listing, filtering, and looping.

## Alerting on failure

Trigger on the job's status topic reporting an error:

```yaml
automation:
  - alias: Stuff backup failed
    triggers:
      - trigger: mqtt
        topic: rclone/stuff_backup/status
        value_template: "{{ value_json.state }}"
        payload: "error"
    actions:
      - action: notify.notify
        data:
          title: Stuff backup failed
          message: "{{ trigger.payload_json.error }}"
```

The status topic is `rclone/<name>/status`, where `<name>` is the `name`
from the run message, lowercased with non-alphanumerics replaced by `_`
(so `Stuff Backup` → `stuff_backup`).
