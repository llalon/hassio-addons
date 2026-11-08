# Home Assistant add-ons

This repository contains Home Assistant add-ons.

## Rclone

The [`rclone/`](rclone/) add-on runs the stable Rclone daemon with its embedded
web UI and exposes that UI through Home Assistant Ingress. An optional MQTT
bridge can expose recurring rclone jobs as Home Assistant switches and sensors
with automatic discovery. See its README for installation, storage access, and
security details.

Add this repository to Home Assistant's add-on store:

`https://github.com/llalon/hassio-addons`
