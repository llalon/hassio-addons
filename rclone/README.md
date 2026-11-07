# Rclone Home Assistant add-on

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

Rclone's configuration is saved persistently at `/config/rclone.conf`. On the
Home Assistant host, the file lives under `/app_configs/<repository>_rclone/`;
the repository identifier is `local` for local installs and a generated
identifier for GitHub installs. Use the web UI to configure remotes and run
Rclone operations, or edit this file directly.

The config contains remote credentials. Rclone obfuscates some secret values,
but that is not encryption; protect this file and any backups that contain it.

The add-on exposes these Home Assistant folders inside the container:

| Container path | Home Assistant folder | Access |
| --- | --- | --- |
| `/share` | Share | Read/write |
| `/media` | Media | Read/write |
| `/backup` | Backups | Read only |

The Rclone web UI can access any files visible to the add-on. Treat
changes, syncs, and deletes as operations performed with the permissions above.

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
