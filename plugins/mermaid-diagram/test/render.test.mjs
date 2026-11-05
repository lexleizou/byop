import assert from "node:assert/strict";
import test from "node:test";

import {
  MAX_SOURCE_CHARS,
  bakeTheme,
  rethemeSvg,
  renderDiagram,
  themeVars,
  toDataUrl,
  validateSource,
} from "../web/diagram-renderer.mjs";

const THEME = {
  colors: {
    background: "#ffffff",
    surface: "#f6f6f5",
    surfaceHover: "#eeeeec",
    text: "#1f1f1f",
    muted: "#6e6e6e",
    border: "#cecdc3",
    accent: "#205ea6",
  },
  fontFamily: { body: '"PingFang SC", sans-serif' },
};

test("renders a Chinese flowchart into a data URL", () => {
  const result = renderDiagram(`flowchart LR
  A[客户需求] --> B{方案判断}
  B -- 可复用 --> C[WebView 图解渲染]
  B -- 不可复用 --> D[仅作产品参考]
  C --> E([主题化 SVG])`, THEME);
  assert.equal(result.ok, true);
  assert.equal(result.type, "flowchart");
  assert.ok(result.width > 0 && result.height > 0);
  assert.ok(result.svg.startsWith("<svg"));
  assert.ok(result.dataUrl.startsWith("data:image/svg+xml;charset=utf-8,"));
});

test("renders a Chinese sequence diagram", () => {
  const result = renderDiagram(`sequenceDiagram
  participant 前端
  participant 服务
  前端 ->> 服务: 渲染请求
  服务 -->> 前端: SVG 结果`, THEME);
  assert.equal(result.ok, true);
  assert.equal(result.type, "sequence");
});

test("renders an agentflow diagram", () => {
  const result = renderDiagram(`agentflow-beta TB
  brief["用户任务"]@{ shape: input }
  flow planner["规划 Agent"]
    plan["拆解任务"]@{ shape: task }
    gate["可执行?"]@{ shape: decision }
    plan --> gate
  end
  publish["输出或等待确认"]@{ shape: action }
  brief --> planner
  planner --> publish`, THEME);
  assert.equal(result.ok, true);
});

test("invalid source fails without throwing and keeps position info", () => {
  const result = renderDiagram("flowchart LR\n  A[", THEME);
  assert.equal(result.ok, false);
  assert.ok(["syntax", "semantic"].includes(result.code));
  assert.ok(result.message.length > 0);
});

test("validates empty, over-long, and frontmatter input", () => {
  assert.deepEqual(validateSource("   "), { ok: false, code: "empty" });
  assert.deepEqual(validateSource("A".repeat(MAX_SOURCE_CHARS + 1)), {
    ok: false,
    code: "too_long",
    max: MAX_SOURCE_CHARS,
  });
  assert.deepEqual(validateSource("---\nconfig:\n  theme: dark\n---\nflowchart LR\n  A-->B"), {
    ok: false,
    code: "init_config",
  });
  assert.deepEqual(validateSource("%%{init: {'theme':'dark'}}%%\nflowchart LR\n  A-->B"), {
    ok: false,
    code: "init_config",
  });
});

test("frontmatter and init are rejected by renderDiagram too", () => {
  assert.equal(
    renderDiagram("---\nconfig:\n  theme: dark\n---\nflowchart LR\n  A-->B", THEME).code,
    "init_config",
  );
  assert.equal(
    renderDiagram("%%{init: {'theme':'dark'}}%%\nflowchart LR\n  A-->B", THEME).code,
    "init_config",
  );
});

test("theme variables are whitelisted, baked, and re-baked once", () => {
  const vars = themeVars({
    colors: { background: "#ff0000", text: "red; } svg{display:none}", accent: "#00f" },
  });
  assert.equal(vars["--pele-bg"], "#ff0000");
  assert.equal(vars["--pele-accent"], "#00f");
  assert.ok(!("--pele-fg" in vars));
  const baked = bakeTheme(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`, vars);
  assert.ok(baked.includes("<style>svg{--pele-bg:#ff0000;--pele-accent:#00f}</style>"));
  const dataUrl = rethemeSvg(baked, THEME);
  assert.ok(dataUrl.startsWith("data:image/svg+xml;charset=utf-8,"));
  const retheme = decodeURIComponent(
    dataUrl.slice("data:image/svg+xml;charset=utf-8,".length),
  );
  assert.ok(retheme.includes("--pele-bg:#ffffff"));
  assert.ok(!retheme.includes("#ff0000"));
  assert.equal(retheme.match(/<style>/g).length, 1);
});

test("toDataUrl round-trips the SVG through an encoding-safe URL", () => {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg">#?%<>&</svg>`;
  const decoded = decodeURIComponent(toDataUrl(svg).slice("data:image/svg+xml;charset=utf-8,".length));
  assert.equal(decoded, svg);
});
