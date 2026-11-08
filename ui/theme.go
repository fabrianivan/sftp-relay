package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Theme is SFTP Relay's dark theme with an emerald accent.
type Theme struct{}

var _ fyne.Theme = (*Theme)(nil)

// Palette shared by custom widgets.
var (
	ColorAccent  = color.NRGBA{R: 16, G: 185, B: 129, A: 255} // emerald-500
	ColorSuccess = color.NRGBA{R: 34, G: 197, B: 94, A: 255}  // green-500
	ColorWarning = color.NRGBA{R: 245, G: 158, B: 11, A: 255} // amber-500
	ColorError   = color.NRGBA{R: 239, G: 68, B: 68, A: 255}  // red-500
	ColorMuted      = color.NRGBA{R: 148, G: 163, B: 184, A: 255} // slate-400
	ColorCard       = color.NRGBA{R: 21, G: 29, B: 42, A: 255}   // slate-800+
	ColorForeground = color.NRGBA{R: 248, G: 250, B: 252, A: 255} // slate-50
)

func (Theme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 11, G: 15, B: 23, A: 255} // modern deep slate #0B0F17
	case theme.ColorNameButton:
		return color.NRGBA{R: 30, G: 41, B: 59, A: 255} // slate-800
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 20, G: 27, B: 38, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 100, G: 116, B: 139, A: 255} // slate-500
	case theme.ColorNameForeground:
		return color.NRGBA{R: 248, G: 250, B: 252, A: 255} // slate-50
	case theme.ColorNameHover:
		return color.NRGBA{R: 51, G: 65, B: 85, A: 255} // slate-700
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 17, G: 24, B: 39, A: 255} // slate-900
	case theme.ColorNameInputBorder:
		return color.NRGBA{R: 51, G: 65, B: 85, A: 255} // slate-700
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 148, G: 163, B: 184, A: 255}
	case theme.ColorNamePrimary:
		return ColorAccent
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 71, G: 85, B: 105, A: 180}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 30, G: 41, B: 59, A: 255}
	case theme.ColorNameShadow:
		return color.NRGBA{A: 110}
	case theme.ColorNameSuccess:
		return ColorSuccess
	case theme.ColorNameWarning:
		return ColorWarning
	case theme.ColorNameError:
		return ColorError
	case theme.ColorNameHeaderBackground:
		return color.NRGBA{R: 17, G: 24, B: 39, A: 255}
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 21, G: 29, B: 42, A: 252}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 16, G: 185, B: 129, A: 70}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 16, G: 185, B: 129, A: 190}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}

func (Theme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (Theme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (Theme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameCaptionText:
		return 12
	case theme.SizeNameInlineIcon:
		return 20
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameInputBorder:
		return 1.5
	default:
		return theme.DefaultTheme().Size(name)
	}
}
