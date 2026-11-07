# Rclone Home Assistant add-on

Runs the latest pinned stable Rclone release as a persistent `rcd` daemon and
serves Rclone's embedded web UI in Home Assistant through Ingress. The sidebar
entry is available to administrators and can be enabled from the add-on's
**Show in sidebar** setting.

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

The embedded Rclone UI can access any files visible to the add-on. Treat
changes, syncs, and deletes as operations performed with the permissions above.

## Network and security

The add-on does not publish a host port. Rclone's remote-control API listens
only on container loopback, and an Nginx proxy accepts UI traffic only from the
Home Assistant Supervisor Ingress address. Home Assistant handles user
authentication for the sidebar UI. Do not change the Rclone API listener to
`0.0.0.0` or expose port 5572; the remote-control API can execute commands and
read or write files as the add-on.

## Updating Rclone

`RCLONE_VERSION` in `Dockerfile` pins the stable Rclone release included in the
image. The build verifies the official SHA-256 checksum file before installing
the binary. Update that value and the add-on version when publishing a newer
release.
