# Model Release Labels

Declarative BYOP plugin that shows a fixed release marker after the model name in
the composer, for example `DeepSeek V4.1 Flash 1017`. It copies how DeepSeek
Harness ships its own `dsh-client-ui-deepseek-model-label-1017` package: the plugin
supplies the badge text and the match rule, the host renders it.

## What it changes

| Part | Owner |
|---|---|
| Badge text and match rule | this plugin (`plugin.json`) |
| Rendering in the composer model chip | BYOP host (`contributes.modelLabels`) |
| Process | never started; no commands and no activation events |

## Match rules

- `match.providers` is required and must contain the Pi provider id.
- `match.modelId` and `match.modelName` are optional; an omitted field matches any
  value, a present field has to match exactly, ignoring case and surrounding space.
- A declared `modelId` also matches the part after the last `/` of the real id, so
  `deepseek-flash` covers `opencode-go/deepseek-flash` as reported by Pi.
- The first claim in registry order (plugin id, then array order) wins. A later
  plugin whose rule overlaps an earlier one is still registered, but its claim is
  reported as an issue on the Plugins page.

## Adding a badge

Add one object to `contributes.modelLabels`:

```json
{
  "id": "deepseek-flash-1017",
  "badge": "1017",
  "match": { "providers": ["deepseek"], "modelId": "deepseek-flash" }
}
```

`badge` takes 1-12 printable characters and must not start or end with a space.
Restart BYOP or rescan plugins on the Plugins page to pick up the change.

## Limits to know before trusting the badge

- The badge is a manual claim stored in this plugin. It does not expire, and the
  host cannot verify it. DeepSeek moved V4.1 Flash to the 1017 snapshot while
  keeping the old `deepseek-flash` model id, and no layer of the API exposes the
  snapshot (checked on 2026-10-18: no `system_fingerprint` in the response, model
  self-report unreliable). DeepSeek's own package calls the badge `Temporary`.
- Use the badge as a reminder of which release you configured, not as proof of
  which release answered the request.
- Only this plugin's provider and model id pairs are covered. Add entries for your
  own channels; the names in this repository match the local Pi catalog as of
  2026-10-31 and may need adjusting on another machine.

## Install for a walkthrough

```
cp -R plugins/model-release-labels ~/.byop/plugins/model-release-labels
```

Then rescan on the Plugins page. The repository copy is the source; the installed
copy under `~/.byop/plugins` is a working artifact.
