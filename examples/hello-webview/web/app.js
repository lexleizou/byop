const output = document.querySelector("#output");
const themeMode = document.querySelector("#theme-mode");

function show(value) {
  output.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
}

function showError(error) {
  show(`${error.code ?? "error"}: ${error.message || String(error)}`);
}

function applyTheme(theme) {
  themeMode.textContent = theme?.mode || "unknown";
}

window.byop.onTheme(applyTheme);

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
      text: "Hello from the isolated WebView.",
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
      text: "Hello from the page",
    }));
  } catch (error) {
    showError(error);
  }
});

document.querySelector("#read-file").addEventListener("click", async () => {
  try {
    show(await window.byop.call("host.readFile", { path: "README.md" }));
  } catch (error) {
    showError(error);
  }
});
