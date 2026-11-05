# Vendored Pele

- Package: `pele@1.7.0` (MIT, kepano / Obsidian; the bundled zoom reset icon
  "rotate-ccw" is ISC from Lucide v1.14.0 — see `LICENSE`).
- Source: the official npm tarball `pele-1.7.0.tgz` from
  `~/AgentScratch/20261020-pele-eval`, SHA256
  `56fa4e8a092285f7e6d9dc7a9f21a8daca7b6bf689b74ed8d62974de00f370d8`.
- Copied file: the unmodified official `node_modules/pele/dist/pele.js` ESM
  bundle, stored here as `pele.mjs`. SHA256 of this file
  `9d090e85100bda4fc8ec282257e07d350954435566354f1bd1a37d2469f10b98`.
- No build step, no npm install at runtime, and no CDN: the page imports this
  file directly. Upgrades must be a deliberate, re-verified change.
