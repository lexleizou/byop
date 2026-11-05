// Pure rendering pipeline for the Mermaid plugin, importable from Node for
// tests and from the page for rendering. Pele renders synchronously to an SVG
// string; the page shows it as a data:image/svg+xml <img> because the host
// CSP is `img-src 'self' data:` (blob: is not allowed). A standalone SVG
// document cannot inherit the page's CSS custom properties, so the whitelisted
// host theme colors are baked into the SVG as `--pele-*` declarations before
// the data URL is built.
import { render } from "./vendor/pele.mjs";

export const MAX_SOURCE_CHARS = 20000;
const RENDER_MAX_WIDTH = 1400;
const ID_PREFIX = "byop-mermaid";
const HEX_COLOR = /^#[0-9a-fA-F]{3,8}$/;

// validateSource rejects empty input, over-long input, and init/config
// frontmatter, which the MVP does not pass through to Pele.
export function validateSource(source) {
  if (typeof source !== "string" || source.trim() === "") {
    return { ok: false, code: "empty" };
  }
  if (source.length > MAX_SOURCE_CHARS) {
    return { ok: false, code: "too_long", max: MAX_SOURCE_CHARS };
  }
  const trimmed = source.trim();
  if (trimmed.startsWith("---") || source.includes("%%{")) {
    return { ok: false, code: "init_config" };
  }
  return { ok: true };
}

// safeFont keeps the family list inside a CSS declaration value: quotes and
// braces could break out of the injected <style> block.
function safeFont(family) {
  if (typeof family !== "string") return "";
  const cleaned = family.replace(/[{};"'<>]/g, "").trim();
  return cleaned.slice(0, 200);
}

// themeVars maps the BYOP theme payload to Pele's whitelisted variables.
export function themeVars(theme) {
  const colors = theme && theme.colors ? theme.colors : {};
  const vars = {
    "--pele-bg": colors.background,
    "--pele-fg": colors.text,
    "--pele-muted": colors.muted,
    "--pele-line": colors.text,
    "--pele-surface": colors.surface,
    "--pele-surface-alt": colors.surfaceHover || colors.surface,
    "--pele-border": colors.border,
    "--pele-accent": colors.accent,
  };
  const accepted = {};
  for (const [name, value] of Object.entries(vars)) {
    if (typeof value === "string" && HEX_COLOR.test(value)) {
      accepted[name] = value;
    }
  }
  const font = safeFont(theme?.fontFamily?.body);
  if (font) accepted["--pele-font"] = font;
  return accepted;
}

// bakeTheme inserts the theme variables into the SVG root so the standalone
// image resolves `var(--pele-*)` against the host colors.
export function bakeTheme(svg, vars) {
  const declarations = Object.entries(vars)
    .map(([name, value]) => `${name}:${value}`)
    .join(";");
  const style = declarations ? `<style>svg{${declarations}}</style>` : "";
  return svg.replace(/<svg[^>]*>/, (openTag) => `${openTag}${style}`);
}

export function toDataUrl(svg) {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

// renderDiagram is the single entry the page calls. It returns a structured
// result; Pele failures never throw so the caller only wires UI states.
export function renderDiagram(source, theme) {
  const validation = validateSource(source);
  if (!validation.ok) {
    return { ok: false, code: validation.code, max: validation.max };
  }
  const vars = themeVars(theme);
  const options = {
    idPrefix: ID_PREFIX,
    maxWidth: RENDER_MAX_WIDTH,
  };
  if (vars["--pele-font"]) options.fontFamily = vars["--pele-font"];
  try {
    const result = render(source.trim(), options);
    const baked = bakeTheme(result.svg, vars);
    return {
      ok: true,
      type: result.type,
      mermaidType: result.mermaidType,
      width: result.width,
      height: result.height,
      svg: baked,
      dataUrl: toDataUrl(baked),
    };
  } catch (error) {
    return {
      ok: false,
      code: error?.code || "syntax",
      message: String(error?.message || error),
      line: typeof error?.line === "number" ? error.line : 0,
      column: typeof error?.column === "number" ? error.column : 0,
    };
  }
}

// rethemeSvg applies a new theme to an already rendered SVG without running
// the source again: the theme block is replaced inside the existing markup.
export function rethemeSvg(bakedSvg, theme) {
  const vars = themeVars(theme);
  const declarations = Object.entries(vars)
    .map(([name, value]) => `${name}:${value}`)
    .join(";");
  const withoutStyle = bakedSvg.replace(/<style>svg\{[^]*?\}<\/style>/, "");
  return toDataUrl(bakeTheme(withoutStyle, vars));
}
