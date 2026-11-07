package main

import (
	"embed"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// The inline diagram card (P30) renders Pele SVG inside the conversation. The
// card page is served over a custom scheme that offers only the fixed files
// embedded below: the card's own assets plus read-only embeds of the Mermaid
// plugin's renderer and Pele bundle, which stay untouched in the plugin
// directory (the SHA recorded in THIRD_PARTY_NOTICES.md still applies).
const (
	diagramWebViewScheme = "byop-diagram"
	// diagramCardURL is the only page the scheme serves: the card shell loads
	// card.mjs as a module, which imports the two embedded plugin files.
	diagramCardURL = "byop-diagram://card/card.html"
	// diagramCardCSP keeps the page offline: scripts and styles come from the
	// scheme only, the rendered SVG is inline, and nothing connects out.
	diagramCardCSP = "default-src 'none'; script-src byop-diagram:; " +
		"style-src byop-diagram: 'unsafe-inline'; img-src data:; font-src 'none'; " +
		"connect-src 'none'; base-uri 'none'; form-action 'none'"
)

var (
	//go:embed assets/diagram
	diagramCardFS embed.FS

	//go:embed plugins/mermaid-diagram/web/diagram-renderer.mjs
	diagramRendererSource string

	//go:embed plugins/mermaid-diagram/web/vendor/pele.mjs
	diagramPeleSource string
)

// diagramSchemeFiles is the whole URL surface of the scheme. Everything else
// is 404, so the card page cannot pull in plugin files it should not have.
var diagramSchemeFiles = map[string]struct {
	body  string
	kind  string
	isDir bool
}{
	"/card.html":            {kind: "text/html; charset=utf-8"},
	"/card.mjs":             {kind: "text/javascript; charset=utf-8"},
	"/card.css":             {kind: "text/css; charset=utf-8"},
	"/diagram-renderer.mjs": {body: diagramRendererSource, kind: "text/javascript; charset=utf-8"},
	"/vendor/pele.mjs":      {body: diagramPeleSource, kind: "text/javascript; charset=utf-8"},
}

type diagramSchemeHandler struct{}

func newDiagramSchemeHandler() http.Handler {
	return diagramSchemeHandler{}
}

func (h diagramSchemeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setDiagramCardHeaders(w)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		diagramCardNotFound(w)
		return
	}
	relative, ok := parseDiagramCardURL(r.URL)
	if !ok {
		diagramCardNotFound(w)
		return
	}
	file, ok := diagramSchemeFiles[relative]
	if !ok {
		diagramCardNotFound(w)
		return
	}
	w.Header().Set("Content-Type", file.kind)
	body := file.body
	if body == "" {
		body, ok = readDiagramCardAsset(relative)
		if !ok {
			diagramCardNotFound(w)
			return
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, body)
}

// readDiagramCardAsset reads one of the card's own files from the embedded
// directory. The map above fixes which paths exist; this only loads bytes.
func readDiagramCardAsset(relative string) (string, bool) {
	name := strings.TrimPrefix(relative, "/")
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	data, err := diagramCardFS.ReadFile("assets/diagram/" + name)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func setDiagramCardHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", diagramCardCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
}

// parseDiagramCardURL accepts exactly one host and clean, single-level paths
// without query, fragment, or escapes. The scheme serves a fixed whitelist, so
// the URL carries nothing else.
func parseDiagramCardURL(u *url.URL) (string, bool) {
	if u == nil || !strings.EqualFold(u.Scheme, diagramWebViewScheme) ||
		u.Hostname() != "card" || u.Host != u.Hostname() || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.EscapedPath(), "%") ||
		strings.Contains(u.Path, `\`) {
		return "", false
	}
	rawPath := u.Path
	if !strings.HasPrefix(rawPath, "/") || strings.HasSuffix(rawPath, "/") {
		return "", false
	}
	relative := strings.TrimPrefix(rawPath, "/")
	if path.Clean(relative) != relative {
		return "", false
	}
	return "/" + relative, true
}

func diagramCardNotFound(w http.ResponseWriter) {
	http.Error(w, "Not found", http.StatusNotFound)
}
