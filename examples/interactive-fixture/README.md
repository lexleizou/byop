# Interactive Fixture

Contract fixture for the P25 interactive contributions. It is not a business
plugin: every command answers only what the host sends, so a walkthrough can
tell which contribution was consumed. It runs on the same JSONL protocol as
`examples/hello-node` and needs no files, network, or child processes.

| Slot | Declaration | Host path that consumes it |
| --- | --- | --- |
| `statusItems` | `fixture.status` | `fixture.status.publish` / `fixture.status.clear` call `host.setStatusItem`; the HUD footer shows or releases the item. Both commands also appear on the plugin detail page, so the item can be checked without a composer. |
| `messageRenderers` (language) | `renderer.qa-card` → `fixture.render.qa-card` | A closed ```` ```qa-card ```` fence in an assistant message. Payload `{language, code}`; result is the typed native view envelope. |
| `messageRenderers` (MIME) | `renderer.image` → `fixture.render.image` | A sent `image/png` attachment chip. Payload `{mimeType, name, size, path}`; `path` is present only while the source file is known. |
| `slashCommands` | `/qa-status` (command), `/qa-prompt` (prompt template) | Composer suggestion list. A command row runs through the leased invoke path; a prompt row only inserts the template. |
| `mentionProviders` | `mention.fixture` → `fixture.mention.query` | Composer `@` query. Payload `{query, workspace}`; result `{items:[{id,label,text}]}` and the chosen `text` replaces the active token. |
| `paletteProviders` | `palette.fixture` → `fixture.palette.query` | Command palette query. Payload `{query}`; result `{items:[{id,label,command,args}]}` and only the declared command IDs run. |

Quick walkthrough:

1. Plugins page → Interactive Fixture → **Publish status item**. The HUD footer
   shows `QA fixture active`; **Clear status item** removes it.
2. Ask the model (or a fake Pi) to answer with a closed `qa-card` fence. The
   fence becomes a card headed `QA CARD from interactive-fixture`.
3. Attach a PNG and send it. The chip becomes a card headed
   `QA IMAGE from interactive-fixture`, listing exactly what the host sent.
4. Type `/qa` in the composer, pick a row explicitly, and type `@` to see the
   mention rows. Nothing here runs from a typed prefix alone.

The renderer results are cached per plugin and content, so a plugin restart or
update drops them; the plain fence or chip stays visible while a render is
pending or after one fails.
