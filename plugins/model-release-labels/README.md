# Model Release Labels

BYOP provides a manual badge editor for this plugin. Choose a model from the current Pi model catalog, enter the badge you want, and save it. A badge is a personal label; it is not verification of the model release serving a request.

## Use

1. Open **Plugins**, select **Model Release Labels**, and enable the plugin.
2. Refresh the model list if needed, choose the exact provider/model entry, and enter a badge of 1–12 printable characters.
3. Save. The composer model chip updates immediately. Select a saved entry to edit or remove it.

The editor uses the models reported by the connected external Pi process. BYOP does not maintain a separate model catalog or copy model credentials. A disconnected catalog does not delete saved badges.

## Identity and persistence

| Property | Behaviour |
|---|---|
| Model match | Exact provider ID and model ID, with case preserved |
| Badge storage | `<BYOP_CONFIG_DIR>/plugin-data/local.model-release-labels/manual-model-labels.json`; the normal config root is `~/.byop` |
| Save | Atomic file replacement; saved entries survive app restarts |
| Disable | Hide the badge while retaining saved entries |
| Uninstall | Retain plugin data for a later reinstall |
| Background worker | None; the host handles editing and display |

An empty badge is not a valid saved entry; use Remove to delete a mapping. A damaged saved-label file is reported by the editor and must not be silently overwritten.

## Compatibility with the previous manifest

The old `contributes.modelLabels` entries remain in this plugin manifest for compatibility with older hosts and to preserve existing local edits. The new manual editor ignores those legacy rules for `local.model-release-labels`, so deleting a saved badge does not reveal an old fixed `1017` marker. Other plugins may still use the existing declarative model-label contract.

## Install

Use **Plugins → Add from folder** and select this directory, or copy the directory into a configured plugin root and rescan. The repository copy is source; an installed copy is a separate working artifact. The manual editor requires a host containing the P23 changes; older hosts continue to use the legacy manifest rules.
