package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Theme is SFTP Relay's modern dark theme with vibrant gradients.
type Theme struct{}

var _ fyne.Theme = (*Theme)(nil)

// Enhanced palette with modern color scheme
var (
	// Primary gradient colors
	ColorAccent     = color.NRGBA{R: 99, G: 102, B: 241, A: 255}  // Indigo-500 (vibrant primary)
	ColorAccentDark = color.NRGBA{R: 79, G: 70, B: 229, A: 255}   // Indigo-600
	ColorAccentGlow = color.NRGBA{R: 99, G: 102, B: 241, A: 60}   // Glow effect
	
	// Status colors with better contrast
	ColorSuccess     = color.NRGBA{R: 16, G: 185, B: 129, A: 255}  // Emerald-500
	ColorSuccessGlow = color.NRGBA{R: 16, G: 185, B: 129, A: 40}
	ColorWarning     = color.NRGBA{R: 251, G: 146, B: 60, A: 255}  // Orange-500
	ColorWarningGlow = color.NRGBA{R: 251, G: 146, B: 60, A: 40}
	ColorError       = color.NRGBA{R: 244, G: 63, B: 94, A: 255}   // Rose-500
	ColorErrorGlow   = color.NRGBA{R: 244, G: 63, B: 94, A: 40}
	
	// Background layers (glass-morphism inspired)
	ColorBackground     = color.NRGBA{R: 10, G: 10, B: 20, A: 255}        // Deep navy
	ColorBackgroundAlt  = color.NRGBA{R: 15, G: 15, B: 30, A: 255}        // Slightly lighter
	ColorCard           = color.NRGBA{R: 20, G: 20, B: 40, A: 245}        // Card background
	ColorCardHover      = color.NRGBA{R: 25, G: 25, B: 50, A: 255}        // Card hover
	ColorCardBorder     = color.NRGBA{R: 60, G: 60, B: 100, A: 80}        // Subtle border
	
	// Text hierarchy
	ColorForeground    = color.NRGBA{R: 248, G: 250, B: 252, A: 255}  // Almost white
	ColorForegroundSub = color.NRGBA{R: 203, G: 213, B: 225, A: 255}  // Slate-300
	ColorMuted         = color.NRGBA{R: 148, G: 163, B: 184, A: 255}  // Slate-400
	ColorMutedDark     = color.NRGBA{R: 100, G: 116, B: 139, A: 255}  // Slate-500
	
	// Interactive elements
	ColorButtonPrimary   = color.NRGBA{R: 99, G: 102, B: 241, A: 255}
	ColorButtonHover     = color.NRGBA{R: 79, G: 70, B: 229, A: 255}
	ColorButtonSecondary = color.NRGBA{R: 30, G: 30, B: 50, A: 255}
	
	// Special effects
	ColorShadow      = color.NRGBA{R: 0, G: 0, B: 0, A: 140}
	ColorShadowLight = color.NRGBA{R: 0, G: 0, B: 0, A: 60}
	ColorHighlight   = color.NRGBA{R: 255, G: 255, B: 255, A: 10}
)

func (Theme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return ColorBackground
	case theme.ColorNameButton:
		return ColorButtonSecondary
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 20, G: 20, B: 35, A: 255}
	case theme.ColorNameDisabled:
		return ColorMutedDark
	case theme.ColorNameForeground:
		return ColorForeground
	case theme.ColorNameHover:
		return ColorCardHover
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 15, G: 15, B: 30, A: 255}
	case theme.ColorNameInputBorder:
		return ColorCardBorder
	case theme.ColorNamePlaceHolder:
		return ColorMuted
	case theme.ColorNamePrimary:
		return ColorAccent
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 60, G: 60, B: 100, A: 180}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 40, G: 40, B: 70, A: 255}
	case theme.ColorNameShadow:
		return ColorShadow
	case theme.ColorNameSuccess:
		return ColorSuccess
	case theme.ColorNameWarning:
		return ColorWarning
	case theme.ColorNameError:
		return ColorError
	case theme.ColorNameHeaderBackground:
		return ColorBackgroundAlt
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 20, G: 20, B: 40, A: 250}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 99, G: 102, B: 241, A: 60}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 99, G: 102, B: 241, A: 200}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}

func (Theme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (Theme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (Theme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 10
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 24
	case theme.SizeNameSubHeadingText:
		return 18
	case theme.SizeNameCaptionText:
		return 12
	case theme.SizeNameInlineIcon:
		return 22
	case theme.SizeNameScrollBar:
		return 12
	case theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameInputBorder:
		return 2
	default:
		return theme.DefaultTheme().Size(name)
	}
}
