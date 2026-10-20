# Hello Node

This directory is a contract fixture for the BYOP P4.2 plugin platform. It is
not a feature plugin. The plain `plugin.mjs` file has no dependencies or build
step and exercises the Node runtime, native view model, typed settings,
plugin-to-host calls, task updates, and refresh notifications.

Run it from the repository root:

```sh
test "$(node -p 'process.versions.node.split(".")[0]')" -ge 25
BYOP_PLUGINS_DIR="$PWD/examples" \
BYOP_PAGE=plugins \
BYOP_PI_CONNECT=0 \
go run .
```

Node.js 25 or newer is required because the fixture uses the Node permission
model's `--allow-net` flag surface.

Open **Hello Node** from the sidebar or its plugin card. The settings page
shows the `greeting`, `showDetails`, and `tone` fields. Saving while the plugin
is active sends `settingsChanged`; the fixture updates the panel through
`host.refreshView`.

The plugin requests only workspace and file reads. It declares network and
process access as disabled. Node additionally runs with its permission model
enabled, while every host call is checked against the same manifest.
