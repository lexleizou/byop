package main

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/pi.svg
var piSVGData []byte

var iconSet map[string]*ui.SVG

func init() {
	iconSet = make(map[string]*ui.SVG)
	for name, markup := range lucideIcons {
		iconSet[name] = ui.MustParseSVG([]byte(lucide(markup)))
	}
	iconSet["pi"] = ui.MustParseSVG(piSVGData)
}

func icon(name string) *ui.SVG {
	if s := iconSet[strings.ToLower(name)]; s != nil {
		return s
	}
	panic(fmt.Sprintf("unknown UI icon %q", name))
}

func iconOr(name, fallbackName string) *ui.SVG {
	if svg := iconSet[strings.ToLower(name)]; svg != nil {
		return svg
	}
	return icon(fallbackName)
}

func lucide(elements string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` +
		elements + `</svg>`
}

// The small UI icon set follows the same Lucide geometry and stroke rules
// as the Pi mark used by Onevra and BYOP.
var lucideIcons = map[string]string{
	"chat": `<path d="M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4z"/>
		<path d="M8 9h8M8 13h5"/>`,
	"plugins": `<rect width="7" height="7" x="3" y="3" rx="1"/>
		<rect width="7" height="7" x="14" y="3" rx="1"/>
		<rect width="7" height="7" x="14" y="14" rx="1"/>
		<rect width="7" height="7" x="3" y="14" rx="1"/>`,
	"library": `<path d="M12 7v14"/>
		<path d="M3 18a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5a3 3 0 0 1 3 3v1"/>
		<path d="M21 18a2 2 0 0 0 2-2V5a2 2 0 0 0-2-2h-5a3 3 0 0 0-3 3v1"/>`,
	"document": `<path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5z"/>
		<path d="M14 2v6h6M8 13h8M8 17h6"/>`,
	"automations": `<path d="M5 7h4V3"/>
		<path d="M19 17h-4v4"/>
		<path d="M5 7a8 8 0 0 1 13.5-2"/>
		<path d="M19 17A8 8 0 0 1 5.5 19"/>`,
	"settings": `<path d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z"/>
		<path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1-1.6v-.2h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"/>`,
	"search": `<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>`,
	"panel-right": `<rect width="18" height="18" x="3" y="3" rx="2"/>
		<path d="M15 3v18"/>`,
	"panel-left": `<rect width="18" height="18" x="3" y="3" rx="2"/>
		<path d="M9 3v18"/>`,
	"sun": `<circle cx="12" cy="12" r="4"/>
		<path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>`,
	"moon": `<path d="M21 12.8A8.5 8.5 0 1 1 11.2 3 7 7 0 0 0 21 12.8Z"/>`,
	"monitor": `<rect width="20" height="14" x="2" y="3" rx="2"/>
		<path d="M8 21h8M12 17v4"/>`,
	"send":      `<path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/>`,
	"stop":      `<rect width="14" height="14" x="5" y="5" rx="2"/>`,
	"paperclip": `<path d="m21.4 11.6-8.9 8.9a6 6 0 0 1-8.5-8.5l9.6-9.6a4 4 0 1 1 5.7 5.7l-9.6 9.6a2 2 0 1 1-2.8-2.8l8.9-8.9"/>`,
	"plus":      `<path d="M5 12h14M12 5v14"/>`,
	"check":     `<path d="m20 6-11 11-5-5"/>`,
	"folder":    `<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>`,
	"file": `<path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5z"/>
		<path d="M14 2v6h6"/>`,
	"terminal": `<polyline points="4 17 10 11 4 5"/><path d="M12 19h8"/>`,
	"package": `<path d="m16.5 9.4-9-5.2M21 16V8a2 2 0 0 0-1-1.7l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.7l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/>
		<path d="M3.3 7 12 12l8.7-5M12 22V12"/>`,
	"clock":        `<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`,
	"play":         `<polygon points="7 4 20 12 7 20 7 4"/>`,
	"chevron-down": `<path d="m6 9 6 6 6-6"/>`,
	"x":            `<path d="M18 6 6 18M6 6l12 12"/>`,
	"info":         `<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>`,
	"sparkles": `<path d="m12 3-1.4 3.6L7 8l3.6 1.4L12 13l1.4-3.6L17 8l-3.6-1.4Z"/>
		<path d="m5 14-1 2.5L1.5 18 4 19l1 3 1-3 2.5-1L6 16.5Z"/>
		<path d="m19 14-.8 2.2L16 17l2.2.8L19 20l.8-2.2L22 17l-2.2-.8Z"/>`,
	"database": `<ellipse cx="12" cy="5" rx="8" ry="3"/>
		<path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5"/>
		<path d="M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"/>`,
	"git": `<circle cx="6" cy="5" r="2"/>
		<circle cx="18" cy="7" r="2"/>
		<circle cx="6" cy="19" r="2"/>
		<path d="M6 7v10M8 7h5a5 5 0 0 1 5 5v0"/>
		<path d="m14 10 3-3 3 3"/>`,
}
