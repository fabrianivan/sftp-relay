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
	ColorSuccess = color.NRGBA{R: 34, G: 197, B: 94, A: 255}
	ColorWarning = color.NRGBA{R: 245, G: 158, B: 11, A: 255}
	ColorError   = color.NRGBA{R: 239, G: 68, B: 68, A: 255}
	ColorMuted   = color.NRGBA{R: 122, G: 133, B: 128, A: 255}
)

func (Theme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 15, G: 19, B: 18, A: 255}
	case theme.ColorNameButton:
		return color.NRGBA{R: 33, G: 41, B: 39, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 26, G: 31, B: 30, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 86, G: 97, B: 93, A: 255}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 228, G: 236, B: 233, A: 255}
	case theme.ColorNameHover:
		return color.NRGBA{R: 42, G: 54, B: 50, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 23, G: 29, B: 28, A: 255}
	case theme.ColorNameInputBorder:
		return color.NRGBA{R: 52, G: 64, B: 60, A: 255}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 104, G: 116, B: 112, A: 255}
	case theme.ColorNamePrimary:
		return ColorAccent
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 60, G: 74, B: 69, A: 200}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 36, G: 45, B: 42, A: 255}
	case theme.ColorNameShadow:
		return color.NRGBA{A: 90}
	case theme.ColorNameSuccess:
		return ColorSuccess
	case theme.ColorNameWarning:
		return ColorWarning
	case theme.ColorNameError:
		return ColorError
	case theme.ColorNameHeaderBackground:
		return color.NRGBA{R: 20, G: 26, B: 24, A: 255}
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 22, G: 28, B: 27, A: 250}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 16, G: 185, B: 129, A: 60}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 16, G: 185, B: 129, A: 170}
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
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameInputBorder:
		return 1.5
	default:
		return theme.DefaultTheme().Size(name)
	}
}
