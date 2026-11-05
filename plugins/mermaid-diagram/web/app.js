// Page wiring only: locale and theme bridges, render button, zoom controls.
// The pure pipeline lives in diagram-renderer.mjs. All user-visible copy is
// loaded from l10n/<locale>.json through the plugin worker (or fetch in a
// bridge-less browser preview) and applied with textContent.
const dom = {
  source: document.querySelector("#source"),
  renderButton: document.querySelector("#render-button"),
  status: document.querySelector("#status"),
  preview: document.querySelector("#preview"),
  previewEmpty: document.querySelector("#preview-empty"),
  zoomOut: document.querySelector("#zoom-out"),
  zoomIn: document.querySelector("#zoom-in"),
  zoomLevel: document.querySelector("#zoom-level"),
  fitWidth: document.querySelector("#fit-width"),
  zoomReset: document.querySelector("#zoom-reset"),
};

const bridge = window.byop;
const state = {
  locale: "en",
  bundle: {},
  localeCache: new Map(),
  theme: bridge && bridge.theme ? bridge.theme : null,
  lastResult: null,
  fitWidth: true,
  zoomLevel: 1,
};
const ZOOM_MIN = 0.25;
const ZOOM_MAX = 4;
const ZOOM_STEP = 1.25;

let rendererPromise = null;
function loadRenderer() {
  if (!rendererPromise) {
    rendererPromise = import("./diagram-renderer.mjs").catch((error) => {
      rendererPromise = null; // a transient failure must not poison retries
      throw error;
    });
  }
  return rendererPromise;
}

function t(key, params) {
  let text = state.bundle[key] || key;
  if (params) {
    for (const [name, value] of Object.entries(params)) {
      text = text.split(`{${name}}`).join(String(value));
    }
  }
  return text;
}

async function fetchLocale(locale) {
  if (state.localeCache.has(locale)) {
    return state.localeCache.get(locale);
  }
  let messages = {};
  let sample = "";
  if (bridge && typeof bridge.postMessage === "function") {
    const result = await bridge.postMessage({ action: "locale", locale });
    if (result && result.messages && typeof result.messages === "object") {
      messages = result.messages;
    }
    if (typeof result?.sample === "string") {
      sample = result.sample;
    }
  } else {
    try {
      const response = await fetch(`../l10n/${locale}.json`);
      if (response.ok) {
        const parsed = await response.json();
        if (parsed && typeof parsed === "object") messages = parsed;
      }
    } catch {
      // Bridge-less preview without an HTTP server: fall back to keys.
    }
    try {
      const response = await fetch(`../samples/${locale}.txt`);
      if (response.ok) sample = (await response.text()).trimEnd();
    } catch {
      // A missing sample only leaves the editor empty.
    }
  }
  const entry = { messages, sample };
  state.localeCache.set(locale, entry);
  return entry;
}

async function applyLocale(locale) {
  const selected = locale === "zh-Hans" ? "zh-Hans" : "en";
  state.locale = selected;
  const entry = await fetchLocale(selected);
  state.bundle = entry.messages;
  document.documentElement.lang = selected;
  document.title = t("pageTitle");
  for (const element of document.querySelectorAll("[data-i18n]")) {
    element.textContent = t(element.dataset.i18n);
  }
  for (const element of document.querySelectorAll("[data-i18n-placeholder]")) {
    element.placeholder = t(element.dataset.i18nPlaceholder);
  }
  if (dom.source.value === "") {
    dom.source.value = entry.sample;
  }
  if (state.lastResult) {
    dom.status.textContent = renderStatus(state.lastResult);
    dom.preview.alt = t("diagramAlt");
  } else {
    setStatus(t("statusIdle"), "info");
  }
}

function renderStatus(result) {
  return t("statusRendered", {
    type: result.type,
    width: result.width,
    height: result.height,
  });
}

function setStatus(text, tone) {
  dom.status.textContent = text;
  dom.status.dataset.tone = tone || "info";
}

function describeFailure(failure) {
  switch (failure.code) {
    case "empty":
      return t("errorEmpty");
    case "too_long":
      return t("errorTooLong", { max: failure.max });
    case "init_config":
      return t("errorInitConfig");
    case "syntax":
    case "semantic":
    case "limit":
    case "unsupported-diagram": {
      const message = failure.message || t("errorUnknown");
      if (failure.line > 0) {
        return t("errorRender", { message: `${message} (L${failure.line}:C${failure.column})` });
      }
      return t("errorRender", { message });
    }
    default:
      return t("errorUnknown");
  }
}

function applyZoom() {
  const result = state.lastResult;
  dom.fitWidth.setAttribute("aria-pressed", state.fitWidth ? "true" : "false");
  dom.zoomLevel.textContent = `${Math.round(state.zoomLevel * 100)}%`;
  if (!result) return;
  if (state.fitWidth) {
    dom.preview.style.removeProperty("width");
    dom.preview.dataset.mode = "fit";
  } else {
    dom.preview.dataset.mode = "zoom";
    dom.preview.style.width = `${Math.round(result.width * state.zoomLevel)}px`;
  }
}

function setZoomControlsEnabled(enabled) {
  for (const control of [dom.zoomOut, dom.zoomIn, dom.fitWidth, dom.zoomReset]) {
    control.disabled = !enabled;
  }
}

async function renderSource() {
  dom.renderButton.disabled = true;
  dom.renderButton.textContent = t("renderingButton");
  try {
    const renderer = await loadRenderer();
    const result = renderer.renderDiagram(dom.source.value, state.theme);
    if (!result.ok) {
      state.lastResult = null;
      dom.preview.hidden = true;
      dom.preview.removeAttribute("src");
      dom.previewEmpty.hidden = false;
      setZoomControlsEnabled(false);
      setStatus(describeFailure(result), "error");
      return;
    }
    state.lastResult = result;
    dom.preview.src = result.dataUrl;
    dom.preview.alt = t("diagramAlt");
    dom.preview.hidden = false;
    dom.previewEmpty.hidden = true;
    state.zoomLevel = 1;
    setZoomControlsEnabled(true);
    applyZoom();
    setStatus(renderStatus(result), "info");
  } catch (error) {
    state.lastResult = null;
    dom.preview.hidden = true;
    dom.preview.removeAttribute("src");
    dom.previewEmpty.hidden = false;
    setZoomControlsEnabled(false);
    setStatus(t("errorRender", { message: String(error?.message || error) }), "error");
  } finally {
    dom.renderButton.disabled = false;
    dom.renderButton.textContent = t("renderButton");
  }
}

function changeZoom(factor) {
  state.fitWidth = false;
  state.zoomLevel = Math.min(
    ZOOM_MAX,
    Math.max(ZOOM_MIN, state.zoomLevel * factor),
  );
  applyZoom();
}

dom.renderButton.addEventListener("click", renderSource);
dom.source.addEventListener("keydown", (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
    event.preventDefault();
    renderSource();
  }
});
dom.zoomOut.addEventListener("click", () => changeZoom(1 / ZOOM_STEP));
dom.zoomIn.addEventListener("click", () => changeZoom(ZOOM_STEP));
dom.fitWidth.addEventListener("click", () => {
  state.fitWidth = true;
  state.zoomLevel = 1;
  applyZoom();
});
dom.zoomReset.addEventListener("click", () => {
  state.fitWidth = true;
  state.zoomLevel = 1;
  applyZoom();
});
setZoomControlsEnabled(false);

if (bridge) {
  bridge.onTheme((theme) => {
    state.theme = theme || null;
    if (state.lastResult) {
      loadRenderer().then((renderer) => {
        if (!state.lastResult) return;
        state.lastResult = {
          ...state.lastResult,
          dataUrl: renderer.rethemeSvg(state.lastResult.svg, state.theme),
        };
        dom.preview.src = state.lastResult.dataUrl;
      });
    }
  });
  bridge.onLocale((locale) => {
    applyLocale(locale);
  });
  bridge.call("host.getLocale")
    .then((result) => applyLocale(result.locale))
    .catch(() => applyLocale(bridge.locale));
} else {
  const guess = String(navigator.language || "en");
  applyLocale(guess.startsWith("zh") ? "zh-Hans" : "en");
}
