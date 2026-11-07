// Tests for the inline diagram card page helpers (P30). The pure functions —
// SVG sanitization, theme variables, and stage sizing — are covered here; the
// Pele render pipeline itself keeps the plugin's own 17 tests untouched.
import assert from "node:assert/strict";
import test from "node:test";

import {
  cardThemeVars,
  clampScale,
  sanitizeSvg,
  stageSize,
} from "./card.mjs";

const THEME = {
  Background: "#111315",
  Text: "#e8e6e3",
  Muted: "#9a9892",
  Surface: "#1c1e20",
  SurfaceAlt: "#26282a",
  Border: "#3a3c3e",
  Accent: "#205ea6",
  Danger: "#ce2c31",
  Chart: ["#205ea6", "#1fd8a4", "#ffca16", "#ce2c31", "#8a8a88"],
  FontFamily: '"PingFang SC", sans-serif',
  FontSize: 15,
  HighContrast: false,
};

test("sanitizeSvg removes script tags with and without bodies", () => {
  assert.equal(sanitizeSvg('<svg><script>alert(1)</script><rect/></svg>'), "<svg><rect/></svg>");
  assert.equal(sanitizeSvg('<svg><script src="x.js"/><rect/></svg>'), "<svg><rect/></svg>");
});

test("sanitizeSvg removes foreignObject embeds entirely", () => {
  const svg = '<svg><g/><foreignObject><div>HTML</div></foreignObject><path/></svg>';
  assert.equal(sanitizeSvg(svg), "<svg><g/><path/></svg>");
});

test("sanitizeSvg strips every on* attribute", () => {
  const svg = '<svg onload="x()"><rect onloadstart=\'y\' onmouseover=alert(1)/></svg>';
  const clean = sanitizeSvg(svg);
  assert.ok(!/\son[a-zA-Z]+/.test(clean), clean);
  assert.ok(clean.includes("<rect/>"), clean);
});

test("sanitizeSvg removes javascript: link targets", () => {
  const svg = '<svg><a href="javascript:alert(1)"><text>go</text></a>' +
    '<image xlink:href="javascript:alert(2)"/></svg>';
  const clean = sanitizeSvg(svg);
  assert.ok(!clean.includes("javascript:"), clean);
  assert.ok(clean.includes("<text>go</text>"), clean);
});

test("sanitizeSvg keeps ordinary diagram markup", () => {
  const svg = '<svg xmlns="http://www.w3.org/2000/svg">' +
    '<text class="label">客户需求</text><path d="M0 0"/></svg>';
  assert.equal(sanitizeSvg(svg), svg);
});

test("cardThemeVars maps the whitelisted --pele-* variables", () => {
  const vars = cardThemeVars(THEME);
  assert.equal(vars["--pele-bg"], "#111315");
  assert.equal(vars["--pele-fg"], "#e8e6e3");
  assert.equal(vars["--pele-line"], "#e8e6e3");
  assert.equal(vars["--pele-muted"], "#9a9892");
  assert.equal(vars["--pele-surface"], "#1c1e20");
  assert.equal(vars["--pele-surface-alt"], "#26282a");
  assert.equal(vars["--pele-border"], "#3a3c3e");
  assert.equal(vars["--pele-accent"], "#205ea6");
  assert.equal(vars["--pele-danger"], "#ce2c31");
  for (let i = 0; i < 5; i++) {
    assert.equal(vars["--pele-chart" + (i + 1)], THEME.Chart[i]);
  }
  assert.equal(vars["--pele-font"], "PingFang SC, sans-serif");
  assert.equal(vars["--pele-font-size"], "15px");
});

test("cardThemeVars in high contrast uses the text color for lines and borders", () => {
  const vars = cardThemeVars({ ...THEME, HighContrast: true });
  assert.equal(vars["--pele-fg"], THEME.Text);
  assert.equal(vars["--pele-line"], THEME.Text);
  assert.equal(vars["--pele-border"], THEME.Text);
  assert.equal(vars["--pele-muted"], THEME.Text);
  assert.equal(vars["--pele-bg"], THEME.Background);
});

test("cardThemeVars drops values that are not hex colors", () => {
  const vars = cardThemeVars({
    Background: "red; } svg{display:none}",
    Text: "inherit",
    Accent: "#00f",
  });
  assert.ok(!("--pele-bg" in vars));
  assert.ok(!("--pele-fg" in vars));
  assert.equal(vars["--pele-accent"], "#00f");
});

test("stageSize multiplies the natural size and stays at least 1px", () => {
  assert.deepEqual(stageSize(720, 480, 1), { width: 720, height: 480 });
  assert.deepEqual(stageSize(720, 480, 0.5), { width: 360, height: 240 });
  assert.deepEqual(stageSize(720, 480, 2), { width: 1440, height: 960 });
  assert.deepEqual(stageSize(0.4, 0.2, 1), { width: 1, height: 1 });
});

test("clampScale keeps zoom between 0.25 and 4", () => {
  assert.equal(clampScale(0.1), 0.25);
  assert.equal(clampScale(0.25), 0.25);
  assert.equal(clampScale(1), 1);
  assert.equal(clampScale(1.25), 1.25);
  assert.equal(clampScale(10), 4);
  assert.equal(clampScale(NaN), 1);
});

test("sanitizeSvg drops frames, embeds and objects", () => {
  assert.equal(sanitizeSvg('<svg><iframe src="x"></iframe><rect/></svg>'), "<svg><rect/></svg>");
  assert.equal(sanitizeSvg('<svg><embed src="x"/><object data="y"></object><rect/></svg>'), "<svg><rect/></svg>");
});

test("sanitizeSvg strips data: and javascript: style urls", () => {
  assert.equal(
    sanitizeSvg('<svg><rect style="fill:url(javascript:alert(1))"/></svg>').includes("javascript:"),
    false,
  );
  assert.equal(
    sanitizeSvg('<svg><image href="data:text/html,<script>1</script>"/></svg>').includes("data:"),
    false,
  );
});

test("sanitizeSvg strips url() references inside style blocks and use/animate", () => {
  assert.equal(sanitizeSvg('<svg><style>rect{fill:url(javascript:alert(1))}</style><rect/></svg>').includes("javascript:"), false);
  assert.equal(sanitizeSvg('<svg><animate attributeName="href" to="javascript:alert(1)"/></svg>').includes("javascript:"), false);
});
