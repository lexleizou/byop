# BYOP Design System

This directory is the public repository copy of the BYOP design data. The
canonical design source is `../../../design-system/pi-workbench/`; the two
`pi-workbench/tokens.json` files and the two
`pi-workbench/tokens.extensions.json` files must remain byte-identical.

| File | Authority |
|---|---|
| `pi-workbench/tokens.json` | Authoritative BYOP Radix product tokens consumed by the app |
| `pi-workbench/tokens.extensions.json` | Compatibility and migration layer for non-conflicting extension geometry, Chat details and aliases |
| `onevra/tokens.json` | Read-only Onevra upstream reference; not a replacement product palette |

Validate both product JSON files with `jq empty`. Onevra values and source
line references may remain in compatibility descriptions, but must not
override the product values.

The product foundation is 12/14/18 px type, 0.5 px visible borders, 4/8/12/16/24
spacing, 8 px input and 12 px overlay radii, and 250 ms ease-out motion with a
0 ms reduced-motion fallback. Liquid Glass is limited to chrome, navigation and
necessary overlays; reading surfaces and the Composer background stay opaque.
High contrast or reduced transparency uses opaque Radix colors. P12 token publication, existing page implementation and model-channel flows are complete. Native macOS checks and remaining limits are recorded in `docs/P12-validation.md`. The presence of tokens does
not prove every page consumes them. Penpot was not re-exported.
