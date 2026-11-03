# Locale fixture

Contract fixture for the plugin locale consumer. It declares one locale
contribution (`fr`) whose entry is a standalone MyGo catalog file, plus an inert
node entry that nothing activates, so a resource-only plugin stays at zero
workers.

The keys it provides are host keys (`appWindowTitle`, `settingsLanguageTitle`,
`pluginScanFound`) to show that a plugin can override existing text and add new
text, and one fixture-only key. `examples/` is fixture only, so this plugin is
not a business plugin and its `fr.json` is not a translation deliverable.
