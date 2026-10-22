const output = document.querySelector("#output");
const themeMode = document.querySelector("#theme-mode");
const localeMode = document.querySelector("#locale-mode");

const messages = {
  en: {
    pageTitle: "Hello WebView",
    eyebrow: "LANGUAGE PACK CONTRACT FIXTURE",
    heading: "Isolated WebView",
    lede: "This page is served by byop-plugin:// and reaches BYOP only through the identity-checked bridge.",
    themeTitle: "Theme bridge",
    localeTitle: "Locale bridge",
    localeDetail: "The manifest and this page receive language changes from the host.",
    getLocale: "Get locale",
    getTheme: "Get theme",
    hostCallsTitle: "Host and plugin calls",
    hostCallsDetail: "File access is intentionally undeclared, so a read request must be denied by the host.",
    showNotice: "Show notice",
    pingPlugin: "Ping plugin",
    readFile: "Read file",
    navigationTitle: "Navigation lock",
    navigationDetail: "The host prevents top-level navigation away from this plugin origin.",
    externalLink: "Try an external link",
    ready: "Ready.",
    loading: "loading",
    unknown: "unknown",
  },
  "zh-Hans": {
    pageTitle: "Hello WebView",
    eyebrow: "语言包契约示例",
    heading: "隔离 WebView",
    lede: "此页面由 byop-plugin:// 提供，只能通过经过身份校验的桥接访问 BYOP。",
    themeTitle: "主题桥接",
    localeTitle: "语言桥接",
    localeDetail: "插件清单和此页面会接收宿主发来的语言变化。",
    getLocale: "读取语言",
    getTheme: "读取主题",
    hostCallsTitle: "宿主与插件调用",
    hostCallsDetail: "此插件故意未声明文件权限，因此读取文件必须被宿主拒绝。",
    showNotice: "显示通知",
    pingPlugin: "测试插件",
    readFile: "读取文件",
    navigationTitle: "导航限制",
    navigationDetail: "宿主禁止离开此插件来源的顶层导航。",
    externalLink: "尝试外部链接",
    ready: "就绪。",
    loading: "加载中",
    unknown: "未知",
  },
};

let currentLocale = "en";

function show(value) {
  output.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
}

function showError(error) {
  show(`${error.code ?? "error"}: ${error.message || String(error)}`);
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

window.byop.onTheme(applyTheme);
window.byop.onLocale(applyLocale);

document.querySelector("#get-locale").addEventListener("click", async () => {
  try {
    const result = await window.byop.call("host.getLocale");
    applyLocale(result.locale);
    show(result);
  } catch (error) {
    showError(error);
  }
});

document.querySelector("#get-theme").addEventListener("click", async () => {
  try {
    show(await window.byop.call("host.getTheme"));
  } catch (error) {
    showError(error);
  }
});

document.querySelector("#show-notice").addEventListener("click", async () => {
  try {
    show(await window.byop.call("host.showNotice", {
      text: pageMessages(currentLocale).showNotice,
      tone: "info",
    }));
  } catch (error) {
    showError(error);
  }
});

document.querySelector("#ping-plugin").addEventListener("click", async () => {
  try {
    show(await window.byop.postMessage({
      kind: "ping",
      text: pageMessages(currentLocale).pageTitle,
    }));
  } catch (error) {
    showError(error);
  }
});

window.byop.call("host.getLocale").then((result) => {
  applyLocale(result.locale);
}).catch((error) => {
  applyLocale(window.byop.locale);
  showError(error);
});

document.querySelector("#read-file").addEventListener("click", async () => {
  try {
    show(await window.byop.call("host.readFile", { path: "README.md" }));
  } catch (error) {
    showError(error);
  }
});
