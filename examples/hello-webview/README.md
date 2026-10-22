# Hello WebView

This directory is a BYOP language-pack and WebView contract fixture, not a
feature plugin. It checks manifest `%key%` localization, English fallback,
locale-aware page refresh, the `byop-plugin` scheme, strict CSP, isolated theme
and locale bridges, capability-checked host calls, plugin RPC messages, and the
native Back to Plugins fallback.

Run it from the repository root:

```sh
test "$(node -p 'process.versions.node.split(".")[0]')" -ge 25
BYOP_PLUGINS_DIR="$PWD/examples" \
BYOP_PAGE=plugins \
BYOP_PI_CONNECT=0 \
go run .
```

Open **Hello WebView** from the sidebar. The fixture declares workspace read
access but no file access, so **Read file** must display a permission error.
**Ping plugin** starts the Node process on demand and returns the message with
an ISO timestamp. The manifest strings come from `l10n/<tag>.json`; changing
the language in BYOP refreshes the display names. The page calls
`host.getLocale`, reads `window.byop.locale`, and subscribes with
`window.byop.onLocale` to update its own copy immediately. The external link
is intentionally blocked by the WebView navigation lock.
