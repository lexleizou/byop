const output = document.querySelector("#output");
const themeMode = document.querySelector("#theme-mode");
const localeMode = document.querySelector("#locale-mode");
const bridge = window.byop;

const messages = {
  en: {
    pageTitle: "Hello WebView",
    eyebrow: "THEME AND LOCALE FIXTURE",
    heading: "Isolated WebView",
    lede: "Reads host theme and locale through the isolated bridge.",
    themeTitle: "Theme bridge",
    themeStatus: "Host colors active",
    surface: "surface",
    text: "text",
    accent: "accent",
    success: "success",
    localeTitle: "Locale bridge",
    localeDetail: "The page follows host language changes.",
    getLocale: "Get locale",
    getTheme: "Get theme",
    hostCallsTitle: "Host and plugin calls",
    hostCallsDetail: "Undeclared file access must be denied.",
    showNotice: "Show notice",
    pingPlugin: "Ping plugin",
    readFile: "Read file",
    navigationTitle: "Navigation lock",
    navigationDetail: "Top-level navigation stays on this plugin origin.",
    externalLink: "Try an external link",
    ready: "Ready.",
    loading: "loading",
    unknown: "unknown",
  },
  "zh-Hans": {
    pageTitle: "Hello WebView",
    eyebrow: "主题与语言示例",
    heading: "隔离 WebView",
    lede: "通过隔离桥接读取宿主主题与语言。",
    themeTitle: "主题桥接",
    themeStatus: "宿主颜色已生效",
    surface: "表面",
    text: "文字",
    accent: "强调",
    success: "成功",
    localeTitle: "语言桥接",
    localeDetail: "页面跟随宿主语言变化。",
    getLocale: "读取语言",
    getTheme: "读取主题",
    hostCallsTitle: "宿主与插件调用",
    hostCallsDetail: "未声明的文件访问必须被拒绝。",
    showNotice: "显示通知",
    pingPlugin: "测试插件",
    readFile: "读取文件",
    navigationTitle: "导航限制",
    navigationDetail: "顶层导航保持在插件来源内。",
    externalLink: "尝试外部链接",
    ready: "就绪。",
    loading: "加载中",
    unknown: "未知",
  },
};

let currentLocale = "en";

function show(value) {
  output.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
  output.dataset.state = "ready";
}

function showError(error) {
  show(`${error.code ?? "error"}: ${error.message || String(error)}`);
  output.dataset.state = "error";
}

function applyTheme(theme) {
  themeMode.textContent = theme?.mode || messages[currentLocale].unknown;
}

function pageMessages(locale) {
  return messages[locale] || messages.en;
}

function applyLocale(locale) {
  currentLocale = typeof locale === "string" && messages[locale] ? locale : "en";
  const copy = pageMessages(currentLocale);
  document.documentElement.lang = currentLocale;
  document.title = copy.pageTitle;
  for (const element of document.querySelectorAll("[data-i18n]")) {
    const value = copy[element.dataset.i18n];
    if (value) element.textContent = value;
  }
  localeMode.textContent = currentLocale;
  applyTheme(window.byop.theme);
}

if (bridge) {
  bridge.onTheme(applyTheme);
  bridge.onLocale(applyLocale);
} else {
  applyLocale(document.documentElement.lang);
}

document.querySelector("#get-locale").addEventListener("click", async () => {
  try {
    const result = bridge
      ? await bridge.call("host.getLocale")
      : { locale: currentLocale, preview: true };
    applyLocale(result.locale);
    show(result);
  } catch (error) {
    showError(error);
  }
});

document.querySelector("#get-theme").addEventListener("click", async () => {
  try {
    show(bridge ? await bridge.call("host.getTheme") : { preview: true });
  } catch (error) {
    showError(error);
  }
});

document.querySelector("#show-notice").addEventListener("click", async () => {
  try {
    const result = bridge ? await bridge.call("host.showNotice", {
      text: pageMessages(currentLocale).showNotice,
      tone: "info",
    }) : { preview: true };
    show(result);
  } catch (error) {
    showError(error);
  }
});

document.querySelector("#ping-plugin").addEventListener("click", async () => {
  try {
    const result = bridge ? await bridge.postMessage({
      kind: "ping",
      text: pageMessages(currentLocale).pageTitle,
    }) : { preview: true };
    show(result);
  } catch (error) {
    showError(error);
  }
});

if (bridge) {
  bridge.call("host.getLocale").then((result) => {
    applyLocale(result.locale);
  }).catch((error) => {
    applyLocale(bridge.locale);
    showError(error);
  });
}

document.querySelector("#read-file").addEventListener("click", async () => {
  try {
    if (!bridge) {
      throw { code: "preview", message: "The host bridge is unavailable." };
    }
    show(await bridge.call("host.readFile", { path: "README.md" }));
  } catch (error) {
    showError(error);
  }
});
