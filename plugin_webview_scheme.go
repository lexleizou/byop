package main

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	pluginWebViewScheme       = "byop-plugin"
	maxPluginWebViewFileBytes = 5 << 20
)

type pluginWebViewSchemeHandler struct {
	state *appState
}

func newPluginWebViewSchemeHandler(state *appState) http.Handler {
	return &pluginWebViewSchemeHandler{state: state}
}

func (h *pluginWebViewSchemeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setPluginWebViewResponseHeaders(w, nil)
	if h == nil || h.state == nil {
		pluginWebViewNotFound(w)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		pluginWebViewNotFound(w)
		return
	}
	pluginID, relative, ok := parsePluginWebViewURL(r.URL)
	if !ok {
		pluginWebViewNotFound(w)
		return
	}
	entry, ok := h.state.registeredPluginWebViewEntry(pluginID)
	if !ok {
		pluginWebViewNotFound(w)
		return
	}
	setPluginWebViewResponseHeaders(w, entry.Manifest)
	target, info, err := resolvePluginPath(entry.SourceDirectory, relative)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxPluginWebViewFileBytes {
		pluginWebViewNotFound(w)
		return
	}
	file, err := os.Open(target)
	if err != nil {
		pluginWebViewNotFound(w)
		return
	}
	defer file.Close()
	if info, err = file.Stat(); err != nil || !info.Mode().IsRegular() ||
		info.Size() > maxPluginWebViewFileBytes {
		pluginWebViewNotFound(w)
		return
	}

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(target)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, file)
}

func parsePluginWebViewURL(u *url.URL) (pluginID, relative string, ok bool) {
	if u == nil || !strings.EqualFold(u.Scheme, pluginWebViewScheme) ||
		u.Host == "" || u.Host != u.Hostname() || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.EscapedPath(), "%") {
		return "", "", false
	}
	pluginID = strings.ToLower(u.Hostname())
	if !pluginIDPattern.MatchString(pluginID) {
		return "", "", false
	}
	rawPath := u.Path
	if !strings.HasPrefix(rawPath, "/") || rawPath == "/" || strings.HasSuffix(rawPath, "/") ||
		strings.Contains(rawPath, `\`) {
		return "", "", false
	}
	relative = strings.TrimPrefix(rawPath, "/")
	if !safePluginRelativePath(relative) || path.Clean(relative) != relative {
		return "", "", false
	}
	return pluginID, relative, true
}

func setPluginWebViewResponseHeaders(w http.ResponseWriter, manifest *pluginManifest) {
	connectSource := "'none'"
	if manifest != nil && manifest.Capabilities.Network {
		connectSource = "https:"
	}
	w.Header().Set("Content-Security-Policy", pluginWebViewCSP(connectSource))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
}

func pluginWebViewCSP(connectSource string) string {
	return "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; font-src 'self'; connect-src " + connectSource + "; " +
		"media-src 'self'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"
}

func pluginWebViewNotFound(w http.ResponseWriter) {
	http.Error(w, "Not found", http.StatusNotFound)
}
