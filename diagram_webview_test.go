package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func serveDiagramCard(t *testing.T, rawURL string, method string) *httptest.ResponseRecorder {
	t.Helper()
	handler := newDiagramSchemeHandler()
	request := httptest.NewRequest(method, rawURL, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestDiagramSchemeHandlerServesWhitelist(t *testing.T) {
	response := serveDiagramCard(t, diagramCardURL, http.MethodGet)
	if response.Code != http.StatusOK {
		t.Fatalf("card.html status = %d", response.Code)
	}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("card.html content type = %q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected security headers: %#v", response.Header())
	}
	csp := response.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'none'", "script-src byop-diagram:", "style-src byop-diagram: 'unsafe-inline'",
		"img-src data:", "font-src 'none'", "connect-src 'none'",
		"base-uri 'none'", "form-action 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP %q lacks %q", csp, want)
		}
	}
	body := response.Body.String()
	if !strings.Contains(body, "card.mjs") || !strings.Contains(body, "card.css") {
		t.Fatalf("card.html body = %q", body)
	}
}

func TestDiagramSchemeHandlerContentTypes(t *testing.T) {
	tests := []struct {
		url  string
		kind string
	}{
		{"byop-diagram://card/card.mjs", "text/javascript; charset=utf-8"},
		{"byop-diagram://card/card.css", "text/css; charset=utf-8"},
		{"byop-diagram://card/diagram-renderer.mjs", "text/javascript; charset=utf-8"},
		{"byop-diagram://card/vendor/pele.mjs", "text/javascript; charset=utf-8"},
	}
	for _, test := range tests {
		response := serveDiagramCard(t, test.url, http.MethodGet)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d", test.url, response.Code)
		}
		if got := response.Header().Get("Content-Type"); got != test.kind {
			t.Fatalf("%s content type = %q, want %q", test.url, got, test.kind)
		}
	}
	// The embedded plugin renderer keeps its importable surface intact.
	if !strings.Contains(serveDiagramCard(t, "byop-diagram://card/diagram-renderer.mjs", http.MethodGet).Body.String(), "validateSource") {
		t.Fatal("embedded diagram-renderer.mjs lost validateSource")
	}
	if !strings.Contains(serveDiagramCard(t, "byop-diagram://card/vendor/pele.mjs", http.MethodGet).Body.String(), "agentflow") {
		t.Fatal("embedded pele.mjs lost the agentflow renderer")
	}
}

func TestDiagramSchemeHandlerRejectsUnsafeTargets(t *testing.T) {
	for _, rawURL := range []string{
		"byop-diagram://card/missing.html",
		"byop-diagram://card/vendor/missing.mjs",
		"byop-diagram://other/card/card.html",
		"byop-diagram://card/../card.html",
		"byop-diagram://card/%2e%2e/card.html",
		"byop-diagram://card/card.html?x=1",
		"byop-diagram://card/" + strings.Repeat("a", 300) + ".html",
		"byop-diagram://card",
	} {
		t.Run(rawURL, func(t *testing.T) {
			response := serveDiagramCard(t, rawURL, http.MethodGet)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", response.Code)
			}
		})
	}
	head := serveDiagramCard(t, diagramCardURL, http.MethodHead)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD response = %d %q", head.Code, head.Body.String())
	}
	post := serveDiagramCard(t, diagramCardURL, http.MethodPost)
	if post.Code != http.StatusNotFound {
		t.Fatalf("POST status = %d", post.Code)
	}
}

func newDiagramTestState() *appState {
	return &appState{inlineDiagrams: make(map[string]*inlineDiagram)}
}

func TestInlineDiagramForKeyAndTooLong(t *testing.T) {
	state := newDiagramTestState()
	first := state.inlineDiagramFor("flowchart LR\n  A-->B")
	if first == nil || first.Status != inlineDiagramIdle {
		t.Fatalf("first state = %#v", first)
	}
	again := state.inlineDiagramFor("flowchart LR\n  A-->B")
	if again != first {
		t.Fatal("same source returned a different state")
	}
	if first.Key != inlineDiagramKey(first.Source) {
		t.Fatalf("key %q does not match its source", first.Key)
	}
	long := strings.Repeat("A", inlineDiagramMaxSourceUnits+1)
	tooLong := state.inlineDiagramFor(long)
	if tooLong.Status != inlineDiagramTooLong {
		t.Fatalf("over-long source status = %v", tooLong.Status)
	}
	exact := state.inlineDiagramFor(strings.Repeat("A", inlineDiagramMaxSourceUnits))
	if exact.Status != inlineDiagramIdle {
		t.Fatalf("limit-length source status = %v", exact.Status)
	}
	if utf16CodeUnits("A日") != 2 {
		t.Fatal("utf16CodeUnits miscounts")
	}
}

func TestParseInlineDiagramResult(t *testing.T) {
	result, err := parseInlineDiagramResult(map[string]any{
		"ok": true, "width": float64(720), "height": float64(480), "agentflow": true,
	})
	if err != nil || !result.OK || result.Width != 720 || result.Height != 480 || !result.Agentflow {
		t.Fatalf("success parse = %#v, err %v", result, err)
	}
	failure, err := parseInlineDiagramResult(map[string]any{
		"ok": false, "error": "syntax", "line": float64(3),
	})
	if err != nil || failure.OK || failure.Error != "syntax" || failure.Line != 3 {
		t.Fatalf("failure parse = %#v, err %v", failure, err)
	}
	if _, err := parseInlineDiagramResult("nope"); err == nil {
		t.Fatal("non-object value parsed without error")
	}
	if _, err := parseInlineDiagramResult(nil); err == nil {
		t.Fatal("nil value parsed without error")
	}
}

func TestApplyInlineDiagramRenderTimeoutMarksFailed(t *testing.T) {
	originalAlive := inlineDiagramViewAlive
	inlineDiagramViewAlive = func(v *mygo.WebView) bool { return v != nil }
	t.Cleanup(func() { inlineDiagramViewAlive = originalAlive })
	state := newDiagramTestState()
	d := &inlineDiagram{Key: "k", Source: "flowchart LR\n  A-->B", renderSeq: 1}
	d.WebView = &mygo.WebView{}
	state.applyInlineDiagramRender(d, 1, nil, context.DeadlineExceeded)
	if d.Status != inlineDiagramFailed {
		t.Fatalf("timeout status = %v", d.Status)
	}
	if !strings.Contains(d.Err, "5s") {
		t.Fatalf("timeout error = %q", d.Err)
	}
	if d.ErrLine != 0 {
		t.Fatalf("timeout line = %d", d.ErrLine)
	}
	// A reply for an older generation is dropped, not applied.
	d.renderSeq = 2
	state.applyInlineDiagramRender(d, 1, nil, nil)
	if d.Status != inlineDiagramFailed {
		t.Fatal("stale reply overwrote a failed card")
	}
}

func TestApplyInlineDiagramRenderEvalErrorKeepsLine(t *testing.T) {
	originalAlive := inlineDiagramViewAlive
	inlineDiagramViewAlive = func(v *mygo.WebView) bool { return v != nil }
	t.Cleanup(func() { inlineDiagramViewAlive = originalAlive })
	state := newDiagramTestState()
	d := &inlineDiagram{Key: "k", Source: "s", renderSeq: 1}
	d.WebView = &mygo.WebView{}
	state.applyInlineDiagramRender(d, 1, nil, errors.New("boom"))
	if d.Status != inlineDiagramFailed || d.Err != "boom" {
		t.Fatalf("eval error state = %#v", d)
	}
	state.applyInlineDiagramRender(d, 1, map[string]any{
		"ok": false, "error": "PeleError", "line": float64(4),
	}, nil)
	if d.Status != inlineDiagramFailed || d.Err != "PeleError" || d.ErrLine != 4 {
		t.Fatalf("page failure state = %#v", d)
	}
}

func TestApplyInlineDiagramRenderSuccessAndCache(t *testing.T) {
	originalAlive := inlineDiagramViewAlive
	originalRelease := inlineDiagramReleaseView
	inlineDiagramViewAlive = func(v *mygo.WebView) bool { return v != nil }
	inlineDiagramReleaseView = func(v *mygo.WebView) {}
	t.Cleanup(func() {
		inlineDiagramViewAlive = originalAlive
		inlineDiagramReleaseView = originalRelease
	})
	state := newDiagramTestState()
	d := &inlineDiagram{Key: "k", Source: "s", Status: inlineDiagramLoading, renderSeq: 1}
	d.WebView = &mygo.WebView{}
	state.applyInlineDiagramRender(d, 1, map[string]any{
		"ok": true, "width": float64(300), "height": float64(120), "agentflow": false,
	}, nil)
	if d.Status != inlineDiagramReady || d.NaturalW != 300 || d.NaturalH != 120 || d.Agentflow {
		t.Fatalf("success state = %#v", d)
	}
	// Releasing the view keeps the result as a cache: the natural size and the
	// rendered status survive, so the card shows the diagram again instead of
	// claiming to still be working. Only an unfinished render goes back to
	// idle. ensureInlineDiagram rebuilds the view for a ready card.
	state.releaseInlineDiagram(d)
	if d.WebView != nil || d.Status != inlineDiagramReady || d.NaturalW != 300 || d.NaturalH != 120 {
		t.Fatalf("released state = %#v", d)
	}
}

func TestEnsureWithoutWindowMarksFailed(t *testing.T) {
	state := newDiagramTestState()
	d := state.inlineDiagramFor("flowchart LR\n  A-->B")
	state.ensureInlineDiagram(d, inlineDiagramTheme{Background: "#111315"}, 1)
	if d.Status != inlineDiagramFailed || d.Err == "" {
		t.Fatalf("no-window ensure state = %#v", d)
	}
	// A failed card never retries, and a too-long card never builds a view.
	state.ensureInlineDiagram(d, inlineDiagramTheme{}, 2)
	if d.Status != inlineDiagramFailed {
		t.Fatal("failed card re-ensured")
	}
	long := state.inlineDiagramFor(strings.Repeat("B", inlineDiagramMaxSourceUnits+1))
	state.ensureInlineDiagram(long, inlineDiagramTheme{}, 2)
	if long.Status != inlineDiagramTooLong || long.WebView != nil {
		t.Fatalf("too long state = %#v", long)
	}
}

func TestMaxInlineDiagramViewsAndLRURelease(t *testing.T) {
	originalFactory := inlineDiagramViewFactory
	originalAlive := inlineDiagramViewAlive
	originalRelease := inlineDiagramReleaseView
	inlineDiagramViewFactory = func(s *appState, d *inlineDiagram, theme inlineDiagramTheme, gen uint64) (*mygo.WebView, error) {
		return &mygo.WebView{}, nil
	}
	inlineDiagramViewAlive = func(v *mygo.WebView) bool { return v != nil }
	inlineDiagramReleaseView = func(v *mygo.WebView) {}
	t.Cleanup(func() {
		inlineDiagramViewFactory = originalFactory
		inlineDiagramViewAlive = originalAlive
		inlineDiagramReleaseView = originalRelease
	})
	state := newDiagramTestState()
	theme := inlineDiagramTheme{Background: "#111315"}
	var cards []*inlineDiagram
	for i := 0; i < maxInlineDiagramViews+2; i++ {
		d := state.inlineDiagramFor("diagram-" + string(rune('a'+i)))
		state.ensureInlineDiagram(d, theme, i)
		cards = append(cards, d)
	}
	alive := 0
	for _, d := range state.inlineDiagrams {
		if inlineDiagramViewAlive(d.WebView) {
			alive++
		}
	}
	if alive > maxInlineDiagramViews {
		t.Fatalf("alive views = %d, want <= %d", alive, maxInlineDiagramViews)
	}
	// The oldest stamped card was released first; caches survive the release.
	oldest := cards[0]
	if oldest.WebView != nil || oldest.Status != inlineDiagramIdle {
		t.Fatalf("oldest card state = %#v", oldest)
	}
	if oldest.NaturalW != 0 || oldest.NaturalH != 0 {
		t.Fatalf("released card kept a rendered size: %#v", oldest)
	}
	// The newest cards are the ones still alive.
	if !inlineDiagramViewAlive(cards[len(cards)-1].WebView) {
		t.Fatal("newest card was released")
	}
}

func TestDropStaleInlineDiagramsReleasesUnbuiltCards(t *testing.T) {
	originalRelease := inlineDiagramReleaseView
	originalAlive := inlineDiagramViewAlive
	inlineDiagramViewAlive = func(v *mygo.WebView) bool { return v != nil }
	inlineDiagramReleaseView = func(v *mygo.WebView) {}
	t.Cleanup(func() {
		inlineDiagramReleaseView = originalRelease
		inlineDiagramViewAlive = originalAlive
	})
	state := newDiagramTestState()
	fresh := &inlineDiagram{Key: "fresh", Source: "fresh", Status: inlineDiagramLoading}
	fresh.WebView = &mygo.WebView{}
	fresh.lastFrame = 7
	stale := &inlineDiagram{Key: "stale", Source: "stale", Status: inlineDiagramLoading}
	stale.WebView = &mygo.WebView{}
	stale.lastFrame = 5
	stale.NaturalW, stale.NaturalH = 10, 20
	state.inlineDiagrams["fresh"] = fresh
	state.inlineDiagrams["stale"] = stale
	state.dropStaleInlineDiagrams(7)
	if fresh.WebView == nil || fresh.Status != inlineDiagramLoading {
		t.Fatalf("fresh card state = %#v", fresh)
	}
	if stale.WebView != nil || stale.Status != inlineDiagramIdle {
		t.Fatalf("stale card state = %#v", stale)
	}
	if stale.NaturalW != 10 || stale.NaturalH != 20 {
		t.Fatalf("stale card cache lost: %#v", stale)
	}
}

func TestSetInlineDiagramScaleClampsAndForwards(t *testing.T) {
	originalAlive := inlineDiagramViewAlive
	originalApply := inlineDiagramApplyEval
	inlineDiagramViewAlive = func(v *mygo.WebView) bool { return v != nil }
	var codes []string
	inlineDiagramApplyEval = func(webview *mygo.WebView, code string, timeout time.Duration) {
		codes = append(codes, code)
		if timeout != inlineDiagramApplyTimeout {
			t.Errorf("scale timeout = %s, want %s", timeout, inlineDiagramApplyTimeout)
		}
	}
	t.Cleanup(func() {
		inlineDiagramViewAlive = originalAlive
		inlineDiagramApplyEval = originalApply
	})
	state := newDiagramTestState()
	d := &inlineDiagram{Key: "k", Source: "s"}
	d.WebView = &mygo.WebView{}
	state.setInlineDiagramScale(d, 10)
	state.setInlineDiagramScale(d, 0.01)
	if len(codes) != 2 {
		t.Fatalf("scale codes = %q", codes)
	}
	if !strings.Contains(codes[0], "4") || !strings.Contains(codes[1], "0.25") {
		t.Fatalf("scale codes = %q", codes)
	}
	// A released card receives no scale call at all.
	released := &inlineDiagram{Key: "k2", Source: "s2"}
	state.setInlineDiagramScale(released, 2)
	if len(codes) != 2 {
		t.Fatalf("released card got a scale call: %q", codes)
	}
}
