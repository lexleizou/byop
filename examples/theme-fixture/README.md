# Theme fixture

Contract fixture for the plugin theme consumer. It declares one theme
contribution (`theme.fixture`) whose light and dark entries are standalone
semantic color files with an optional `font` / `codeFont` family, plus an inert
node entry that nothing activates, so a resource-only plugin stays at zero
workers.

The manifest resolves `%manifestName%` / `%manifestDescription%` from `l10n/`,
while the font families are real names (`Helvetica Neue` body, `Menlo` code) and
are not localized UI copy. The light file declares `background` and `text`; the
dark file carries its own values; a field missing from the matching mode keeps
the built-in token. With high contrast on, a selected theme colour never
replaces the accessible palette; with reduce transparency on, every applied
colour is forced opaque. `examples/` is fixture only, so this plugin is not a
business plugin and its theme is not a design deliverable.
