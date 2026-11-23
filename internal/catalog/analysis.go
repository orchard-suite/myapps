package catalog

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
)

// The analysis is the apps built here against the categories, and how each
// works with Orrery. A count is how many apps name a category, not how
// mature the category is: one app that fits a category is enough.

// IntegrationKinds are the three marks, in the order the table draws them.
var IntegrationKinds = []Option{
	{"data", "Data"},
	{"blueprints", "Blueprints"},
	{"actions", "Actions"},
}

// IntegrationMark is one of a card's three marks.
type IntegrationMark struct {
	Kind     Option
	Evidence Evidence
}

// On says whether the mark is set.
func (m IntegrationMark) On() bool { return m.Evidence.On() }

// Marks are the three marks in the order of IntegrationKinds.
func (i Integration) Marks() []IntegrationMark {
	ev := []Evidence{i.Data, i.Blueprints, i.Actions}
	out := make([]IntegrationMark, len(IntegrationKinds))
	for n, k := range IntegrationKinds {
		out[n] = IntegrationMark{Kind: k, Evidence: ev[n]}
	}
	return out
}

// AnalysisRow is one app people open or app built here: a dot per category it names, and its
// three marks. Marks is empty when the integration is unknown.
type AnalysisRow struct {
	Card  Card
	Dots  []bool
	Marks []IntegrationMark
}

// CategoryCount is how many rows name one category, and which.
type CategoryCount struct {
	Category Category
	Count    int
	Titles   []string
}

// Analysis is the rows, the per-category and per-mark counts, and the
// radar of the per-category counts.
type Analysis struct {
	Categories []Category
	Rows       []AnalysisRow
	Counts     []CategoryCount
	// MarkCounts is how many rows carry each mark, in the order of
	// IntegrationKinds.
	MarkCounts []int
	// Known is false when the integration is unknown.
	Known bool
	Radar Radar
}

// AnalysisOf is the apps people open and the apps built here, by title, against the known categories and
// against Orrery.
func AnalysisOf(l Landscape) Analysis {
	a := Analysis{Categories: l.Categories, Known: l.SourcesKnown, MarkCounts: make([]int, len(IntegrationKinds))}
	for _, c := range l.Cards {
		if c.Kind != "built" && c.Kind != "app" {
			continue
		}
		row := AnalysisRow{Card: c, Dots: make([]bool, len(l.Categories))}
		for i, p := range l.Categories {
			row.Dots[i] = c.Serves(p.Key)
		}
		if a.Known && c.Integration != nil {
			row.Marks = c.Integration.Marks()
		}
		a.Rows = append(a.Rows, row)
	}
	slices.SortStableFunc(a.Rows, func(x, y AnalysisRow) int {
		return cmp.Compare(strings.ToLower(x.Card.Title), strings.ToLower(y.Card.Title))
	})
	a.Counts = make([]CategoryCount, len(l.Categories))
	for i, p := range l.Categories {
		a.Counts[i] = CategoryCount{Category: p, Titles: []string{}}
	}
	for _, r := range a.Rows {
		for i, on := range r.Dots {
			if on {
				a.Counts[i].Count++
				a.Counts[i].Titles = append(a.Counts[i].Titles, r.Card.Title)
			}
		}
		for i, m := range r.Marks {
			if m.On() {
				a.MarkCounts[i]++
			}
		}
	}
	counts := make([]int, len(a.Counts))
	names := make([]string, len(a.Counts))
	for i, c := range a.Counts {
		counts[i], names[i] = c.Count, c.Category.Name
	}
	a.Radar = RadarOf(names, counts)
	return a
}

// Point is a place in the radar's drawing.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Line is one line of an axis's label.
type Line struct {
	Text string
	At   Point
}

// RadarStep is where an axis's point and its number sit for one count.
type RadarStep struct {
	Point  Point `json:"point"`
	Number Point `json:"number"`
}

// RadarAxis is one category's spoke: its end, its label, and where its
// point and number sit for every count from 0 to the radar's Max, so a
// page that filters the rows moves the point without any geometry of
// its own.
type RadarAxis struct {
	Count  int
	End    Point
	Anchor string
	Lines  []Line
	// Tag is where the category's number sits, just past the end, for a
	// screen too narrow for the names.
	Tag   Point
	Steps []RadarStep
}

// At is where the axis's point sits for its own count.
func (a RadarAxis) At() RadarStep { return a.Steps[min(a.Count, len(a.Steps)-1)] }

// Tracks are every axis's steps, in axis order, for a page that moves
// the points as it filters.
func (r Radar) Tracks() [][]RadarStep {
	out := make([][]RadarStep, len(r.Axes))
	for i, a := range r.Axes {
		out[i] = a.Steps
	}
	return out
}

// Ring is one value's polygon behind the spokes.
type Ring struct {
	Value  int
	Points string
}

// Radar is the drawing of the per-category counts: the size of its view
// box, its centre, the rings, the axes in category order, and the area
// the counts enclose. The view box holds the rings and the numbers on
// the spokes; the names fall outside it, so the page leaves room for
// them where it shows them.
type Radar struct {
	Width, Height float64
	Center        Point
	Radius        float64
	Max           int
	Rings         []Ring
	Axes          []RadarAxis
	Area          string
}

// The radar's drawing, in view box units.
const (
	radarRadius  = 150
	radarWidth   = 2 * (radarRadius + tagGap + 12)
	radarHeight  = radarWidth
	labelGap     = 18
	tagGap       = 16
	numberGap    = 14
	labelLine    = 14
	labelWrap    = 16
	middleWithin = 0.12
)

// RadarMax is the outer ring's value: the largest count, at least one,
// rounded up to an even number above five so the rings step by two.
func RadarMax(counts []int) int {
	m := max(1, slices.Max(append([]int{0}, counts...)))
	if m > 5 && m%2 == 1 {
		m++
	}
	return m
}

// RadarStepOf is the value between rings.
func RadarStepOf(max int) int {
	if max > 5 {
		return 2
	}
	return 1
}

// RadarOf lays out one spoke per name, clockwise from the top, with a
// point at each count. Fewer than three names draw no radar.
func RadarOf(names []string, counts []int) Radar {
	r := Radar{Width: radarWidth, Height: radarHeight, Center: Point{radarWidth / 2, radarHeight / 2}, Radius: radarRadius}
	n := len(names)
	if n < 3 || len(counts) != n {
		return Radar{}
	}
	r.Max = RadarMax(counts)
	unit := func(i int) (float64, float64) {
		a := -math.Pi/2 + float64(i)*2*math.Pi/float64(n)
		return math.Cos(a), math.Sin(a)
	}
	at := func(i int, dist float64) Point {
		dx, dy := unit(i)
		return Point{round(r.Center.X + dx*dist), round(r.Center.Y + dy*dist)}
	}
	scale := r.Radius / float64(r.Max)
	for v := RadarStepOf(r.Max); v <= r.Max; v += RadarStepOf(r.Max) {
		ps := make([]Point, n)
		for i := range n {
			ps[i] = at(i, float64(v)*scale)
		}
		r.Rings = append(r.Rings, Ring{Value: v, Points: Points(ps)})
	}
	area := make([]Point, n)
	for i := range n {
		dx, dy := unit(i)
		ax := RadarAxis{Count: counts[i], End: at(i, r.Radius)}
		for v := 0; v <= r.Max; v++ {
			ax.Steps = append(ax.Steps, RadarStep{Point: at(i, float64(v)*scale), Number: at(i, float64(v)*scale+numberGap)})
		}
		ax.Anchor, ax.Lines = label(names[i], at(i, r.Radius+labelGap), dx, dy)
		ax.Tag = at(i, r.Radius+tagGap)
		area[i] = ax.At().Point
		r.Axes = append(r.Axes, ax)
	}
	r.Area = Points(area)
	return r
}

// label places an axis's name past its end: centred over a spoke near
// the vertical, else to the side the spoke points; above the end for an
// upper spoke, below it for a lower one, centred on it otherwise.
func label(name string, p Point, dx, dy float64) (string, []Line) {
	anchor := "middle"
	switch {
	case dx > middleWithin:
		anchor = "start"
	case dx < -middleWithin:
		anchor = "end"
	}
	words := Wrap(name, labelWrap)
	h := float64(len(words)) * labelLine
	// A line's baseline sits about three quarters down its height.
	top := p.Y - h/2
	switch {
	case dy < -0.5:
		top = p.Y - h
	case dy > 0.5:
		top = p.Y
	}
	lines := make([]Line, len(words))
	for i, w := range words {
		lines[i] = Line{Text: w, At: Point{p.X, round(top + float64(i)*labelLine + labelLine*0.75)}}
	}
	return anchor, lines
}

// Wrap breaks a name into lines of whole words, each at most width
// characters unless one word is longer.
func Wrap(name string, width int) []string {
	var lines []string
	for _, w := range strings.Fields(name) {
		if n := len(lines); n > 0 && len(lines[n-1])+1+len(w) <= width {
			lines[n-1] += " " + w
			continue
		}
		lines = append(lines, w)
	}
	return lines
}

// Points is a polygon's points attribute.
func Points(ps []Point) string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = num(p.X) + "," + num(p.Y)
	}
	return strings.Join(parts, " ")
}

func round(v float64) float64 { return math.Round(v*10) / 10 }

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
