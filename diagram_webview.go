package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/egoist/mygo"
)

// The inline diagram card (P30) keeps one WebView per rendered Mermaid block.
// The card state, its view lifecycle, and the async render call live here; the
// card UI that builds and lays the views out belongs to seat B and calls only
// the functions of the contract at the bottom of this file.

const (
	// maxInlineDiagramViews bounds how many diagram card WebViews are alive at
	// once, independent of the file previews.
	maxInlineDiagramViews = 6
	// inlineDiagramMaxSourceUnits is the UTF-16 length the plugin renderer
	// refuses (MAX_SOURCE_CHARS); the host rejects exactly what the page would.
	inlineDiagramMaxSourceUnits = 20000
	// inlineDiagramRenderTimeout bounds the page render call; a slower page
	// becomes a failed card instead of a frozen transcript.
	inlineDiagramRenderTimeout = 5 * time.Second
	// inlineDiagramApplyTimeout bounds the setTheme/setScale follow-ups, which
	// only repaint and can never fail the card.
	inlineDiagramApplyTimeout = time.Second
	inlineDiagramMinScale     = float32(0.25)
	inlineDiagramMaxScale     = float32(4)
)

type inlineDiagramTheme struct {
	Background, Text, Muted, Surface, SurfaceAlt, Border, Accent, Danger string // "#rrggbb"
	Chart                                                                [5]string
	FontFamily                                                           string
	FontSize                                                             float32
	HighContrast                                                         bool
}

type inlineDiagramStatus uint8

const (
	inlineDiagramIdle inlineDiagramStatus = iota // 尚未建视图（或已释放）
	inlineDiagramLoading
	inlineDiagramReady
	inlineDiagramFailed
	inlineDiagramTooLong
)

type inlineDiagram struct {
	Key, Source        string
	Status             inlineDiagramStatus
	WebView            *mygo.WebView
	NaturalW, NaturalH float32 // 成功后有效；释放视图后仍保留作缓存
	Agentflow          bool
	Err                string
	ErrLine            int

	// lastFrame is the transcript frame that still builds this card; views the
	// next frame does not reach are dropped by dropStaleInlineDiagrams.
	lastFrame int
	// renderSeq cancels a render reply that answers an older view generation.
	renderSeq uint64
	// scale mirrors the last scale the host asked the page to apply.
	scale float32
}

// inlineDiagramResult is the page's render reply, decoded from the eval value.
type inlineDiagramResult struct {
	OK        bool    `json:"ok"`
	Width     float32 `json:"width"`
	Height    float32 `json:"height"`
	Agentflow bool    `json:"agentflow"`
	Error     string  `json:"error"`
	Line      int     `json:"line"`
}

// Test seams keep the view lifecycle reachable without a real window: the
// production values call mygo directly, and the tests below replace single
// ones. They are not configuration and nothing outside this file reads them.
var (
	inlineDiagramViewFactory = func(
		s *appState, d *inlineDiagram, theme inlineDiagramTheme, gen uint64,
	) (*mygo.WebView, error) {
		return s.newInlineDiagramWebView(d, theme, gen)
	}
	inlineDiagramViewAlive = func(v *mygo.WebView) bool {
		return v != nil && !v.IsDestroyed()
	}
	inlineDiagramReleaseView = func(v *mygo.WebView) {
		if v != nil && !v.IsDestroyed() {
			v.Destroy()
		}
	}
	inlineDiagramEval = func(ctx context.Context, page *mygo.Page, code string) (any, error) {
		return page.EvalContext(ctx, code)
	}
	// inlineDiagramApplyEval sends a fire-and-forget page call with a bounded
	// wait; the tests capture the code synchronously instead of spawning.
	inlineDiagramApplyEval = func(webview *mygo.WebView, code string, timeout time.Duration) {
		if webview == nil {
			return
		}
		page := webview.Page()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			_, _ = inlineDiagramEval(ctx, page, code)
		}()
	}
)

func inlineDiagramKey(source string) string {
	sum := sha256.Sum256([]byte(source))
	return fmt.Sprintf("%x", sum[:])
}

// inlineDiagramFor returns the state of one block's source, creating it on
// first sight. A source the page itself would refuse is marked TooLong at
// once, so the card never builds a view for it.
func (s *appState) inlineDiagramFor(source string) *inlineDiagram {
	if s.inlineDiagrams == nil {
		s.inlineDiagrams = make(map[string]*inlineDiagram)
	}
	key := inlineDiagramKey(source)
	d := s.inlineDiagrams[key]
	if d == nil {
		d = &inlineDiagram{Key: key, Source: source}
		if utf16CodeUnits(source) > inlineDiagramMaxSourceUnits {
			d.Status = inlineDiagramTooLong
		}
		s.inlineDiagrams[key] = d
	}
	return d
}

// ensureInlineDiagram builds the card's view when one is missing and starts
// the render call; a card that is already loading, ready, failed, or too long
// only refreshes its frame stamp. Called from the frame, on the UI thread.
func (s *appState) ensureInlineDiagram(d *inlineDiagram, theme inlineDiagramTheme, frame int) {
	if d == nil {
		return
	}
	d.lastFrame = frame
	switch d.Status {
	case inlineDiagramTooLong, inlineDiagramFailed:
		return
	case inlineDiagramLoading:
		return
	case inlineDiagramReady:
		// A released view keeps the rendered status as its cache (see
		// releaseInlineDiagram). Coming back needs a new view, and the page
		// re-renders on load, so the card never stays blank. A view that still
		// points at a destroyed page counts as released.
		if d.WebView != nil && inlineDiagramViewAlive(d.WebView) {
			return
		}
	}
	if d.WebView != nil && !inlineDiagramViewAlive(d.WebView) {
		d.WebView = nil
		d.Status = inlineDiagramIdle
	}
	if d.WebView != nil {
		return
	}
	s.trimInlineDiagrams(d)
	// The generation is the view's identity: a callback that arrives after the
	// view was released or replaced belongs to an older page and is dropped.
	d.renderSeq++
	gen := d.renderSeq
	webview, err := inlineDiagramViewFactory(s, d, theme, gen)
	if err != nil {
		s.failInlineDiagram(d, err)
		if s.window != nil {
			s.window.Invalidate()
		}
		return
	}
	d.WebView = webview
	// A ready card that lost its view keeps its status: its surface stays blank
	// at the cached height until the new page renders, instead of claiming to
	// still be working. Every other state starts a render, which the page
	// begins once its shell has loaded.
	if d.Status != inlineDiagramReady {
		d.Status = inlineDiagramLoading
	}
}

// newInlineDiagramWebView creates the card shell WebView with the same hard
// boundaries the plugin views use: no permissions, no new windows, and every
// navigation stopped — an http(s) click goes to the system browser instead.
func (s *appState) newInlineDiagramWebView(
	d *inlineDiagram,
	theme inlineDiagramTheme,
	gen uint64,
) (*mygo.WebView, error) {
	if s.window == nil {
		return nil, errors.New("window is not ready")
	}
	webview, err := s.window.NewWebView(mygo.WebViewOptions{
		URL:             diagramCardURL,
		BackgroundColor: theme.Background,
	})
	if err != nil {
		return nil, err
	}
	page := webview.Page()
	page.SetPermissionHandler(func(mygo.PermissionRequest) bool { return false })
	page.SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions {
		return nil
	})
	loaded := false // the shell's own first load is the only one let through
	page.OnWillNavigate(func(event *mygo.NavigateEvent) {
		if event != nil && !loaded && event.URL == diagramCardURL {
			return // the shell's own first load
		}
		if event != nil {
			event.PreventDefault()
		}
		if parsed, err := url.Parse(diagramCardNavigateTarget(event)); err == nil &&
			(parsed.Scheme == "http" || parsed.Scheme == "https") {
			if err := mygo.Shell.OpenExternal(parsed.String()); err != nil {
				s.postUI(func() {
					if s.window != nil {
						s.window.Invalidate()
					}
				})
			}
		}
	})
	page.OnDidFailLoad(func(loadErr *mygo.LoadError) {
		message := "page load failed"
		if loadErr != nil {
			message = loadErr.Error()
		}
		s.postUI(func() {
			if d.renderSeq != gen {
				return // an older page's failure, not this card's
			}
			s.failInlineDiagram(d, errors.New(message))
		})
	})
	page.OnDidFinishLoad(func() {
		loaded = true
		if page.URL() != diagramCardURL {
			return
		}
		s.postUI(func() {
			if d.renderSeq != gen {
				return
			}
			go s.renderInlineDiagram(page, d, gen, theme)
		})
	})
	return webview, nil
}

// diagramCardNavigateTarget is the URL a blocked navigation aimed at; a nil
// event has no target, so the guard never opens anything.
func diagramCardNavigateTarget(event *mygo.NavigateEvent) string {
	if event == nil {
		return ""
	}
	return event.URL
}

// renderInlineDiagram runs the page render with the 5s budget and hands the
// reply back to the UI thread. The source and theme go to the page as JSON
// literals, never as concatenated script text.
func (s *appState) renderInlineDiagram(
	page *mygo.Page,
	d *inlineDiagram,
	seq uint64,
	theme inlineDiagramTheme,
) {
	if d == nil || page == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), inlineDiagramRenderTimeout)
	defer cancel()
	source, _ := json.Marshal(d.Source)
	payload, _ := json.Marshal(theme)
	value, err := inlineDiagramEval(ctx, page, fmt.Sprintf(
		"return window.byopDiagram.render(%s, %s)", source, payload,
	))
	s.postUI(func() {
		s.applyInlineDiagramRender(d, seq, value, err)
	})
}

// applyInlineDiagramRender lands a render reply on the UI thread. A reply for
// a view that was recreated, released, or replaced since the call started is
// dropped, so a stale success cannot overwrite a newer attempt.
func (s *appState) applyInlineDiagramRender(
	d *inlineDiagram,
	seq uint64,
	value any,
	evalErr error,
) {
	if d == nil || d.renderSeq != seq || !inlineDiagramViewAlive(d.WebView) {
		return
	}
	if evalErr != nil {
		s.failInlineDiagram(d, inlineDiagramRenderError(evalErr))
		return
	}
	result, err := parseInlineDiagramResult(value)
	if err != nil {
		s.failInlineDiagram(d, err)
		return
	}
	if !result.OK {
		s.failInlineDiagram(d, errors.New(result.Error), result.Line)
		return
	}
	d.Status = inlineDiagramReady
	d.Err = ""
	d.ErrLine = 0
	d.NaturalW = result.Width
	d.NaturalH = result.Height
	d.Agentflow = result.Agentflow
	if s.window != nil {
		s.window.Invalidate()
	}
}

func parseInlineDiagramResult(value any) (inlineDiagramResult, error) {
	var result inlineDiagramResult
	m, ok := value.(map[string]any)
	if !ok {
		return result, errors.New("diagram page returned no render object")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return result, errors.New("diagram page returned invalid render data")
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, errors.New("diagram page returned invalid render data")
	}
	return result, nil
}

// inlineDiagramRenderError turns an eval failure into the card's message; the
// context deadline is the 5s budget expiring, which the UI renders as a timed
// out card rather than a script error.
func inlineDiagramRenderError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return fmt.Errorf("render did not finish in %s", inlineDiagramRenderTimeout)
	}
	return err
}

func (s *appState) failInlineDiagram(d *inlineDiagram, err error, line ...int) {
	if d == nil {
		return
	}
	d.Status = inlineDiagramFailed
	d.Err = err.Error()
	d.ErrLine = 0
	if len(line) > 0 {
		d.ErrLine = line[0]
	}
	if s.window != nil {
		s.window.Invalidate()
	}
}

// setInlineDiagramScale forwards a zoom step to the page. The card repaints
// only, so a failed or lost reply never changes the card's state.
func (s *appState) setInlineDiagramScale(d *inlineDiagram, scale float32) {
	if d == nil || !inlineDiagramViewAlive(d.WebView) {
		return
	}
	if scale < inlineDiagramMinScale {
		scale = inlineDiagramMinScale
	}
	if scale > inlineDiagramMaxScale {
		scale = inlineDiagramMaxScale
	}
	d.scale = scale
	inlineDiagramApplyEval(d.WebView,
		fmt.Sprintf("return window.byopDiagram.setScale(%g)", scale),
		inlineDiagramApplyTimeout)
}

// setInlineDiagramTheme pushes new palette values to every alive card page.
// The pages only swap CSS variables, so the rendered SVG survives untouched.
func (s *appState) setInlineDiagramTheme(theme inlineDiagramTheme) {
	payload, _ := json.Marshal(theme)
	background := theme.Background
	for _, d := range s.inlineDiagrams {
		if !inlineDiagramViewAlive(d.WebView) {
			continue
		}
		d.WebView.SetBackgroundColor(background)
		inlineDiagramApplyEval(d.WebView,
			fmt.Sprintf("return window.byopDiagram.setTheme(%s)", payload),
			inlineDiagramApplyTimeout)
	}
}

// releaseInlineDiagram destroys the view but keeps the state: the natural
// size cache survives, so a rebuilt card does not jump.
func (s *appState) releaseInlineDiagram(d *inlineDiagram) {
	if d == nil {
		return
	}
	if d.WebView != nil {
		inlineDiagramReleaseView(d.WebView)
		d.WebView = nil
	}
	d.renderSeq++
	// A released view keeps what it rendered: the natural size and the status
	// are the card's cache, so scrolling back shows the diagram again without a
	// re-parse, and a failed source keeps its failure instead of retrying on
	// every pass. Only an unfinished render has to start over.
	if d.Status == inlineDiagramLoading {
		d.Status = inlineDiagramIdle
	}
}

// dropStaleInlineDiagrams frees the card views the transcript stopped
// building this frame, keeping their caches. Called from the frame like the
// file preview counterpart.
func (s *appState) dropStaleInlineDiagrams(frame int) {
	for _, d := range s.inlineDiagrams {
		if d.lastFrame != frame {
			s.releaseInlineDiagram(d)
		}
	}
}

// trimInlineDiagrams keeps at most maxInlineDiagramViews alive card views by
// releasing the oldest stamped ones, never the card currently being ensured.
func (s *appState) trimInlineDiagrams(keep *inlineDiagram) {
	for key, d := range s.inlineDiagrams {
		if d.WebView != nil && !inlineDiagramViewAlive(d.WebView) {
			delete(s.inlineDiagrams, key)
		}
	}
	for {
		alive := 0
		for _, d := range s.inlineDiagrams {
			if inlineDiagramViewAlive(d.WebView) {
				alive++
			}
		}
		if alive < maxInlineDiagramViews {
			return
		}
		oldest, oldestFrame := (*inlineDiagram)(nil), 0
		for _, d := range s.inlineDiagrams {
			if !inlineDiagramViewAlive(d.WebView) || d == keep {
				continue
			}
			if oldest == nil || d.lastFrame < oldestFrame {
				oldest, oldestFrame = d, d.lastFrame
			}
		}
		if oldest == nil {
			return
		}
		s.releaseInlineDiagram(oldest)
	}
}
