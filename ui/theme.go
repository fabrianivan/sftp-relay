package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Theme is SFTP Relay's look: a deep ink canvas on which each stream of
// files has its own colour — sky for the source server, violet for the local
// folder, and teal for the target they both flow into. Teal doubles as the
// colour of everything that is healthy or can be pressed.
type Theme struct{}

var _ fyne.Theme = (*Theme)(nil)

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 255} }

// alpha returns c with its opacity replaced.
func alpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

// Palette. Everything in the UI draws from these values.
var (
	// Surfaces, from the window background up to raised controls.
	ColorBackground = rgb(8, 13, 17)
	ColorRail       = rgb(5, 9, 12)
	ColorSurface    = rgb(14, 22, 28)
	ColorRaised     = rgb(24, 36, 45)
	ColorBorder     = rgb(31, 46, 57)
	ColorSeparator  = rgb(22, 33, 41)

	// Text, from primary to faint.
	ColorForeground    = rgb(232, 241, 242)
	ColorForegroundSub = rgb(170, 189, 195)
	ColorMuted         = rgb(116, 138, 147)
	ColorFaint         = rgb(78, 98, 107)

	// Streams.
	ColorSource = rgb(86, 170, 255)  // files arriving from the source server
	ColorLocal  = rgb(178, 144, 255) // files backed up from this computer
	ColorAccent = rgb(54, 211, 198)  // the target; also "healthy" and "press me"

	ColorOnAccent = rgb(2, 26, 25)
	ColorSuccess  = ColorAccent
	ColorWarning  = rgb(242, 183, 82)
	ColorError    = rgb(251, 113, 121)
)

func (Theme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return ColorBackground
	case theme.ColorNameForeground:
		return ColorForeground
	case theme.ColorNameDisabled: // also used for low-importance (secondary) text
		return ColorMuted
	case theme.ColorNamePlaceHolder:
		return ColorFaint

	case theme.ColorNameButton:
		return ColorRaised
	case theme.ColorNameDisabledButton:
		return rgb(15, 23, 29)
	case theme.ColorNameHover:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 15}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 30}

	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return ColorAccent
	case theme.ColorNameForegroundOnPrimary:
		return ColorOnAccent
	case theme.ColorNameFocus:
		return alpha(ColorAccent, 105)
	case theme.ColorNameSelection:
		return alpha(ColorAccent, 46)

	case theme.ColorNameInputBackground:
		return ColorBackground
	case theme.ColorNameInputBorder:
		return ColorBorder

	case theme.ColorNameSuccess:
		return ColorSuccess
	case theme.ColorNameWarning:
		return ColorWarning
	case theme.ColorNameError:
		return ColorError
	case theme.ColorNameForegroundOnSuccess, theme.ColorNameForegroundOnWarning, theme.ColorNameForegroundOnError:
		return ColorBackground

	case theme.ColorNameHeaderBackground:
		return ColorRaised
	case theme.ColorNameMenuBackground:
		return ColorRaised
	case theme.ColorNameOverlayBackground:
		return ColorSurface
	case theme.ColorNameSeparator:
		return ColorSeparator
	case theme.ColorNameScrollBar:
		return rgb(58, 79, 92)
	case theme.ColorNameScrollBarBackground:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 8}
	case theme.ColorNameShadow:
		return color.NRGBA{A: 44}

	// Stream colours, so icons can be tinted to match their stream.
	case colorNameSource:
		return ColorSource
	case colorNameLocal:
		return ColorLocal
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}

func (Theme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (Theme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (Theme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameText:
		return 14
	case theme.SizeNameCaptionText:
		return 12
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameInlineIcon:
		return 20
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameInputRadius:
		return 8
	case theme.SizeNameSelectionRadius:
		return 5
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameScrollBarSmall:
		return 4
	default:
		return theme.DefaultTheme().Size(name)
	}
}
