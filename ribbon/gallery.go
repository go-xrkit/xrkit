package ribbon

import (
	"fmt"
	"math"
)

// Direction is which way an arrow key moves the gallery's selection.
type Direction int

// The four arrows.
const (
	Left Direction = iota
	Right
	Up
	Down
)

// String names the direction.
func (d Direction) String() string {
	switch d {
	case Left:
		return "left"
	case Right:
		return "right"
	case Up:
		return "up"
	case Down:
		return "down"
	}
	return fmt.Sprintf("Direction(%d)", int(d))
}

// View is the arc the glasses actually SHOW the viewer, in degrees. It is what
// [github.com/go-xrkit/xrkit/glasses.Profile.FOV] answers.
//
// It is not the panorama's window. The window covers the view plus a margin, so
// that a screen entering the view is already in the buffer; the gallery has to
// fit inside the VIEW, because a screen laid out in the margin is a screen the
// viewer cannot see, and the whole point of the gallery is seeing them all at
// once.
type View struct {
	HDeg, VDeg float64
}

// check rejects a view the gallery cannot be laid out in.
func (v View) check(p Pano) error {
	if v.HDeg <= 0 || v.HDeg > 360 || v.VDeg <= 0 || v.VDeg > 180 {
		return fmt.Errorf("%w: %g° x %g°", ErrViewSpan, v.HDeg, v.VDeg)
	}
	if v.HDeg > p.Window.HSpanDeg || v.VDeg > p.Window.VSpanDeg {
		return fmt.Errorf("%w: a %g°x%g° view in a %g°x%g° window",
			ErrViewWindow, v.HDeg, v.VDeg, p.Window.HSpanDeg, p.Window.VSpanDeg)
	}
	return nil
}

// Cell is where one screen sits in the gallery.
//
// The angles are relative to STRAIGHT AHEAD, not to a longitude on the ribbon:
// the gallery is head-locked, so a cell is at the same place in the buffer
// whatever the ribbon's yaw is. Centre and HalfSpan are longitudes, in radians;
// Height and HalfHeight are on the unit cylinder, the same units as the ribbon's
// band, growing UPWARDS. The pair is what a caller needs to draw a highlight
// round the selected screen without asking this package for pixels.
type Cell struct {
	// Row and Col are the cell's place in the grid, counting from the top left.
	Row, Col int
	// Centre and HalfSpan are the screen's longitude and half its angular width.
	Centre, HalfSpan float64
	// Height and HalfHeight are the middle of the screen and half its height, on
	// the unit cylinder.
	Height, HalfHeight float64
}

// cell is a Cell with the vertical table that draws it.
type cell struct {
	Cell
	v vtab
}

// shapeTie is how close two candidate grid shapes' areas have to be to count as
// equal. Relative, because the areas are products of angles, and generous,
// because a tie decided by the last bit of a float64 is a tie decided by which
// way a rounding went — and the tie-breaks below are the reasons a person can
// actually name.
const shapeTie = 1e-9

// Gallery is every screen at once, laid out as a grid in front of the viewer.
//
// # Why a grid, and why this grid
//
// The ribbon answers "what is next to this one" and answers it well. It cannot
// answer "where is the one three round the back", because that screen is behind
// the viewer's head and the only way to it is through the ones in between. The
// gallery is the other question, and it is worth its own mode only if every
// screen in it is big enough to RECOGNISE — which is a constraint on the layout,
// not a hope about it.
//
// So the shape is derived, not tabulated. For each number of columns from 1 to
// N, the rows follow (ceil), the cell size follows from the view and the gap,
// and every screen is fitted inside its cell keeping its own aspect ratio. The
// shape chosen is the one under which the screens cover the most angular area.
// That is the metric a viewer would use if they could see all the candidates at
// once, and it produces the answers a person would defend: six 16:9 screens in a
// 16:9 view come out 3x2 rather than 6x1, because a row of six is limited by its
// width to a sixth of the view and 3x2 is limited by its height to a half.
//
// Ties — and with equal screens in a view of the right shape they are exact —
// go first to the shape wasting the fewest cells, then to the one with the
// fewest rows, which is the wider of the two and matches a landscape view.
//
// # The grid is rigid
//
// A ragged last row is left-aligned rather than centred. Centring it is prettier
// and it breaks the columns: Down from the top right would land between two
// cells, and there is no answer to "which one" that a viewer could predict.
// Rigid columns make Up and Down exact, and the block as a whole is still
// centred in the view.
//
// # Head-locked, and stable
//
// Cells are placed relative to straight ahead, so the gallery does not turn with
// the ribbon, and the order is always the ribbon's own order starting from
// screen 0 — never rotated to bring the selection to the middle. A layout that
// moved every time it opened would cost the viewer the one thing a grid is for,
// which is knowing where a screen WAS.
type Gallery struct {
	c          *Compositor
	r          *Ribbon
	view       View
	cols, rows int
	cells      []cell
	sel        int
}

// NewGallery lays every screen on c's ribbon out as a grid inside view.
//
// It is built once, at startup, like the compositor: the geometry does not
// depend on the yaw or on the selection, so opening the gallery costs nothing
// and drawing it costs one pass over the cells.
func NewGallery(c *Compositor, view View) (*Gallery, error) {
	if err := view.check(c.pano); err != nil {
		return nil, err
	}
	r := c.r
	// The view in the units the layout is done in: longitude is linear, so the
	// width is just the arc; height on the cylinder is NOT linear in latitude, so
	// the height is the tangent of the half-span, doubled. Laying the grid out in
	// latitude instead would put the rows in the right place and give them the
	// wrong sizes, which reads as the top row being subtly squashed.
	availW := rad(view.HDeg)
	availH := 2 * math.Tan(rad(view.VDeg)/2)
	// The CONFIGURED gap, not [Ribbon.Gap]: under Spread the ribbon's actual gap
	// is whatever was left over from filling the circle, which is a fact about
	// the circle and has nothing to say about a grid in front of the viewer.
	//
	// The same number of degrees is used on both axes. Near the horizon a height
	// of h on the cylinder subtends atan(h) ≈ h radians, so equal numbers here
	// are gaps the viewer sees as equal, which is what a gap is for.
	gap := rad(r.lay.GapDeg)

	aspects := make([]float64, r.Len())
	for i := range aspects {
		aspects[i] = r.screens[i].Aspect()
	}
	cols, rows, ok := chooseShape(aspects, availW, availH, gap)
	if !ok {
		return nil, fmt.Errorf("%w: %d screens with a %g° gap in a %g°x%g° view",
			ErrGalleryFit, r.Len(), r.lay.GapDeg, view.HDeg, view.VDeg)
	}
	cellW := (availW - float64(cols-1)*gap) / float64(cols)
	cellH := (availH - float64(rows-1)*gap) / float64(rows)

	g := &Gallery{c: c, r: r, view: view, cols: cols, rows: rows, cells: make([]cell, r.Len())}
	vSpan := rad(c.pano.Window.VSpanDeg)
	for i := range g.cells {
		s := r.screens[i]
		row, col := i/cols, i%cols
		// Fit the whole screen inside its cell, keeping its shape: contain, not
		// cover. A screen cropped to fill its cell would be a screen the viewer
		// cannot identify by its corners, which is how people recognise the one
		// they want.
		w := math.Min(cellW, cellH*s.Aspect())
		// The block fills the available width and height exactly, by
		// construction, so centring it is a matter of starting half a view to the
		// left and half a view up.
		cl := cell{Cell: Cell{
			Row: row, Col: col,
			Centre:   -availW/2 + cellW/2 + float64(col)*(cellW+gap),
			HalfSpan: w / 2,
			Height:   availH/2 - cellH/2 - float64(row)*(cellH+gap),
			// Height from width and aspect, exactly as on the ribbon: an arc of α
			// radians on the unit cylinder is α long, so this keeps the pixels
			// square.
			HalfHeight: w / s.Aspect() / 2,
		}}
		// A cell narrower than one panorama column draws nothing at all. Refusing
		// it beats shipping a gallery with an invisible screen in it, which looks
		// like a capture failure and is a resolution mistake.
		if px := w / (2 * c.halfW) * float64(c.pano.W); px < 1 {
			return nil, fmt.Errorf("%w: screen %d would be %.2f panorama columns wide", ErrGalleryFit, i, px)
		}
		cl.v = buildVTab(c.pano.H, vSpan, cl.Height, cl.HalfHeight, s.H)
		if len(cl.v.rows) == 0 {
			return nil, fmt.Errorf("%w: %d rows over %g° do not reach gallery cell %d",
				ErrPanoTooShort, c.pano.H, c.pano.Window.VSpanDeg, i)
		}
		g.cells[i] = cl
	}
	return g, nil
}

// chooseShape picks the number of columns and rows, and reports false if no
// shape leaves room for a cell — a gap wider than the view divided by the
// screens it has to separate.
//
// It is a function of its own so that the choice can be tested as a choice,
// against the alternatives it rejected, rather than only through the pixels it
// eventually produces.
func chooseShape(aspects []float64, availW, availH, gap float64) (cols, rows int, ok bool) {
	n := len(aspects)
	best := -1.0
	for c := 1; c <= n; c++ {
		r := (n + c - 1) / c
		area, fits := fitArea(aspects, c, r, availW, availH, gap)
		if !fits {
			continue
		}
		switch {
		case area > best*(1+shapeTie):
			// Bigger by more than rounding: take it.
		case area >= best*(1-shapeTie) && ok:
			// A tie, and with equal screens in a view of the right shape these are
			// exact rather than near. Fewest wasted cells first, then fewest rows:
			// of two shapes showing the screens at the same size, the one that is
			// wider and less ragged is the one a person would have drawn.
			if e, be := c*r-n, cols*rows-n; e > be || (e == be && r >= rows) {
				continue
			}
		default:
			continue
		}
		cols, rows, best, ok = c, r, area, true
	}
	return cols, rows, ok
}

// fitArea is the angular area the screens cover in a cols x rows grid, and false
// if the gaps leave no room for a cell.
//
// Area, rather than the size of the smallest screen or the fraction of the view
// used, because it is the only one of the three that answers "how big are they"
// for screens of DIFFERENT shapes without letting one awkward portrait display
// dictate the shape of the whole grid.
func fitArea(aspects []float64, cols, rows int, availW, availH, gap float64) (float64, bool) {
	cellW := (availW - float64(cols-1)*gap) / float64(cols)
	cellH := (availH - float64(rows-1)*gap) / float64(rows)
	if cellW <= 0 || cellH <= 0 {
		return 0, false
	}
	area := 0.0
	for _, a := range aspects {
		// Contain: limited by the cell's width, or by its height times the aspect,
		// whichever bites first. The height that follows is w/a, so the area is
		// w²/a.
		w := math.Min(cellW, cellH*a)
		area += w * w / a
	}
	return area, true
}

// Cols is how many columns the grid has.
func (g *Gallery) Cols() int { return g.cols }

// Rows is how many rows the grid has. The last one may be ragged.
func (g *Gallery) Rows() int { return g.rows }

// Len is how many screens are in the gallery, which is all of them.
func (g *Gallery) Len() int { return len(g.cells) }

// View returns the view the gallery was laid out to fit inside.
func (g *Gallery) View() View { return g.view }

// At returns the i'th cell. It indexes, so an out-of-range i panics, like a
// slice and like [Ribbon.At].
func (g *Gallery) At(i int) Cell { return g.cells[i].Cell }

// Selected is the screen the viewer has highlighted.
func (g *Gallery) Selected() int { return g.sel }

// Select highlights screen i.
func (g *Gallery) Select(i int) error {
	if i < 0 || i >= len(g.cells) {
		return fmt.Errorf("%w: %d", ErrIndex, i)
	}
	g.sel = i
	return nil
}

// Move walks the selection one cell in a direction.
//
// # Left and right wrap; up and down clamp
//
// They are not the same kind of axis, and treating them alike is what makes a
// grid of a circle feel arbitrary.
//
// Left and right run along the RIBBON. That axis really is circular — the seam
// between the last screen and the first is a gap the viewer can turn through —
// so the selection wraps, and right past the end of a row continues at the start
// of the next, exactly as reading does and exactly as [Nav.Next] steps. Holding
// right therefore visits every screen and comes back to where it started, which
// is what a person holding a key is asking for. Wrapping within a row instead
// would trap the selection in that row forever.
//
// Up and down are not an axis of the arrangement at all: they exist because a
// line was folded into a grid, and the fold has no seam. So they clamp. Wrapping
// them would jump the selection a whole row — a dozen screens, on a big ribbon —
// in answer to a key that means "one".
//
// A ragged last row is reached the way a person would predict: down from a
// column with nothing under it lands on the LAST screen, the nearest thing
// below, rather than refusing to move. Down from the bottom row does nothing.
func (g *Gallery) Move(d Direction) error {
	n := len(g.cells)
	switch d {
	case Right:
		g.sel = (g.sel + 1) % n
	case Left:
		g.sel = (g.sel + n - 1) % n
	case Up:
		if g.sel >= g.cols {
			g.sel -= g.cols
		}
	case Down:
		if g.sel/g.cols < (n-1)/g.cols {
			g.sel = min(g.sel+g.cols, n-1)
		}
	default:
		return fmt.Errorf("%w: %s", ErrDirection, d)
	}
	return nil
}

// Frame appends the blits for the whole gallery and returns the extended slice.
// Passing dst[:0] of the previous frame's slice reuses the storage, and the
// frame then allocates nothing at all.
//
// There is no yaw argument, and that is the point: the gallery is head-locked,
// so its geometry is the same every frame and the blits are the same blits the
// ribbon produces. The application's existing blitter draws it unchanged.
//
// The selection is not drawn. Which screen is highlighted is in [Gallery.At] and
// [Gallery.Selected], for a caller to outline however it outlines things; a
// package that produces geometry has no business deciding what a highlight looks
// like.
func (g *Gallery) Frame(dst []Blit) []Blit {
	for i := range g.cells {
		cl := &g.cells[i]
		dst = g.c.place(dst, i, cl.Centre, cl.HalfSpan, &cl.v, g.r.screens[i].W)
	}
	return dst
}
