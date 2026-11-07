// The inline diagram card page (P30). The host drives the page through
// window.byopDiagram only: render(source, theme) parses and renders once with
// Pele (max width 720), setTheme swaps CSS variables without re-parsing, and
// setScale applies zoom with drag-to-pan while zoomed in. The page has no
// host bridge; the SVG is sanitized and inlined so its text stays selectable
// and the CSS variables theme it. validateSource and Pele load lazily so the
// pure helpers stay importable from the node tests without a bundled module
// graph; the Go handler serves both files at their imported paths.

const RENDER_MAX_WIDTH = 720;
const HEX_COLOR = /^#[0-9a-fA-F]{3,8}$/;
const MIN_SCALE = 0.25;
const MAX_SCALE = 4;

// sanitizeSvg strips what a rendered diagram must never carry before the SVG
// is inlined into the card: scripts, foreignObject embeds, every on*
// attribute, and javascript: link targets. It is a pure string function so
// the node tests run it without a DOM.
export function sanitizeSvg(svg) {
  if (typeof svg !== "string") return "";
  return svg
    .replace(/<script\b[^>]*\/>/gi, "")
    .replace(/<script[\s\S]*?<\/script\s*>/gi, "")
    .replace(/<script[\s\S]*$/i, "")
    .replace(/<foreignObject[\s\S]*?<\/foreignObject\s*>/gi, "")
    .replace(/<foreignObject\b[^>]*\/>/gi, "")
    // Frames and embeds load a second document; an SVG has no use for them.
    .replace(/<iframe\b[^>]*\/>/gi, "")
    .replace(/<iframe[\s\S]*?<\/iframe\s*>/gi, "")
    .replace(/<embed\b[^>]*>/gi, "")
    .replace(/<object[\s\S]*?<\/object\s*>/gi, "")
    .replace(/<object\b[^>]*\/>/gi, "")
    // A data: URL can carry a whole HTML document.
    .replace(/\s(href|xlink:href|src|action)\s*=\s*"\s*data:[^"]*"/gi, "")
    .replace(/\s(href|xlink:href|src|action)\s*=\s*'\s*data:[^']*'/gi, "")
    .replace(/\s(href|xlink:href|src|action)\s*=\s*data:[^\s>]+/gi, "")
    .replace(/\s(on[a-zA-Z]+)\s*=\s*"[^"]*"/gi, "")
    .replace(/\s(on[a-zA-Z]+)\s*=\s*'[^']*'/gi, "")
    .replace(/\s(on[a-zA-Z]+)\s*=\s*([^\s>]+?)(?=\s*\/?\s*>|\s|$)/gi, "")
    .replace(/\s(href|xlink:href|src|action)\s*=\s*"\s*javascript:[^"]*"/gi, "")
    .replace(/\s(href|xlink:href|src|action)\s*=\s*'\s*javascript:[^']*'/gi, "")
    .replace(/\s(href|xlink:href|src|action)\s*=\s*javascript:[^\s>]+/gi, "")
    // A style attribute can still fetch: url(javascript:…) and expressions.
    .replace(/\sstyle\s*=\s*"[^"]*url\s*\(\s*['"]?\s*(javascript|data):[^"]*"/gi, "")
    .replace(/\sstyle\s*=\s*'[^']*url\s*\(\s*['"]?\s*(javascript|data):[^']*'/gi, "")
    // <style> blocks and <use>/<animate>/<set> can reference a URL too, in any
    // attribute, so a bare javascript:/data: value is dropped wherever it
    // appears rather than only on href/src/action.
    .replace(/url\(\s*['"]?\s*(javascript|data):[^)]*\)/gi, "")
    .replace(/\s[a-zA-Z:_-]+\s*=\s*"\s*(javascript|data):[^"]*"/gi, "")
    .replace(/\s[a-zA-Z:_-]+\s*=\s*'\s*(javascript|data):[^']*'/gi, "");
}

// cardThemeVars maps the host's inlineDiagramTheme to the --pele-* variables
// the renderer's SVG expects. Only hex colors pass, so a malformed host value
// can never write a selector or expression into the page. High contrast
// replaces line and muted colors with the text color for 3:1 graphics.
export function cardThemeVars(theme) {
  const t = theme || {};
  const text = typeof t.Text === "string" && HEX_COLOR.test(t.Text) ? t.Text : "";
  const vars = {
    "--pele-bg": t.Background,
    "--pele-fg": t.HighContrast && text ? text : t.Text,
    "--pele-line": t.HighContrast && text ? text : t.Text,
    "--pele-muted": t.HighContrast && text ? text : t.Muted,
    "--pele-surface": t.Surface,
    "--pele-surface-alt": t.SurfaceAlt,
    "--pele-border": t.HighContrast && text ? text : t.Border,
    "--pele-accent": t.Accent,
    "--pele-danger": t.Danger,
  };
  const chart = Array.isArray(t.Chart) ? t.Chart : [];
  for (let i = 0; i < 5; i++) {
    vars["--pele-chart" + (i + 1)] = chart[i];
  }
  const accepted = {};
  for (const [name, value] of Object.entries(vars)) {
    if (typeof value === "string" && HEX_COLOR.test(value)) accepted[name] = value;
  }
  if (typeof t.FontFamily === "string" && t.FontFamily.trim()) {
    accepted["--pele-font"] = t.FontFamily.replace(/[{};"'<>]/g, "").trim();
  }
  if (typeof t.FontSize === "number" && t.FontSize > 0) {
    accepted["--pele-font-size"] = t.FontSize + "px";
  }
  return accepted;
}

// stageSize sizes the stage wrapper so the SVG, transformed with scale, fills
// exactly the box the scroll container can pan over. Host clamps the body
// height before layout; the page only multiplies.
export function stageSize(naturalW, naturalH, scale) {
  const w = Math.max(1, Math.round(naturalW * scale));
  const h = Math.max(1, Math.round(naturalH * scale));
  return { width: w, height: h };
}

// clampScale is the zoom range the spec fixes: 0.25 to 4.
export function clampScale(scale) {
  if (typeof scale !== "number" || !isFinite(scale)) return 1;
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, scale));
}

let naturalW = 0;
let naturalH = 0;
let currentScale = 1;
let theme = null;

function applyTheme(next) {
  theme = next || {};
  const root = document.documentElement;
  const vars = cardThemeVars(theme);
  root.style.setProperty("--pele-bg", vars["--pele-bg"] || "#ffffff");
  root.style.setProperty("--pele-fg", vars["--pele-fg"] || "#000000");
  for (const [name, value] of Object.entries(vars)) {
    root.style.setProperty(name, value);
  }
  root.style.setProperty(
    "--pele-font-size",
    vars["--pele-font-size"] || "15px"
  );
  root.dataset.highContrast = theme.HighContrast ? "true" : "false";
}

function applyScale(scale) {
  currentScale = clampScale(scale);
  const stage = document.getElementById("stage");
  const scroll = document.getElementById("scroll");
  if (!stage || !scroll) return { width: naturalW, height: naturalH };
  const box = stageSize(naturalW, naturalH, currentScale);
  stage.style.width = box.width + "px";
  stage.style.height = box.height + "px";
  stage.style.transform = "scale(" + currentScale + ")";
  stage.style.transformOrigin = "0 0";
  stage.dataset.scale = String(currentScale);
  return box;
}

// dragPan lets the user pan with the pointer while the diagram overflows, so
// zoomed-in cards do not depend on tiny scrollbars.
function dragPan(scroll) {
  let down = false;
  let lastX = 0;
  let lastY = 0;
  scroll.addEventListener("pointerdown", (event) => {
    if (scroll.scrollWidth <= scroll.clientWidth &&
        scroll.scrollHeight <= scroll.clientHeight) return;
    down = true;
    lastX = event.clientX;
    lastY = event.clientY;
    scroll.classList.add("dragging");
  });
  window.addEventListener("pointermove", (event) => {
    if (!down) return;
    scroll.scrollLeft += lastX - event.clientX;
    scroll.scrollTop += lastY - event.clientY;
    lastX = event.clientX;
    lastY = event.clientY;
  });
  window.addEventListener("pointerup", () => {
    down = false;
    scroll.classList.remove("dragging");
  });
}

function install() {
  const scroll = document.getElementById("scroll");
  if (scroll) dragPan(scroll);
  document.documentElement.addEventListener("click", (event) => {
    const link = event.target && event.target.closest
      ? event.target.closest("a[href]")
      : null;
    if (link && link.getAttribute("href")) {
      event.preventDefault();
      // The host blocks every navigation and opens http(s) itself; an
      // anchor that navigates is how the click reaches the host handler.
      location.href = link.getAttribute("href");
    }
  });
}

if (typeof document !== "undefined" && document.documentElement) {
  install();
}

export const byopDiagram = {
  async render(source, nextTheme) {
    applyTheme(nextTheme);
    try {
      const [{ validateSource }, { render }] = await Promise.all([
        import("./diagram-renderer.mjs"),
        import("./vendor/pele.mjs"),
      ]);
      const validation = validateSource(source);
      if (!validation.ok) {
        const messages = {
          empty: "empty source",
          too_long: "source is too long",
          init_config: "init/config frontmatter is not supported",
        };
        return { ok: false, error: messages[validation.code] || "invalid source", line: 0 };
      }
      const options = { idPrefix: "byop-inline", maxWidth: RENDER_MAX_WIDTH };
      const vars = cardThemeVars(theme || {});
      if (vars["--pele-font"]) options.fontFamily = vars["--pele-font"];
      const result = render(source.trim(), options);
      const clean = sanitizeSvg(result.svg);
      const stage = document.getElementById("stage");
      naturalW = result.width;
      naturalH = result.height;
      if (stage) {
        stage.innerHTML = clean;
        applyScale(1);
      }
      return {
        ok: true,
        width: result.width,
        height: result.height,
        agentflow: result.type === "agentflow",
      };
    } catch (error) {
      return {
        ok: false,
        error: String((error && error.message) || error),
        line: typeof error?.line === "number" ? error.line : 0,
      };
    }
  },
  setTheme(nextTheme) {
    applyTheme(nextTheme);
    return true;
  },
  setScale(scale) {
    return applyScale(scale);
  },
};

if (typeof window !== "undefined") {
  window.byopDiagram = byopDiagram;
}
