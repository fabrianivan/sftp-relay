package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Shared building blocks. Every screen is assembled from these so spacing,
// type sizes and status colours stay consistent across the app.

const (
	panelRadius = 12 // corner radius of panels
	gutter      = 28 // page margin
	gap         = 14 // space between panels
)

var (
	styleBold = fyne.TextStyle{Bold: true}
	styleMono = fyne.TextStyle{Monospace: true}
)

// tone is the meaning carried by a coloured status element.
type tone int

const (
	toneNeutral tone = iota
	toneInfo         // sky: in progress, and the source-server stream
	toneLocal        // violet: the local-folder stream
	toneSuccess      // teal: healthy, delivered
	toneWarning
	toneDanger
)

func (t tone) color() color.NRGBA {
	switch t {
	case toneInfo:
		return ColorSource
	case toneLocal:
		return ColorLocal
	case toneSuccess:
		return ColorSuccess
	case toneWarning:
		return ColorWarning
	case toneDanger:
		return ColorError
	default:
		return ColorMuted
	}
}

// ---- text ---------------------------------------------------------------

func newText(s string, size float32, c color.Color, style fyne.TextStyle) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle = style
	return t
}

// setText updates a canvas text only when something changed.
func setText(t *canvas.Text, s string, c color.Color) {
	if t.Text == s && t.Color == c {
		return
	}
	t.Text, t.Color = s, c
	t.Refresh()
}

// overline is the small caption above a value or a group.
func overline(s string) *canvas.Text { return newText(s, 11, ColorMuted, styleBold) }

// sectionTitle heads a panel.
func sectionTitle(s string) *canvas.Text { return newText(s, 15, ColorForeground, styleBold) }

// subtitle is the muted line under a page title.
func subtitle(s string) *canvas.Text { return newText(s, 13, ColorMuted, fyne.TextStyle{}) }

// mutedLabel is secondary text that can truncate or wrap.
func mutedLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Importance = widget.LowImportance
	l.Truncation = fyne.TextTruncateEllipsis
	return l
}

// ---- layout -------------------------------------------------------------

func vstack(space float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedVBoxLayout(space), objs...)
}

func hstack(space float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedHBoxLayout(space), objs...)
}

func pad(top, bottom, left, right float32, o fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedLayout(top, bottom, left, right), o)
}

// tight removes the padding a Label reserves around its text, so labels line
// up with plain canvas text.
func tight(l fyne.CanvasObject) *fyne.Container {
	p := theme.InnerPadding()
	return pad(-p, -p, -p, -p, l)
}

// vcenter centres an object vertically without stretching it.
func vcenter(o fyne.CanvasObject) *fyne.Container {
	return container.NewVBox(layout.NewSpacer(), o, layout.NewSpacer())
}

// columnsLayout places objects side by side with weighted widths.
type columnsLayout struct {
	space   float32
	weights []float32
}

func columns(space float32, weights []float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(&columnsLayout{space: space, weights: weights}, objs...)
}

func (l *columnsLayout) weight(i int) float32 {
	if i < len(l.weights) && l.weights[i] > 0 {
		return l.weights[i]
	}
	return 1
}

func (l *columnsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 {
		return
	}
	var total float32
	for i := range objs {
		total += l.weight(i)
	}
	avail := size.Width - l.space*float32(len(objs)-1)
	x := float32(0)
	for i, o := range objs {
		w := avail * l.weight(i) / total
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w + l.space
	}
}

func (l *columnsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var total, unit, h float32
	for i, o := range objs {
		total += l.weight(i)
		m := o.MinSize()
		unit = fyne.Max(unit, m.Width/l.weight(i))
		h = fyne.Max(h, m.Height)
	}
	if len(objs) == 0 {
		return fyne.Size{}
	}
	return fyne.NewSize(unit*total+l.space*float32(len(objs)-1), h)
}

// ---- surfaces -----------------------------------------------------------

// panelInset is a rounded surface with a hairline border around content.
func panelInset(inset float32, content fyne.CanvasObject) *fyne.Container {
	bg := canvas.NewRectangle(ColorSurface)
	bg.CornerRadius = panelRadius
	bg.StrokeColor = ColorBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, pad(inset, inset, inset, inset, content))
}

func panel(content fyne.CanvasObject) *fyne.Container { return panelInset(16, content) }

// titledPanel is a panel with a heading row; trailing sits right of the title.
func titledPanel(title string, trailing fyne.CanvasObject, body fyne.CanvasObject) *fyne.Container {
	head := container.NewBorder(nil, nil, vcenter(sectionTitle(title)), trailing)
	return panel(container.NewBorder(pad(0, 12, 0, 0, head), nil, nil, nil, body))
}

func hairline() *canvas.Rectangle {
	r := canvas.NewRectangle(ColorSeparator)
	r.SetMinSize(fyne.NewSize(1, 1))
	return r
}

// emptyState is the centred message shown when a list has nothing in it.
func emptyState(title, hint string) *fyne.Container {
	t := newText(title, 14, ColorForegroundSub, styleBold)
	t.Alignment = fyne.TextAlignCenter
	h := newText(hint, 13, ColorMuted, fyne.TextStyle{})
	h.Alignment = fyne.TextAlignCenter
	return container.NewCenter(vstack(4, t, h))
}

// ---- page scaffold ------------------------------------------------------

// pageHeader is the title block at the top of a screen, with actions on the right.
func pageHeader(title string, sub *widget.Label, actions ...fyne.CanvasObject) *fyne.Container {
	text := vstack(5, newText(title, 22, ColorForeground, styleBold), tight(sub))
	return container.NewBorder(nil, nil, nil, vcenter(hstack(8, actions...)), text)
}

// page lays a header over a body with the standard margins.
func page(header, body fyne.CanvasObject) *fyne.Container {
	return pad(20, gutter, gutter, gutter,
		container.NewBorder(pad(0, 16, 0, 0, header), nil, nil, nil, body))
}

// searchEntry is a text filter with a magnifier.
func searchEntry(placeholder string, onChange func(string)) *widget.Entry {
	e := widget.NewEntry()
	e.SetPlaceHolder(placeholder)
	e.ActionItem = widget.NewIcon(theme.NewColoredResource(theme.SearchIcon(), theme.ColorNamePlaceHolder))
	e.OnChanged = onChange
	return e
}

// field puts a caption in front of a control in a toolbar.
func field(caption string, control fyne.CanvasObject, width float32) *fyne.Container {
	c := newText(caption, 13, ColorMuted, fyne.TextStyle{})
	outline := canvas.NewRectangle(color.Transparent)
	outline.StrokeColor = ColorBorder
	outline.StrokeWidth = 1
	outline.CornerRadius = theme.InputRadiusSize()
	return hstack(8, pad(0, 2, 4, 0, vcenter(c)),
		container.NewGridWrap(fyne.NewSize(width, control.MinSize().Height), container.NewStack(control, outline)))
}

// ---- status elements ----------------------------------------------------

// newDot returns a small status light and the object to place in a layout.
func newDot(c color.Color) (*canvas.Circle, fyne.CanvasObject) { return newDotSized(c, 8) }

func newDotSized(c color.Color, size float32) (*canvas.Circle, fyne.CanvasObject) {
	d := canvas.NewCircle(c)
	return d, container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(size), d))
}

// shortPath keeps the end of a long path, which is where the folder name is.
func shortPath(p string, max int) string {
	r := []rune(p)
	if len(r) <= max || max < 4 {
		return p
	}
	tail := r[len(r)-(max-1):]
	for i, c := range tail[:len(tail)/2] {
		if c == '/' || c == '\\' {
			tail = tail[i:]
			break
		}
	}
	return "…" + string(tail)
}

func setDot(c *canvas.Circle, col color.Color) {
	if c.FillColor != col {
		c.FillColor = col
		c.Refresh()
	}
}

// pill is a small tinted status label.
type pill struct {
	widget.BaseWidget
	bg    *canvas.Rectangle
	txt   *canvas.Text
	label string
	tone  tone
	ready bool
}

func newPill(label string, t tone) *pill {
	p := &pill{bg: canvas.NewRectangle(color.Transparent), txt: newText("", 12, ColorMuted, styleBold)}
	p.bg.CornerRadius = 7
	p.txt.Alignment = fyne.TextAlignCenter
	p.ExtendBaseWidget(p)
	p.Set(label, t)
	return p
}

// Set updates the pill and reports whether its width may have changed.
func (p *pill) Set(label string, t tone) bool {
	if p.ready && p.label == label && p.tone == t {
		return false
	}
	resized := p.label != label
	p.ready, p.label, p.tone = true, label, t
	c := t.color()
	p.txt.Text, p.txt.Color = label, c
	p.bg.FillColor = alpha(c, 34)
	p.Refresh()
	return resized
}

func (p *pill) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(p.bg, pad(3, 3, 9, 9, p.txt)))
}

// meter is a thin progress bar in the colour of its stream.
type meter struct {
	widget.BaseWidget
	value float64
	col   color.NRGBA
}

func newMeter(c color.NRGBA) *meter {
	m := &meter{col: c}
	m.ExtendBaseWidget(m)
	return m
}

func (m *meter) SetValue(v float64) {
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	m.value = v
	m.Refresh()
}

func (m *meter) CreateRenderer() fyne.WidgetRenderer {
	track := canvas.NewRectangle(ColorRaised)
	track.CornerRadius = 3
	fill := canvas.NewRectangle(m.col)
	fill.CornerRadius = 3
	return &meterRenderer{m: m, track: track, fill: fill}
}

type meterRenderer struct {
	m           *meter
	track, fill *canvas.Rectangle
}

func (r *meterRenderer) Layout(s fyne.Size) {
	r.track.Move(fyne.NewPos(0, 0))
	r.track.Resize(s)
	r.fill.Move(fyne.NewPos(0, 0))
	r.fill.Resize(fyne.NewSize(s.Width*float32(r.m.value), s.Height))
}
func (r *meterRenderer) MinSize() fyne.Size           { return fyne.NewSize(60, 6) }
func (r *meterRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.track, r.fill} }
func (r *meterRenderer) Destroy()                     {}
func (r *meterRenderer) Refresh() {
	r.Layout(r.m.Size())
	canvas.Refresh(r.m)
}

// ---- rail navigation ----------------------------------------------------

// railButton is one destination on the icon rail: icon above a small label.
type railButton struct {
	widget.BaseWidget
	label    string
	icon     fyne.Resource
	onTap    func()
	selected bool
	hovered  bool
}

var (
	_ fyne.Tappable     = (*railButton)(nil)
	_ desktop.Hoverable = (*railButton)(nil)
)

func newRailButton(label string, icon fyne.Resource, onTap func()) *railButton {
	b := &railButton{label: label, icon: icon, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *railButton) setSelected(on bool) {
	if b.selected != on {
		b.selected = on
		b.Refresh()
	}
}

func (b *railButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}
func (b *railButton) MouseIn(*desktop.MouseEvent)    { b.hovered = true; b.Refresh() }
func (b *railButton) MouseMoved(*desktop.MouseEvent) {}
func (b *railButton) MouseOut()                      { b.hovered = false; b.Refresh() }
func (b *railButton) Cursor() desktop.Cursor         { return desktop.PointerCursor }

func (b *railButton) CreateRenderer() fyne.WidgetRenderer {
	r := &railButtonRenderer{
		b:    b,
		bg:   canvas.NewRectangle(color.Transparent),
		icon: widget.NewIcon(b.icon),
		text: newText(b.label, 11, ColorMuted, fyne.TextStyle{}),
	}
	r.bg.CornerRadius = 12
	r.text.Alignment = fyne.TextAlignCenter
	r.Refresh()
	return r
}

type railButtonRenderer struct {
	b    *railButton
	bg   *canvas.Rectangle
	icon *widget.Icon
	text *canvas.Text
}

func (r *railButtonRenderer) Layout(s fyne.Size) {
	const iconSize = 22
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(s)
	r.icon.Resize(fyne.NewSquareSize(iconSize))
	r.icon.Move(fyne.NewPos((s.Width-iconSize)/2, 10))
	th := r.text.MinSize().Height
	r.text.Resize(fyne.NewSize(s.Width, th))
	r.text.Move(fyne.NewPos(0, 10+iconSize+3))
}

func (r *railButtonRenderer) MinSize() fyne.Size { return fyne.NewSize(72, 58) }
func (r *railButtonRenderer) Destroy()           {}
func (r *railButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.icon, r.text}
}

func (r *railButtonRenderer) Refresh() {
	switch {
	case r.b.selected:
		r.bg.FillColor = alpha(ColorAccent, 30)
		r.text.Color = ColorAccent
		r.text.TextStyle = styleBold
		r.icon.SetResource(theme.NewPrimaryThemedResource(r.b.icon))
	case r.b.hovered:
		r.bg.FillColor = color.NRGBA{R: 255, G: 255, B: 255, A: 14}
		r.text.Color = ColorForeground
		r.text.TextStyle = fyne.TextStyle{}
		r.icon.SetResource(theme.NewThemedResource(r.b.icon))
	default:
		r.bg.FillColor = color.Transparent
		r.text.Color = ColorMuted
		r.text.TextStyle = fyne.TextStyle{}
		r.icon.SetResource(theme.NewColoredResource(r.b.icon, theme.ColorNameDisabled))
	}
	r.bg.Refresh()
	r.text.Refresh()
}

// ---- segmented control --------------------------------------------------

// segment is one choice of a segmented control.
type segment struct {
	widget.BaseWidget
	label    string
	onTap    func()
	selected bool
	hovered  bool
}

var (
	_ fyne.Tappable     = (*segment)(nil)
	_ desktop.Hoverable = (*segment)(nil)
)

func (g *segment) Tapped(*fyne.PointEvent)        { g.onTap() }
func (g *segment) MouseIn(*desktop.MouseEvent)    { g.hovered = true; g.Refresh() }
func (g *segment) MouseMoved(*desktop.MouseEvent) {}
func (g *segment) MouseOut()                      { g.hovered = false; g.Refresh() }
func (g *segment) Cursor() desktop.Cursor         { return desktop.PointerCursor }

func (g *segment) CreateRenderer() fyne.WidgetRenderer {
	r := &segmentRenderer{g: g, bg: canvas.NewRectangle(color.Transparent), text: newText(g.label, 13, ColorMuted, fyne.TextStyle{})}
	r.bg.CornerRadius = 8
	r.text.Alignment = fyne.TextAlignCenter
	r.Refresh()
	return r
}

type segmentRenderer struct {
	g    *segment
	bg   *canvas.Rectangle
	text *canvas.Text
}

func (r *segmentRenderer) Layout(s fyne.Size) {
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(s)
	th := r.text.MinSize().Height
	r.text.Resize(fyne.NewSize(s.Width, th))
	r.text.Move(fyne.NewPos(0, (s.Height-th)/2))
}

func (r *segmentRenderer) MinSize() fyne.Size {
	// Measured in bold so a segment does not change width when it is selected.
	w := fyne.MeasureText(r.g.label, 13, styleBold).Width
	return fyne.NewSize(w+30, 32)
}
func (r *segmentRenderer) Destroy()                     {}
func (r *segmentRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.text} }

func (r *segmentRenderer) Refresh() {
	switch {
	case r.g.selected:
		r.bg.FillColor = rgb(40, 58, 70)
		r.text.Color = ColorForeground
		r.text.TextStyle = styleBold
	case r.g.hovered:
		r.bg.FillColor = color.NRGBA{R: 255, G: 255, B: 255, A: 10}
		r.text.Color = ColorForeground
		r.text.TextStyle = fyne.TextStyle{}
	default:
		r.bg.FillColor = color.Transparent
		r.text.Color = ColorMuted
		r.text.TextStyle = fyne.TextStyle{}
	}
	r.bg.Refresh()
	r.text.Refresh()
}

// segmented is a row of mutually exclusive choices.
type segmented struct {
	box      *fyne.Container
	items    []*segment
	selected int
	onSelect func(int)
}

func newSegmented(labels []string, onSelect func(int)) *segmented {
	s := &segmented{selected: -1, onSelect: onSelect}
	row := hstack(2)
	for i, l := range labels {
		g := &segment{label: l, onTap: func() { s.Select(i) }}
		g.ExtendBaseWidget(g)
		s.items = append(s.items, g)
		row.Add(g)
	}
	bg := canvas.NewRectangle(ColorSurface)
	bg.CornerRadius = 11
	bg.StrokeColor = ColorBorder
	bg.StrokeWidth = 1
	s.box = container.NewHBox(container.NewStack(bg, pad(4, 4, 4, 4, row)))
	return s
}

// Select marks a choice and notifies the owner.
func (s *segmented) Select(i int) {
	if i < 0 || i >= len(s.items) || i == s.selected {
		return
	}
	s.selected = i
	for n, g := range s.items {
		g.selected = n == i
		g.Refresh()
	}
	if s.onSelect != nil {
		s.onSelect(i)
	}
}
