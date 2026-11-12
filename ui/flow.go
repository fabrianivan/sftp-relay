package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The relay map: where files come from, where they go, and whether anything
// is travelling right now. Each stream keeps its colour everywhere in the app.

// Streams of the map.
const (
	streamSource = iota // source server → relay
	streamLocal         // local folder → relay
	streamTarget        // relay → target server
	streamCount
)

var streamColors = [streamCount]color.NRGBA{ColorSource, ColorLocal, ColorAccent}

// Theme colour names for the stream colours, so icons can be tinted with them.
const (
	colorNameSource fyne.ThemeColorName = "streamSource"
	colorNameLocal  fyne.ThemeColorName = "streamLocal"
)

// flowNode is one station of the map: a server or a folder.
type flowNode struct {
	box    *fyne.Container
	bg     *canvas.Rectangle
	chip   *canvas.Rectangle
	icon   *widget.Icon
	res    fyne.Resource
	tint   fyne.ThemeColorName
	col    color.NRGBA
	title  *canvas.Text
	detail *widget.Label
	badge  *pill
	on     bool
	ready  bool
}

func newFlowNode(title string, res fyne.Resource, col color.NRGBA, tint fyne.ThemeColorName) *flowNode {
	n := &flowNode{
		bg:     canvas.NewRectangle(ColorSurface),
		chip:   canvas.NewRectangle(alpha(col, 36)),
		icon:   widget.NewIcon(res),
		res:    res,
		tint:   tint,
		col:    col,
		title:  newText(title, 14, ColorForeground, styleBold),
		detail: mutedLabel(""),
		badge:  newPill("Off", toneNeutral),
	}
	n.bg.CornerRadius = 14
	n.bg.StrokeWidth = 1
	n.chip.CornerRadius = 10
	chip := container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(40),
		container.NewStack(n.chip, container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(20), n.icon)))))
	head := container.NewBorder(nil, nil, nil, n.badge, vcenter(n.title))
	n.box = container.NewStack(n.bg, pad(10, 10, 14, 14,
		container.NewBorder(nil, nil, pad(0, 0, 0, 12, chip), nil,
			vcenter(vstack(3, head, tight(n.detail))))))
	return n
}

// set updates the station. A station that is not configured is drawn as an
// empty slot, so the shape of the map never changes.
func (n *flowNode) set(on bool, detail, badge string, t tone) {
	if !n.ready || n.on != on {
		n.ready, n.on = true, on
		if on {
			n.bg.FillColor = ColorRaised
			n.bg.StrokeColor = alpha(n.col, 70)
			n.chip.FillColor = alpha(n.col, 40)
			n.title.Color = ColorForeground
			n.icon.SetResource(theme.NewColoredResource(n.res, n.tint))
		} else {
			n.bg.FillColor = color.Transparent
			n.bg.StrokeColor = ColorBorder
			n.chip.FillColor = ColorSurface
			n.title.Color = ColorMuted
			n.icon.SetResource(theme.NewColoredResource(n.res, theme.ColorNamePlaceHolder))
		}
		n.bg.Refresh()
		n.chip.Refresh()
		n.title.Refresh()
	}
	if n.detail.Text != detail {
		n.detail.SetText(detail)
	}
	if n.badge.Set(badge, t) {
		n.box.Refresh() // the badge changed width
	}
}

// flowMap draws the stations, the lines between them and the relay hub.
type flowMap struct {
	widget.BaseWidget
	source, local, target *flowNode

	on     [streamCount]bool // the stream is configured
	moving [streamCount]bool // files are travelling on it
	ends   [streamCount][2]fyne.Position

	hubValue, hubCaption string
	hubFrac              float32
	hubColor             color.NRGBA

	phase   float32
	shown   bool
	anim    *fyne.Animation
	running bool
}

func newFlowMap() *flowMap {
	m := &flowMap{
		source:   newFlowNode("Source server", theme.StorageIcon(), ColorSource, colorNameSource),
		local:    newFlowNode("Local folder", theme.FolderIcon(), ColorLocal, colorNameLocal),
		target:   newFlowNode("Target server", theme.UploadIcon(), ColorAccent, theme.ColorNamePrimary),
		hubColor: ColorMuted,
		shown:    true,
	}
	m.ExtendBaseWidget(m)
	return m
}

// setStream says whether a stream exists and whether files are moving on it.
func (m *flowMap) setStream(i int, on, moving bool) {
	moving = moving && on
	if m.on[i] == on && m.moving[i] == moving {
		return
	}
	m.on[i], m.moving[i] = on, moving
	m.Refresh()
	m.syncAnimation()
}

// setHub updates the dial in the middle: a short value, a caption, and how
// much of the wait until the next scan has passed (0–1).
func (m *flowMap) setHub(value, caption string, frac float32, c color.NRGBA) {
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	if m.hubValue == value && m.hubCaption == caption && m.hubFrac == frac && m.hubColor == c {
		return
	}
	m.hubValue, m.hubCaption, m.hubFrac, m.hubColor = value, caption, frac, c
	m.Refresh()
}

// setShown pauses the animation while the dashboard is not on screen.
func (m *flowMap) setShown(v bool) {
	m.shown = v
	m.syncAnimation()
}

// setPhase moves the travelling dots; 0–1 is one trip along a line.
func (m *flowMap) setPhase(p float32) {
	m.phase = p
	m.Refresh()
}

// syncAnimation runs the animation only while something is moving on screen.
func (m *flowMap) syncAnimation() {
	want := m.shown && (m.moving[streamSource] || m.moving[streamLocal] || m.moving[streamTarget])
	if want == m.running {
		return
	}
	m.running = want
	if !want {
		m.anim.Stop()
		return
	}
	if m.anim == nil {
		m.anim = fyne.NewAnimation(1500*time.Millisecond, m.setPhase)
		m.anim.RepeatCount = fyne.AnimationRepeatForever
		m.anim.Curve = fyne.AnimationLinear
	}
	m.anim.Start()
}

const (
	flowNodeHeight   = 70
	flowNodeMinWidth = 272 // room for the longest title next to its badge
	flowRowGap       = 14
	flowHubSize      = 104
	flowDots         = 3 // travelling dots per stream
)

func (m *flowMap) CreateRenderer() fyne.WidgetRenderer {
	r := &flowRenderer{m: m}
	for i := range r.lines {
		r.lines[i] = canvas.NewLine(ColorBorder)
		r.lines[i].StrokeWidth = 2.5
		for k := 0; k < flowDots; k++ {
			halo := canvas.NewCircle(alpha(streamColors[i], 46))
			dot := canvas.NewCircle(streamColors[i])
			r.halos[i] = append(r.halos[i], halo)
			r.dots[i] = append(r.dots[i], dot)
		}
	}
	r.plate = canvas.NewCircle(ColorSurface)
	r.track = canvas.NewArc(0, 360, 0.88, ColorRaised)
	r.arc = canvas.NewArc(0, 1, 0.88, ColorAccent)
	r.arc.CornerRadius = 3
	r.value = newText("", 24, ColorForeground, styleBold)
	r.value.Alignment = fyne.TextAlignCenter
	r.caption = newText("", 11, ColorMuted, fyne.TextStyle{})
	r.caption.Alignment = fyne.TextAlignCenter

	for i := range r.lines {
		r.objects = append(r.objects, r.lines[i])
	}
	for i := range r.lines {
		for k := 0; k < flowDots; k++ {
			r.objects = append(r.objects, r.halos[i][k], r.dots[i][k])
		}
	}
	r.objects = append(r.objects, r.plate, r.track, r.arc, r.value, r.caption, m.source.box, m.local.box, m.target.box)
	r.Refresh()
	return r
}

type flowRenderer struct {
	m       *flowMap
	lines   [streamCount]*canvas.Line
	dots    [streamCount][]*canvas.Circle
	halos   [streamCount][]*canvas.Circle
	plate   *canvas.Circle
	track   *canvas.Arc
	arc     *canvas.Arc
	value   *canvas.Text
	caption *canvas.Text
	objects []fyne.CanvasObject
}

func (r *flowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *flowRenderer) Destroy()                     {}

func (r *flowRenderer) MinSize() fyne.Size {
	return fyne.NewSize(2*flowNodeMinWidth+flowHubSize+150, 2*flowNodeHeight+flowRowGap)
}

func (r *flowRenderer) Layout(s fyne.Size) {
	m := r.m
	w := fyne.Min(380, fyne.Max(flowNodeMinWidth, s.Width*0.32))
	cx, cy := s.Width/2, s.Height/2
	node := fyne.NewSize(w, flowNodeHeight)

	m.source.box.Resize(node)
	m.source.box.Move(fyne.NewPos(0, cy-flowRowGap/2-flowNodeHeight))
	m.local.box.Resize(node)
	m.local.box.Move(fyne.NewPos(0, cy+flowRowGap/2))
	m.target.box.Resize(node)
	m.target.box.Move(fyne.NewPos(s.Width-w, cy-flowNodeHeight/2))

	const clear = 12 // space between a line and what it connects
	hubLeft, hubRight := cx-flowHubSize/2-clear, cx+flowHubSize/2+clear
	m.ends[streamSource] = [2]fyne.Position{{X: w + clear, Y: cy - flowRowGap/2 - flowNodeHeight/2}, {X: hubLeft, Y: cy - 10}}
	m.ends[streamLocal] = [2]fyne.Position{{X: w + clear, Y: cy + flowRowGap/2 + flowNodeHeight/2}, {X: hubLeft, Y: cy + 10}}
	m.ends[streamTarget] = [2]fyne.Position{{X: hubRight, Y: cy}, {X: s.Width - w - clear, Y: cy}}
	for i, l := range r.lines {
		l.Position1, l.Position2 = m.ends[i][0], m.ends[i][1]
	}

	hub := fyne.NewSquareSize(flowHubSize)
	corner := fyne.NewPos(cx-flowHubSize/2, cy-flowHubSize/2)
	for _, o := range []fyne.CanvasObject{r.plate, r.track, r.arc} {
		o.Resize(hub)
		o.Move(corner)
	}
	vh, ch := r.value.MinSize().Height, r.caption.MinSize().Height
	top := cy - (vh+ch)/2
	r.value.Resize(fyne.NewSize(flowHubSize, vh))
	r.value.Move(fyne.NewPos(corner.X, top))
	r.caption.Resize(fyne.NewSize(flowHubSize, ch))
	r.caption.Move(fyne.NewPos(corner.X, top+vh))

	r.placeDots()
}

// placeDots puts the travelling dots where the current phase says they are.
func (r *flowRenderer) placeDots() {
	const dot, halo = 7, 15
	for i := range r.dots {
		a, b := r.m.ends[i][0], r.m.ends[i][1]
		for k := range r.dots[i] {
			t := r.m.phase + float32(k)/flowDots
			t -= float32(int(t))
			x, y := a.X+(b.X-a.X)*t, a.Y+(b.Y-a.Y)*t
			r.dots[i][k].Resize(fyne.NewSquareSize(dot))
			r.dots[i][k].Move(fyne.NewPos(x-dot/2, y-dot/2))
			r.halos[i][k].Resize(fyne.NewSquareSize(halo))
			r.halos[i][k].Move(fyne.NewPos(x-halo/2, y-halo/2))
		}
	}
}

func (r *flowRenderer) Refresh() {
	m := r.m
	for i, l := range r.lines {
		switch {
		case m.moving[i]:
			l.StrokeColor = streamColors[i]
		case m.on[i]:
			l.StrokeColor = alpha(streamColors[i], 95)
		default:
			l.StrokeColor = ColorBorder
		}
		for k := range r.dots[i] {
			if m.moving[i] {
				r.dots[i][k].Show()
				r.halos[i][k].Show()
			} else {
				r.dots[i][k].Hide()
				r.halos[i][k].Hide()
			}
		}
	}

	r.arc.FillColor = m.hubColor
	r.arc.EndAngle = 360 * m.hubFrac
	if m.hubFrac <= 0 {
		r.arc.Hide()
	} else {
		r.arc.Show()
	}
	r.value.Text, r.caption.Text = m.hubValue, m.hubCaption
	if len(m.hubValue) > 3 {
		r.value.TextSize = 15
	} else {
		r.value.TextSize = 24
	}

	r.Layout(m.Size())
	canvas.Refresh(m)
}

// hourChart is a 24-bar histogram of the last day: one bar per hour, the
// newest on the right, failures stacked in red on top of deliveries.
type hourChart struct {
	widget.BaseWidget
	ok, bad [24]int
}

func newHourChart() *hourChart {
	c := &hourChart{}
	c.ExtendBaseWidget(c)
	return c
}

func (c *hourChart) set(ok, bad [24]int) {
	if c.ok == ok && c.bad == bad {
		return
	}
	c.ok, c.bad = ok, bad
	c.Refresh()
}

// peak is the busiest hour's file count.
func (c *hourChart) peak() int {
	max := 0
	for i := range c.ok {
		if v := c.ok[i] + c.bad[i]; v > max {
			max = v
		}
	}
	return max
}

func (c *hourChart) CreateRenderer() fyne.WidgetRenderer {
	r := &hourChartRenderer{c: c}
	for i := range r.ok {
		r.ok[i] = canvas.NewRectangle(ColorRaised)
		r.ok[i].CornerRadius = 2
		r.bad[i] = canvas.NewRectangle(ColorError)
		r.bad[i].CornerRadius = 2
		r.objects = append(r.objects, r.ok[i], r.bad[i])
	}
	return r
}

type hourChartRenderer struct {
	c       *hourChart
	ok, bad [24]*canvas.Rectangle
	objects []fyne.CanvasObject
}

func (r *hourChartRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *hourChartRenderer) Destroy()                     {}
func (r *hourChartRenderer) MinSize() fyne.Size           { return fyne.NewSize(220, 44) }

func (r *hourChartRenderer) Layout(s fyne.Size) {
	const space, stub = 4, 3
	n := float32(len(r.ok))
	w := (s.Width - space*(n-1)) / n
	peak := float32(r.c.peak())
	for i := range r.ok {
		x := float32(i) * (w + space)
		okH, badH := float32(stub), float32(0)
		if peak > 0 {
			okH = fyne.Max(stub, s.Height*float32(r.c.ok[i])/peak)
			badH = s.Height * float32(r.c.bad[i]) / peak
			if r.c.bad[i] > 0 {
				badH = fyne.Max(stub, badH)
			}
			if r.c.ok[i] == 0 && r.c.bad[i] > 0 {
				okH = 0
			}
		}
		if okH+badH > s.Height {
			okH = s.Height - badH
		}
		r.ok[i].Resize(fyne.NewSize(w, okH))
		r.ok[i].Move(fyne.NewPos(x, s.Height-okH))
		r.bad[i].Resize(fyne.NewSize(w, badH))
		r.bad[i].Move(fyne.NewPos(x, s.Height-okH-badH))
	}
}

func (r *hourChartRenderer) Refresh() {
	for i := range r.ok {
		switch {
		case r.c.ok[i] == 0:
			r.ok[i].FillColor = ColorRaised
		case i == len(r.ok)-1:
			r.ok[i].FillColor = ColorAccent
		default:
			r.ok[i].FillColor = alpha(ColorAccent, 150)
		}
		if r.c.bad[i] > 0 {
			r.bad[i].Show()
		} else {
			r.bad[i].Hide()
		}
	}
	r.Layout(r.c.Size())
	canvas.Refresh(r.c)
}
