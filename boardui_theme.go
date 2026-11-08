package main

import (
	"log"
	"sync"

	"github.com/egoist/mygo/ui"
)

// P31 §1–§2. Seat A (theme, fonts, shell) owns this file; Seat B (chat area)
// builds against the names only and never edits it.

// boardUIFontFamily is the BoardUI preset's text family list: Inter for Latin
// and digits, Noto Sans SC for Chinese, then the system font (§1.2).
const boardUIFontFamily = "BYOP Inter, BYOP Noto Sans SC, system-ui"

// boarduiBlue is BoardUI's public blue scale from the free-layer theme.css
// (Tailwind v4 blue, blue-400 overridden to #3392ff); the neutral scale and
// its overrides 100=#f7f7f7, 200=#ebebeb, 925=#121212 are inlined below.
var boarduiBlue = map[string]string{
	"50":  "#eff6ff",
	"100": "#dbeafe",
	"300": "#8ec5ff",
	"400": "#3392ff",
	"500": "#2b7fff",
	"600": "#155dfc",
	"700": "#1447e6",
	"800": "#193cb8",
	"950": "#162456",
}

// boardUIThemePalette resolves the BoardUI palette per docs/P31-design.md §2:
// opaque Tailwind-derived colors, blue accent, and the deviation rows noted
// there (accent takes 600 so white-on-accent reaches 5.25; muted is darkened).
// BoardUI never uses the glass or a wallpaper, so reduceTransparency does
// not change anything.
func boardUIThemePalette(dark, high, reduceTransparency bool) palette {
	p := palette{
		preset:             appearancePresetBoardUI,
		visualMetrics:      metricsForAppearancePreset(appearancePresetBoardUI),
		dark:               dark,
		highContrast:       high,
		reduceTransparency: reduceTransparency,
	}
	if dark {
		p.background = ui.Hex("#121212")
		p.surface = ui.Hex("#171717")
		p.surfaceSubtle = ui.Hex("#262626")
		p.surfaceHover = ui.Hex("#262626")
		p.surfacePressed = ui.Hex("#404040")
		p.border = ui.Hex("#404040")
		p.borderStrong = ui.Hex("#737373")
		p.text = ui.Hex("#fafafa")
		p.muted = ui.Hex("#a1a1a1")
		p.selection = ui.Hex("#162456").Alpha(.60)
		p.readingSurface = p.surface
		p.raisedSurface = p.surfaceSubtle
		p.composerSurface = p.surfaceSubtle
		p.scrollbar = ui.RGBA(255, 255, 255, .18)
		p.shadow = ui.RGBA(0, 0, 0, .16)
		p.shadowCard = ui.RGBA(0, 0, 0, .14)
		p.shadowOverlay = ui.RGBA(0, 0, 0, .20)
		p.shadowStage = ui.RGBA(0, 0, 0, .20)
	} else {
		p.background = ui.Hex("#ffffff")
		p.surface = ui.Hex("#f7f7f7")
		p.surfaceSubtle = ui.Hex("#ebebeb")
		p.surfaceHover = ui.Hex("#ebebeb")
		p.surfacePressed = ui.Hex("#d4d4d4")
		p.border = ui.Hex("#ebebeb")
		p.borderStrong = ui.Hex("#d4d4d4")
		p.text = ui.Hex("#0a0a0a")
		p.muted = ui.Hex("#696969")
		p.selection = ui.Hex("#dbeafe")
		p.readingSurface = p.surface
		p.raisedSurface = ui.Hex("#ffffff")
		p.composerSurface = ui.Hex("#ffffff")
		p.scrollbar = ui.RGBA(0, 0, 0, .18)
		p.shadow = ui.RGBA(0, 0, 0, .06)
		p.shadowCard = ui.RGBA(0, 0, 0, .05)
		p.shadowOverlay = ui.RGBA(0, 0, 0, .10)
		p.shadowStage = ui.RGBA(0, 0, 0, .10)
	}
	if high {
		if dark {
			p.muted = ui.Hex("#cecece")
			p.border = ui.Hex("#a1a1a1")
			p.borderStrong = ui.Hex("#a1a1a1")
		} else {
			p.muted = ui.Hex("#3a3a3a")
			p.border = ui.Hex("#737373")
			p.borderStrong = ui.Hex("#737373")
		}
	}
	p.accent = ui.Hex(boarduiBlue["600"])
	p.accentHover = ui.Hex(boarduiBlue["700"])
	p.accentPressed = ui.Hex(boarduiBlue["800"])
	p.accentText = ui.Hex(boarduiBlue["700"])
	if dark {
		p.accentText = ui.Hex(boarduiBlue["300"])
	}
	p.onAccent = ui.Hex("#ffffff")
	p.focus = ui.Hex(boarduiBlue["500"])
	p.danger = ui.Hex("#fb2c36")
	p.dangerText = ui.Hex("#c10007")
	p.success = ui.Hex("#7ccf00")
	p.successText = ui.Hex("#3c6300")
	p.warning = ui.Hex("#f0b100")
	p.warningText = ui.Hex("#894b00")
	if dark {
		p.danger = ui.Hex("#ff6467")
		p.dangerText = ui.Hex("#ff6467")
		p.success = ui.Hex("#9ae600")
		p.successText = ui.Hex("#9ae600")
		p.warning = ui.Hex("#fdc700")
		p.warningText = ui.Hex("#fdc700")
	}
	p.popoverTint = p.raisedSurface
	p.popoverBorder = p.border
	// BoardUI does not use the glass, but the palette must fill the field so
	// the old glass call sites still compile.
	p.glassTint = p.surface
	// P32 §5.4: BoardUI conversation typography.
	p.chatFont = chatFontBoardUI
	p.chatLineHeight = chatLineBoardUI
	p.chatCodeFont = chatCodeBoardUI
	applyPluginThemeOverlay(&p)
	return p
}

var ensureBoardUIFontsOnce sync.Once

// ensureBoardUIFonts is a seam so tests can count calls: the BYOP and
// J Agent branches must never invoke it. Never swap it in parallel tests:
// other tests read the default implementation.
var ensureBoardUIFonts = defaultEnsureBoardUIFonts

// boardUIFontRegistrationError holds the error of the first registration so
// every later call reports the same outcome as the first one.
var boardUIFontRegistrationError error

// defaultEnsureBoardUIFonts registers the embedded fonts once: the four Inter
// files as the "BYOP Inter" family and the three Noto Sans SC files as
// "BYOP Noto Sans SC". Core Text picks the weight closest to the requested
// one among a family's files. A failure only logs; the family list then falls
// back to the system font. Other presets never call it.
func defaultEnsureBoardUIFonts() error {
	ensureBoardUIFontsOnce.Do(func() {
		var first error
		families := []struct {
			data []byte
			name string
		}{
			{fontInterRegular, "BYOP Inter"},
			{fontInterMedium, "BYOP Inter"},
			{fontInterSemiBold, "BYOP Inter"},
			{fontInterBold, "BYOP Inter"},
			{fontNotoSansSCRegular, "BYOP Noto Sans SC"},
			{fontNotoSansSCMedium, "BYOP Noto Sans SC"},
			{fontNotoSansSCBold, "BYOP Noto Sans SC"},
		}
		for _, f := range families {
			if ferr := ui.RegisterFont(f.data, f.name); ferr != nil {
				if first == nil {
					first = ferr
				}
				log.Printf("register BoardUI font %s: %v", f.name, ferr)
			}
		}
		boardUIFontRegistrationError = first
	})
	return boardUIFontRegistrationError
}

// glassAllowed reports whether a palette may use the product glass and the
// wallpaper; BoardUI surfaces are opaque (§4).
func glassAllowed(p palette) bool { return p.preset != appearancePresetBoardUI }
