package ribbon

import (
	"errors"
	"math"
	"testing"
)

// The application's own numbers, because the layout has to be right for THEM.
//
// A VITURE Beast in side-by-side 3D shows 1920x1080 per eye and the application
// sizes the ribbon so that one screen is exactly one full view — 51.57° of
// longitude. The panorama it composites into covers the view plus a margin:
// 2814x1933 pixels over 76°x54°. So the gallery is laid out in 51.57°x28.38° and
// drawn into a buffer half as wide again, and the whole question is whether it
// stays inside the smaller one.
const beastScreenDeg = 51.57

// oneScreenView is the view a single screen of the given aspect fills exactly
// when it spans hDeg of longitude. Derived rather than written down, so that
// changing the density does not silently leave the view describing a different
// pair of glasses.
func oneScreenView(hDeg, aspect float64) View {
	return View{HDeg: hDeg, VDeg: 2 * deg(math.Atan(rad(hDeg)/aspect/2))}
}

func beastView() View { return oneScreenView(beastScreenDeg, 16.0/9.0) }

func beastPano() Pano { return window(76, 54, 2814, 1933) }

// beastLayout is the ribbon that makes one screen one view.
func beastLayout() Layout {
	return Layout{
		DensityDeg:   beastScreenDeg / (16.0 / 9.0),
		GapDeg:       3,
		FullWidthDeg: beastScreenDeg,
		Arrangement:  Spread,
	}
}

// alike is n screens of the same shape, which is what a desk of identical
// monitors looks like and the case where the grid shape has to be decided on the
// view alone.
func alike(n, w, h int) []Screen {
	s := make([]Screen, n)
	for i := range s {
		s[i] = Screen{ID: string(rune('a' + i)), W: w, H: h}
	}
	return s
}

func mustGallery(t *testing.T, c *Compositor, v View) *Gallery {
	t.Helper()
	g, err := NewGallery(c, v)
	if err != nil {
		t.Fatalf("NewGallery: %v", err)
	}
	return g
}

// beastGallery is six identical screens on a Beast, the configuration the
// gallery exists for.
func beastGallery(t *testing.T, n int, pano Pano) (*Ribbon, *Compositor, *Gallery) {
	t.Helper()
	// Six screens at one view each is 309° of ribbon; the density has to come
	// down for more of them to fit in a circle at all. The gallery's shape does
	// not depend on the density — only on the view, the gap and the aspects — so
	// this changes what the ribbon looks like and not what the grid does.
	lay := beastLayout()
	if n > 6 {
		lay.DensityDeg = 300 / float64(n) / (16.0 / 9.0)
	}
	r := mustPlace(t, alike(n, 1920, 1080), lay)
	c := mustComposite(t, r, pano)
	return r, c, mustGallery(t, c, beastView())
}

func TestDirectionString(t *testing.T) {
	for _, tc := range []struct {
		d    Direction
		want string
	}{{Left, "left"}, {Right, "right"}, {Up, "up"}, {Down, "down"}, {Direction(9), "Direction(9)"}} {
		if got := tc.d.String(); got != tc.want {
			t.Errorf("Direction(%d).String() = %q, want %q", int(tc.d), got, tc.want)
		}
	}
	if got := ModeGallery.String(); got != "gallery" {
		t.Errorf("ModeGallery.String() = %q, want %q", got, "gallery")
	}
}

func TestGalleryRejectsWhatItCannotLayOut(t *testing.T) {
	_, c, _ := beastGallery(t, 6, beastPano())
	for _, tc := range []struct {
		name string
		view View
		want error
	}{
		{"no width", View{HDeg: 0, VDeg: 28}, ErrViewSpan},
		{"more than a circle", View{HDeg: 361, VDeg: 28}, ErrViewSpan},
		{"no height", View{HDeg: 50, VDeg: 0}, ErrViewSpan},
		{"more than pole to pole", View{HDeg: 50, VDeg: 181}, ErrViewSpan},
		// A view wider than the buffer would put the outer cells in columns that
		// do not exist, and place would clip them — which is exactly the "nothing
		// falls outside the view" promise broken quietly.
		{"wider than the window", View{HDeg: 77, VDeg: 28}, ErrViewWindow},
		{"taller than the window", View{HDeg: 50, VDeg: 55}, ErrViewWindow},
	} {
		g, err := NewGallery(c, tc.view)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if g != nil {
			t.Errorf("%s: got a gallery as well as an error", tc.name)
		}
	}

	// A gap wider than the view divided by the screens it has to separate: every
	// candidate shape leaves a cell with no room at all.
	wide := mustPlace(t, alike(3, 1920, 1080), Layout{
		DensityDeg: beastScreenDeg / (16.0 / 9.0), GapDeg: 30, FullWidthDeg: 60, Arrangement: Spread})
	if _, err := NewGallery(mustComposite(t, wide, beastPano()), beastView()); !errors.Is(err, ErrGalleryFit) {
		t.Errorf("a 30° gap in a %g° view: err = %v, want %v", beastView().HDeg, err, ErrGalleryFit)
	}

	// A buffer too coarse for the grid: the cells are a third of a column wide,
	// so every one of them would draw nothing.
	r := mustPlace(t, alike(6, 1920, 1080), beastLayout())
	if _, err := NewGallery(mustComposite(t, r, window(360, 70, 8, 512)), beastView()); !errors.Is(err, ErrGalleryFit) {
		t.Errorf("an 8-column panorama: err = %v, want %v", err, ErrGalleryFit)
	}

	// And too few ROWS for the grid, which is not the same thing: the band across
	// the middle still lands on a row, and the gallery's rows sit above and below
	// it, between two samples.
	if _, err := NewGallery(mustComposite(t, r, window(360, 170, 4000, 5)), beastView()); !errors.Is(err, ErrPanoTooShort) {
		t.Errorf("a 5-row panorama: err = %v, want %v", err, ErrPanoTooShort)
	}
}

// TestGridShapeIsDerivedFromTheView: the shapes a person would draw, and the
// proof that they were CHOSEN — every rejected shape shows the screens smaller.
func TestGridShapeIsDerivedFromTheView(t *testing.T) {
	for _, tc := range []struct {
		n          int
		cols, rows int
	}{
		{1, 1, 1},
		{2, 2, 1},
		{3, 2, 2},
		{4, 2, 2},
		{5, 3, 2},
		{6, 3, 2}, // the headline: not 6x1, which is unreadable
		{7, 3, 3},
		{9, 3, 3},
		{12, 4, 3},
	} {
		_, _, g := beastGallery(t, tc.n, beastPano())
		if g.Cols() != tc.cols || g.Rows() != tc.rows {
			t.Errorf("%d screens: %dx%d, want %dx%d", tc.n, g.Cols(), g.Rows(), tc.cols, tc.rows)
		}
		if g.Len() != tc.n {
			t.Errorf("%d screens: Len = %d", tc.n, g.Len())
		}
		if g.View() != beastView() {
			t.Errorf("%d screens: View = %+v", tc.n, g.View())
		}
		// The claim is not that this shape is nice, it is that it is the biggest.
		// Measured against every alternative, with the same fitting rule.
		v := beastView()
		aspects := make([]float64, tc.n)
		for i := range aspects {
			aspects[i] = 16.0 / 9.0
		}
		availW, availH := rad(v.HDeg), 2*math.Tan(rad(v.VDeg)/2)
		gap := rad(3)
		best, _ := fitArea(aspects, g.Cols(), g.Rows(), availW, availH, gap)
		for cols := 1; cols <= tc.n; cols++ {
			rows := (tc.n + cols - 1) / cols
			area, ok := fitArea(aspects, cols, rows, availW, availH, gap)
			if ok && area > best*(1+shapeTie) {
				t.Errorf("%d screens: chose %dx%d covering %.6f, but %dx%d covers %.6f",
					tc.n, g.Cols(), g.Rows(), best, cols, rows, area)
			}
		}
	}
	// A row of six across the Beast's view really is the disaster it is claimed
	// to be: each screen a sixth of the width, against a half at 3x2.
	v := beastView()
	aspects := make([]float64, 6)
	for i := range aspects {
		aspects[i] = 16.0 / 9.0
	}
	availW, availH := rad(v.HDeg), 2*math.Tan(rad(v.VDeg)/2)
	flat, _ := fitArea(aspects, 6, 1, availW, availH, rad(3))
	grid, _ := fitArea(aspects, 3, 2, availW, availH, rad(3))
	if grid < 3*flat {
		t.Errorf("3x2 covers %.6f and 6x1 covers %.6f; the metric does not discriminate", grid, flat)
	}
}

// TestShapeTieBreaks reaches the cases the Beast cannot: two shapes that show
// the screens at EXACTLY the same size, where the choice is made by the
// tie-breaks rather than by the area.
func TestShapeTieBreaks(t *testing.T) {
	// Two square screens in a square view. 2x1 and 1x2 are the same size to the
	// last bit; the wider one wins.
	cols, rows, ok := chooseShape([]float64{1, 1}, 1, 1, 0)
	if !ok || cols != 2 || rows != 1 {
		t.Errorf("two squares in a square view: %dx%d ok=%v, want 2x1", cols, rows, ok)
	}
	// Five squares in a view so wide that every shape with two rows is limited by
	// its HEIGHT, and so gives the screens exactly the same size. 3x2 wastes one
	// cell and 4x2 wastes three, so the tie goes to 3x2 — and then 5x1, which is
	// limited by width instead, beats both on area and takes it.
	if area3, _ := fitArea([]float64{1, 1, 1, 1, 1}, 3, 2, 6, 1, 0); area3 != 1.25 {
		t.Errorf("3x2 in a 6x1 view covers %v, want 1.25 — the tie this test needs is not there", area3)
	}
	if area4, _ := fitArea([]float64{1, 1, 1, 1, 1}, 4, 2, 6, 1, 0); area4 != 1.25 {
		t.Errorf("4x2 in a 6x1 view covers %v, want 1.25", area4)
	}
	cols, rows, ok = chooseShape([]float64{1, 1, 1, 1, 1}, 6, 1, 0)
	if !ok || cols != 5 || rows != 1 {
		t.Errorf("five squares in a 6x1 view: %dx%d ok=%v, want 5x1", cols, rows, ok)
	}
	// The same view, but only as tall as the row it has to hold, so a single row
	// is no longer the widest each screen can be and the two-row shapes decide it
	// between themselves — on wasted cells.
	cols, rows, ok = chooseShape([]float64{1, 1, 1, 1, 1}, 2.4, 1, 0)
	if !ok || cols != 3 || rows != 2 {
		t.Errorf("five squares in a 2.4x1 view: %dx%d ok=%v, want 3x2", cols, rows, ok)
	}
	// Nothing fits: a gap wider than the view.
	if _, _, ok := chooseShape([]float64{1, 1}, 1, 1, 2); ok {
		t.Error("a gap twice the width of the view still produced a grid")
	}
}

// viewBounds is the view's rectangle in panorama pixels, as real numbers: the
// pixel CENTRES that may be painted are the ones inside it.
func viewBounds(p Pano, v View) (x0, x1, y0, y1 float64) {
	w, h := float64(p.W), float64(p.H)
	x0 = (0.5 - v.HDeg/(2*p.Window.HSpanDeg)) * w
	x1 = (0.5 + v.HDeg/(2*p.Window.HSpanDeg)) * w
	y0 = (0.5 - v.VDeg/(2*p.Window.VSpanDeg)) * h
	y1 = (0.5 + v.VDeg/(2*p.Window.VSpanDeg)) * h
	return x0, x1, y0, y1
}

// TestGalleryStaysInsideTheViewAtTheGlassesResolution: the geometry, at the
// application's real numbers, without allocating a two-megapixel buffer to say
// it. Every blit inside the view, none of them touching.
func TestGalleryStaysInsideTheViewAtTheGlassesResolution(t *testing.T) {
	pano := beastPano()
	for n := 1; n <= 12; n++ {
		_, _, g := beastGallery(t, n, pano)
		blits := g.Frame(nil)
		if len(blits) != n {
			t.Fatalf("%d screens produced %d blits", n, len(blits))
		}
		x0, x1, y0, y1 := viewBounds(pano, beastView())
		for _, b := range blits {
			if float64(b.Dst.X)+0.5 < x0-1e-9 || float64(b.Dst.X+b.Dst.W)-0.5 > x1+1e-9 {
				t.Errorf("%d screens: screen %d spans columns %d..%d, outside the view's %.1f..%.1f",
					n, b.Screen, b.Dst.X, b.Dst.X+b.Dst.W, x0, x1)
			}
			if float64(b.Dst.Y)+0.5 < y0-1e-9 || float64(b.Dst.Y+b.Dst.H)-0.5 > y1+1e-9 {
				t.Errorf("%d screens: screen %d spans rows %d..%d, outside the view's %.1f..%.1f",
					n, b.Screen, b.Dst.Y, b.Dst.Y+b.Dst.H, y0, y1)
			}
			// Big enough to be worth showing: the whole point is seeing them all
			// at once, and a hundred columns of a 2814-wide buffer is a screen a
			// person can recognise.
			if n <= 9 && b.Dst.W < 100 {
				t.Errorf("%d screens: screen %d is only %d columns wide", n, b.Screen, b.Dst.W)
			}
		}
		// Nothing overlaps, checked as rectangles rather than as pixels so that
		// this runs at the real resolution.
		for i := 0; i < len(blits); i++ {
			for j := i + 1; j < len(blits); j++ {
				a, b := blits[i].Dst, blits[j].Dst
				if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
					t.Errorf("%d screens: %d and %d overlap: %+v %+v", n, i, j, a, b)
				}
			}
		}
	}
}

// TestGalleryCellsTileTheView: the layout as geometry — cells in reading order,
// evenly spaced, none outside the view, none overlapping, every screen whole and
// still its own shape.
func TestGalleryCellsTileTheView(t *testing.T) {
	// Screens of different shapes, so that a bug that only shows when the cells
	// are not all filled the same way has somewhere to show.
	r := mustPlace(t, []Screen{
		{ID: "wide", W: 3440, H: 1440},
		{ID: "hd", W: 1920, H: 1080},
		{ID: "portrait", W: 1080, H: 1920},
		{ID: "square", W: 1024, H: 1024},
		{ID: "old", W: 1280, H: 1024},
	}, Layout{DensityDeg: 20, GapDeg: 3, FullWidthDeg: 60, Arrangement: Spread})
	v := beastView()
	g := mustGallery(t, mustComposite(t, r, beastPano()), v)

	halfW := rad(v.HDeg) / 2
	halfH := math.Tan(rad(v.VDeg) / 2)
	for i := 0; i < g.Len(); i++ {
		c := g.At(i)
		if c.Row != i/g.Cols() || c.Col != i%g.Cols() {
			t.Errorf("cell %d is at row %d col %d, want %d,%d in a %d-column grid",
				i, c.Row, c.Col, i/g.Cols(), i%g.Cols(), g.Cols())
		}
		if c.Centre-c.HalfSpan < -halfW-1e-12 || c.Centre+c.HalfSpan > halfW+1e-12 {
			t.Errorf("cell %d spans %.4f°..%.4f°, outside a %g° view",
				i, deg(c.Centre-c.HalfSpan), deg(c.Centre+c.HalfSpan), v.HDeg)
		}
		if c.Height-c.HalfHeight < -halfH-1e-12 || c.Height+c.HalfHeight > halfH+1e-12 {
			t.Errorf("cell %d spans heights %.4f..%.4f, outside ±%.4f",
				i, c.Height-c.HalfHeight, c.Height+c.HalfHeight, halfH)
		}
		// The screen keeps its own shape: an arc of α radians and a height of
		// α/aspect is what makes the pixels square, on the ribbon and here.
		if want := r.At(i).Aspect(); !closeTo(c.HalfSpan/c.HalfHeight, want) {
			t.Errorf("cell %d is %.4f wide for its height, want the screen's %.4f",
				i, c.HalfSpan/c.HalfHeight, want)
		}
		// And in reading order: to the right of its left neighbour, below the row
		// above it.
		if c.Col > 0 {
			l := g.At(i - 1)
			if c.Centre-c.HalfSpan < l.Centre+l.HalfSpan-1e-12 {
				t.Errorf("cell %d overlaps the cell to its left", i)
			}
		}
		if c.Row > 0 {
			a := g.At(i - g.Cols())
			if c.Height+c.HalfHeight > a.Height-a.HalfHeight+1e-12 {
				t.Errorf("cell %d overlaps the cell above it", i)
			}
		}
	}
	// The block is centred: the first column and the last are the same distance
	// from the edges of the view, and so are the top and bottom rows. A layout
	// that filled from one corner would pass every test above and look wrong.
	first, last := g.At(0), g.At(g.Cols()-1)
	if !closeTo(first.Centre-(-halfW), halfW-last.Centre) {
		t.Errorf("the grid is not centred horizontally: %.4f° to the left, %.4f° to the right",
			deg(first.Centre+halfW), deg(halfW-last.Centre))
	}
	bottom := g.At((g.Rows() - 1) * g.Cols())
	if !closeTo(halfH-first.Height, bottom.Height+halfH) {
		t.Errorf("the grid is not centred vertically: %.4f above, %.4f below",
			halfH-first.Height, bottom.Height+halfH)
	}
}

// TestGalleryPixels renders the gallery and asserts on the RESULT, the way
// composite_test does: each screen is filled with its own identity, so every
// panorama pixel says which screen, column and row it came from and the
// assertions are about the picture rather than about the arithmetic.
//
// This is what catches a transposed grid, a mirrored row, a cell drawn upside
// down, or a screen cropped instead of scaled — none of which a test of the
// numbers alone would notice, because the numbers would be self-consistent.
func TestGalleryPixels(t *testing.T) {
	const n = 6
	screens := alike(n, 320, 180)
	r := mustPlace(t, screens, beastLayout())
	pano := window(76, 54, 704, 484)
	c := mustComposite(t, r, pano)
	v := beastView()
	g := mustGallery(t, c, v)
	if g.Cols() != 3 || g.Rows() != 2 {
		t.Fatalf("the grid is %dx%d, want 3x2", g.Cols(), g.Rows())
	}

	src := make([][]uint32, n)
	for i, s := range screens {
		src[i] = make([]uint32, s.W*s.H)
		for y := 0; y < s.H; y++ {
			for x := 0; x < s.W; x++ {
				src[i][y*s.W+x] = uint32(i+1)<<24 | uint32(x)<<12 | uint32(y)
			}
		}
	}
	// The application's blitter, written here rather than in the package: the
	// gallery has to be drawable by the code that already draws the ribbon, with
	// nothing added.
	dst := make([]uint32, pano.W*pano.H)
	hits := make([]int, pano.W*pano.H)
	for _, b := range g.Frame(nil) {
		for j := 0; j < b.Dst.H; j++ {
			row := int(b.SrcY[j])
			out := dst[(b.Dst.Y+j)*pano.W+b.Dst.X:][:b.Dst.W]
			in := src[b.Screen][row*screens[b.Screen].W:][:screens[b.Screen].W]
			for i := range out {
				out[i] = in[b.Column(i)]
				hits[(b.Dst.Y+j)*pano.W+b.Dst.X+i]++
			}
		}
	}

	// 1. Nothing overlaps, and nothing is outside the VIEW — not merely inside
	//    the buffer, which is half as wide again.
	x0, x1, y0, y1 := viewBounds(pano, v)
	seen := map[int]int{}
	for y := 0; y < pano.H; y++ {
		for x := 0; x < pano.W; x++ {
			if hits[y*pano.W+x] > 1 {
				t.Fatalf("pixel (%d,%d) written %d times", x, y, hits[y*pano.W+x])
			}
			val := dst[y*pano.W+x]
			if val == 0 {
				continue
			}
			seen[int(val>>24)-1]++
			if fx, fy := float64(x)+0.5, float64(y)+0.5; fx < x0 || fx > x1 || fy < y0 || fy > y1 {
				t.Fatalf("pixel (%d,%d) is painted outside the view (%.1f..%.1f, %.1f..%.1f)",
					x, y, x0, x1, y0, y1)
			}
		}
	}
	// 2. Every screen is there, and they are all about the same size — this is a
	//    gallery, not a hierarchy.
	if len(seen) != n {
		t.Fatalf("%d of %d screens were painted at all", len(seen), n)
	}
	lo, hi := 1<<30, 0
	for _, px := range seen {
		lo, hi = min(lo, px), max(hi, px)
	}
	// Within a few percent: the rows are at different latitudes, so the tangent
	// mapping gives the top and bottom rows slightly different pixel heights for
	// the same height on the cylinder. Anything more than that is a cell drawn at
	// the wrong size.
	if hi > lo*105/100 {
		t.Errorf("the screens are painted at very different sizes: %d..%d pixels", lo, hi)
	}
	// 3. Reading order, in the picture: screen i+1 is to the right of screen i
	//    within a row, and each row is below the one before. A grid filled column
	//    by column passes every numeric test and fails this one.
	cx := make([]float64, n)
	cy := make([]float64, n)
	for y := 0; y < pano.H; y++ {
		for x := 0; x < pano.W; x++ {
			if val := dst[y*pano.W+x]; val != 0 {
				i := int(val>>24) - 1
				cx[i] += float64(x)
				cy[i] += float64(y)
			}
		}
	}
	for i := range cx {
		cx[i] /= float64(seen[i])
		cy[i] /= float64(seen[i])
	}
	for i := 1; i < n; i++ {
		if i%g.Cols() == 0 {
			if cy[i] <= cy[i-1] {
				t.Errorf("screen %d starts a new row but is at height %.1f, not below %.1f",
					i, cy[i], cy[i-1])
			}
			continue
		}
		if cx[i] <= cx[i-1] {
			t.Errorf("screen %d is at column %.1f, not to the right of screen %d at %.1f",
				i, cx[i], i-1, cx[i-1])
		}
		if math.Abs(cy[i]-cy[i-1]) > 1 {
			t.Errorf("screens %d and %d share a row but sit at heights %.1f and %.1f",
				i-1, i, cy[i-1], cy[i])
		}
	}
	// 4. Each cell shows the WHOLE screen, scaled down: its middle row reads from
	//    the first source column to the last, and its middle column from the
	//    first source row to the last. A cell that cropped instead of scaling
	//    would show a contiguous run that starts and ends in the middle.
	for i := 0; i < n; i++ {
		var cols, rows []int
		midY := int(cy[i])
		for x := 0; x < pano.W; x++ {
			if val := dst[midY*pano.W+x]; int(val>>24)-1 == i {
				cols = append(cols, int(val>>12)&0xfff)
			}
		}
		midX := int(cx[i])
		for y := 0; y < pano.H; y++ {
			if val := dst[y*pano.W+midX]; int(val>>24)-1 == i {
				rows = append(rows, int(val)&0xfff)
			}
		}
		if len(cols) == 0 || len(rows) == 0 {
			t.Fatalf("screen %d has no middle row or column", i)
		}
		stepX := screens[i].W/len(cols) + 1
		if cols[0] > stepX || cols[len(cols)-1] < screens[i].W-1-stepX {
			t.Errorf("screen %d shows source columns %d..%d of %d: it is cropped, not scaled",
				i, cols[0], cols[len(cols)-1], screens[i].W-1)
		}
		stepY := screens[i].H/len(rows) + 1
		if rows[0] > stepY || rows[len(rows)-1] < screens[i].H-1-stepY {
			t.Errorf("screen %d shows source rows %d..%d of %d: it is cropped, not scaled",
				i, rows[0], rows[len(rows)-1], screens[i].H-1)
		}
		// Monotone: not mirrored, not upside down.
		for k := 1; k < len(cols); k++ {
			if cols[k] < cols[k-1] {
				t.Fatalf("screen %d goes from column %d back to %d — it is mirrored", i, cols[k-1], cols[k])
			}
		}
		for k := 1; k < len(rows); k++ {
			if rows[k] < rows[k-1] {
				t.Fatalf("screen %d goes from row %d back to %d — it is upside down", i, rows[k-1], rows[k])
			}
		}
	}
	// 5. The gallery does not turn with the ribbon. It is head-locked, so a
	//    second frame at any yaw is the same picture, pixel for pixel — and the
	//    ribbon's own frame at the same yaw is not, which is what makes the claim
	//    worth stating.
	again := make([]uint32, pano.W*pano.H)
	for _, b := range g.Frame(nil) {
		for j := 0; j < b.Dst.H; j++ {
			row := int(b.SrcY[j])
			out := again[(b.Dst.Y+j)*pano.W+b.Dst.X:][:b.Dst.W]
			in := src[b.Screen][row*screens[b.Screen].W:][:screens[b.Screen].W]
			for i := range out {
				out[i] = in[b.Column(i)]
			}
		}
	}
	for i := range dst {
		if dst[i] != again[i] {
			t.Fatalf("the gallery is not stable: pixel (%d,%d) changed", i%pano.W, i/pano.W)
		}
	}
}

func TestGalleryFrameAllocatesNothing(t *testing.T) {
	_, _, g := beastGallery(t, 6, beastPano())
	blits := g.Frame(nil)
	if n := testing.AllocsPerRun(200, func() {
		blits = g.Frame(blits[:0])
	}); n != 0 {
		t.Errorf("Frame allocated %v times per call", n)
	}
}

func TestSelect(t *testing.T) {
	_, _, g := beastGallery(t, 6, beastPano())
	if g.Selected() != 0 {
		t.Errorf("a fresh gallery has screen %d selected, want 0", g.Selected())
	}
	for _, i := range []int{-1, 6, 99} {
		if err := g.Select(i); !errors.Is(err, ErrIndex) {
			t.Errorf("Select(%d) err = %v, want %v", i, err, ErrIndex)
		}
	}
	if g.Selected() != 0 {
		t.Error("a refused Select moved the selection anyway")
	}
	if err := g.Select(4); err != nil {
		t.Fatal(err)
	}
	if g.Selected() != 4 {
		t.Errorf("Selected = %d, want 4", g.Selected())
	}
	if err := g.Move(Direction(7)); !errors.Is(err, ErrDirection) {
		t.Errorf("Move(Direction(7)) err = %v, want %v", err, ErrDirection)
	}
	if g.Selected() != 4 {
		t.Error("a refused Move moved the selection anyway")
	}
}

// mustMove walks the selection, because a direction from a constant cannot fail
// and writing the check out at every step would bury the sequence being tested.
func mustMove(t *testing.T, g *Gallery, ds ...Direction) {
	t.Helper()
	for _, d := range ds {
		if err := g.Move(d); err != nil {
			t.Fatalf("Move(%s): %v", d, err)
		}
	}
}

// TestALapOfTheGridReturnsToTheStart: the sequence property. Holding right
// visits every screen exactly once and comes back, and left undoes it.
func TestALapOfTheGridReturnsToTheStart(t *testing.T) {
	for _, n := range []int{1, 2, 5, 6, 7, 12} {
		_, _, g := beastGallery(t, n, beastPano())
		for start := 0; start < n; start++ {
			if err := g.Select(start); err != nil {
				t.Fatal(err)
			}
			visited := map[int]bool{}
			for i := 0; i < n; i++ {
				visited[g.Selected()] = true
				mustMove(t, g, Right)
			}
			if len(visited) != n {
				t.Errorf("%d screens from %d: a lap of Right visited %d of them", n, start, len(visited))
			}
			if g.Selected() != start {
				t.Errorf("%d screens: a lap of Right from %d ended on %d", n, start, g.Selected())
			}
			for i := 0; i < n; i++ {
				mustMove(t, g, Left)
			}
			if g.Selected() != start {
				t.Errorf("%d screens: a lap of Left from %d ended on %d", n, start, g.Selected())
			}
			// Right then Left is a round trip from anywhere, including across the
			// end of a row and across the seam.
			mustMove(t, g, Right, Left)
			if g.Selected() != start {
				t.Errorf("%d screens: Right then Left from %d landed on %d", n, start, g.Selected())
			}
		}
	}
}

// TestUpAndDownClampAndAReggedRowBehaves: the vertical axis is not circular, and
// the last row may be short.
func TestUpAndDownClampAndARaggedRowBehaves(t *testing.T) {
	// Seven screens come out 3x3: 0 1 2 / 3 4 5 / 6.
	_, _, g := beastGallery(t, 7, beastPano())
	if g.Cols() != 3 || g.Rows() != 3 {
		t.Fatalf("seven screens came out %dx%d, want 3x3", g.Cols(), g.Rows())
	}
	for _, tc := range []struct {
		from int
		d    Direction
		want int
		why  string
	}{
		{0, Up, 0, "up from the top row has nowhere to go"},
		{2, Up, 2, "up from the top row has nowhere to go"},
		{6, Down, 6, "down from the last row has nowhere to go"},
		{3, Up, 0, "up is one row"},
		{0, Down, 3, "down is one row"},
		{3, Down, 6, "down into the ragged row"},
		// The cases that matter: below screens 4 and 5 there is nothing, and the
		// nearest thing below is the last screen. Refusing to move would strand
		// the viewer; jumping to another column would be arbitrary.
		{4, Down, 6, "down from a column the last row does not reach lands on the last screen"},
		{5, Down, 6, "down from a column the last row does not reach lands on the last screen"},
		{1, Down, 4, "down is one row"},
		{6, Up, 3, "up from the ragged row keeps its column"},
	} {
		if err := g.Select(tc.from); err != nil {
			t.Fatal(err)
		}
		mustMove(t, g, tc.d)
		if g.Selected() != tc.want {
			t.Errorf("%s from %d went to %d, want %d (%s)", tc.d, tc.from, g.Selected(), tc.want, tc.why)
		}
	}
	// Whatever the sequence, the selection is always a screen that exists. This
	// is the invariant the arrows are allowed to be surprising within.
	dirs := []Direction{Left, Right, Up, Down}
	for _, n := range []int{1, 2, 3, 5, 7, 11, 12} {
		_, _, g := beastGallery(t, n, beastPano())
		seq := 0
		for step := 0; step < 500; step++ {
			seq = (seq*7 + 3) % len(dirs)
			mustMove(t, g, dirs[seq])
			if g.Selected() < 0 || g.Selected() >= n {
				t.Fatalf("%d screens: step %d selected %d", n, step, g.Selected())
			}
		}
	}
}

// TestTheGridIsTheRibbonInReadingOrder: the composed claim. Stepping right
// through the gallery visits the screens in exactly the cyclic order that
// pressing Next walks the ribbon. Each half is self-consistent; a grid filled
// column-major, or reversed, agrees with itself and disagrees here.
func TestTheGridIsTheRibbonInReadingOrder(t *testing.T) {
	r, _, g := beastGallery(t, 7, beastPano())
	n := NewNav(r)
	if err := g.Select(0); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2*r.Len(); i++ {
		if g.Selected() != n.Focus() {
			t.Fatalf("step %d: the gallery is on screen %d and the ribbon on %d",
				i, g.Selected(), n.Focus())
		}
		// And the picture agrees with the order: within a row the next screen is
		// to the right, and at the end of a row it is the leftmost of the next.
		a, b := g.At(g.Selected()), g.At((g.Selected()+1)%r.Len())
		if a.Row == b.Row && b.Centre <= a.Centre {
			t.Errorf("step %d: the next screen shares a row but is not to the right", i)
		}
		if a.Row != b.Row && b.Col != 0 {
			t.Errorf("step %d: the next row does not start at its first column", i)
		}
		mustMove(t, g, Right)
		n.Next()
	}
}

// TestOpeningAndClosingLeavesTheRibbonExactlyAsItWas — exactly, not nearly, so
// these are float comparisons with no tolerance at all.
func TestOpeningAndClosingLeavesTheRibbonExactlyAsItWas(t *testing.T) {
	r, _, g := beastGallery(t, 6, beastPano())
	n := NewNav(r)
	for _, tc := range []struct {
		name  string
		setup func()
	}{
		{"at rest", func() { n.SetYaw(r.At(2).Centre) }},
		{"mid-turn", func() {
			n.SetYaw(r.At(0).Centre)
			n.Next()
			n.Advance(0.02) // a fifth of the way, and still moving
		}},
		{"between two screens, as a tracker would leave it", func() {
			n.SetYaw(r.At(3).Centre + rad(4.5))
		}},
		{"on a promoted screen", func() {
			n.SetYaw(r.At(4).Centre)
			n.ToggleFullscreen()
		}},
	} {
		if err := n.SetMode(ModeRibbon); err != nil {
			t.Fatal(err)
		}
		tc.setup()
		yaw, target, focus, mode, moving := n.yaw, n.target, n.Focus(), n.Mode(), n.Moving()

		if err := n.ToggleGallery(g); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if n.Mode() != ModeGallery {
			t.Errorf("%s: opening the gallery left the mode at %v", tc.name, n.Mode())
		}
		// The gallery starts on the screen the viewer is FACING, which between two
		// screens is not the one they last chose.
		if want := r.Nearest(wrap(yaw)); g.Selected() != want {
			t.Errorf("%s: opened on screen %d, want the nearest one, %d", tc.name, g.Selected(), want)
		}
		// Wander, and let the application do what applications do: call Advance
		// every frame whether or not there is anything to advance.
		mustMove(t, g, Right, Right, Down, Left, Up, Up, Down, Right)
		for i := 0; i < 120; i++ {
			n.Advance(1.0 / 60)
		}
		if n.yaw != yaw || n.target != target {
			t.Errorf("%s: the ribbon turned behind the gallery, from %.17g to %.17g",
				tc.name, yaw, n.yaw)
		}
		// A key that is not a way out must not be one.
		n.ToggleFullscreen()
		if n.Mode() != ModeGallery {
			t.Errorf("%s: fullscreen was toggled from inside the gallery", tc.name)
		}

		if err := n.ToggleGallery(g); err != nil {
			t.Fatalf("%s: closing: %v", tc.name, err)
		}
		if n.yaw != yaw {
			t.Errorf("%s: yaw came back as %.17g, want %.17g", tc.name, n.yaw, yaw)
		}
		if n.target != target {
			t.Errorf("%s: target came back as %.17g, want %.17g", tc.name, n.target, target)
		}
		if n.Focus() != focus {
			t.Errorf("%s: focus came back as %d, want %d", tc.name, n.Focus(), focus)
		}
		if n.Mode() != mode {
			t.Errorf("%s: mode came back as %v, want %v", tc.name, n.Mode(), mode)
		}
		if n.Moving() != moving {
			t.Errorf("%s: Moving came back as %v, want %v", tc.name, n.Moving(), moving)
		}
		// And the turn the viewer interrupted still finishes where it was going.
		if moving {
			settleNav(t, n, 1.0/60)
			if !closeTo(n.Yaw(), wrap(target)) {
				t.Errorf("%s: the interrupted turn ended at %.6f°, want %.6f°",
					tc.name, deg(n.Yaw()), deg(wrap(target)))
			}
		}
	}
}

// TestChoosingWhatYouAreLookingAtCostsNoMotion: the composed identity. Open the
// gallery, choose nothing, press Enter — and the ribbon must not move at all,
// from any screen. Every step of this passes with the selection off by one, or
// with Nearest read against the wrong sign; the composition does not.
func TestChoosingWhatYouAreLookingAtCostsNoMotion(t *testing.T) {
	r, _, g := beastGallery(t, 6, beastPano())
	n := NewNav(r)
	for i := 0; i < r.Len(); i++ {
		n.SetYaw(r.At(i).Centre)
		before := n.yaw
		if err := n.ToggleGallery(g); err != nil {
			t.Fatal(err)
		}
		if g.Selected() != i {
			t.Fatalf("facing screen %d, the gallery opened on %d", i, g.Selected())
		}
		if err := n.Choose(); err != nil {
			t.Fatal(err)
		}
		if n.Focus() != i {
			t.Errorf("choosing the screen in front focused %d, want %d", n.Focus(), i)
		}
		if n.Mode() != ModeRibbon {
			t.Errorf("choosing left the mode at %v", n.Mode())
		}
		if n.yaw != before || n.Moving() {
			t.Errorf("choosing the screen already in front moved the ribbon by %.9f°",
				deg(n.target-before))
		}
	}
}

// TestChooseLandsOnTheScreenThatWasHIGHLIGHTED is this package's
// TestYawMustBeAppliedLast: a claim that crosses the gallery's geometry, the
// selection model, the navigation and the ribbon's geometry, stated in PIXELS at
// both ends.
//
// Read the identity of the selected screen out of the gallery's picture; press
// Enter; let the ribbon settle; read the identity of the screen in the middle of
// the view out of the ribbon's picture. They must be the same screen. Transpose
// the grid, get Nearest's sign wrong, or aim at the cell index instead of the
// screen index, and every step still passes on its own.
func TestChooseLandsOnTheScreenThatWasHighlighted(t *testing.T) {
	const n = 6
	screens := alike(n, 320, 180)
	r := mustPlace(t, screens, beastLayout())
	pano := window(76, 54, 704, 484)
	c := mustComposite(t, r, pano)
	g := mustGallery(t, c, beastView())
	nav := NewNav(r)

	src := make([][]uint32, n)
	for i, s := range screens {
		src[i] = make([]uint32, s.W*s.H)
		for y := 0; y < s.H; y++ {
			for x := 0; x < s.W; x++ {
				src[i][y*s.W+x] = uint32(i+1)<<24 | uint32(x)<<12 | uint32(y)
			}
		}
	}
	paint := func(blits []Blit) []uint32 {
		dst := make([]uint32, pano.W*pano.H)
		for _, b := range blits {
			for j := 0; j < b.Dst.H; j++ {
				out := dst[(b.Dst.Y+j)*pano.W+b.Dst.X:][:b.Dst.W]
				in := src[b.Screen][int(b.SrcY[j])*screens[b.Screen].W:][:screens[b.Screen].W]
				for i := range out {
					out[i] = in[b.Column(i)]
				}
			}
		}
		return dst
	}

	// Every route through the grid, so the walk is not one lucky path.
	for _, route := range [][]Direction{
		{},
		{Right},
		{Right, Right},
		{Down},
		{Down, Right},
		{Right, Down, Right},
		{Left, Left},
		{Up, Left},
		{Down, Down, Right, Right, Right},
	} {
		for start := 0; start < n; start++ {
			nav.SetYaw(r.At(start).Centre)
			if err := nav.ToggleGallery(g); err != nil {
				t.Fatal(err)
			}
			mustMove(t, g, route...)

			// Which screen is highlighted, read off the PICTURE rather than off the
			// index: the middle of the selected cell, in pixels.
			cell := g.At(g.Selected())
			gx := int((0.5 + cell.Centre/rad(pano.Window.HSpanDeg)) * float64(pano.W))
			gy := int((0.5 - math.Atan(cell.Height)/rad(pano.Window.VSpanDeg)) * float64(pano.H))
			shown := paint(g.Frame(nil))[gy*pano.W+gx]
			if shown == 0 {
				t.Fatalf("from %d via %v: the middle of the selected cell is not painted", start, route)
			}

			if err := nav.Choose(); err != nil {
				t.Fatal(err)
			}
			settleNav(t, nav, 1.0/60)

			// And which screen the viewer is now looking at, likewise: the middle
			// column of the view, on the horizon.
			ribbon := paint(c.Frame(nil, nav.Yaw()))
			ahead := ribbon[(pano.H/2)*pano.W+pano.W/2]
			if ahead == 0 {
				t.Fatalf("from %d via %v: nothing is straight ahead after choosing", start, route)
			}
			if shown>>24 != ahead>>24 {
				t.Errorf("from %d via %v: the gallery highlighted screen %d, the ribbon turned to %d",
					start, route, int(shown>>24)-1, int(ahead>>24)-1)
			}
			// The middle of a cell is the middle of a screen, and so is the middle
			// of the view: the same source pixel, near enough, at both ends. This is
			// what would catch a cell drawn from the wrong corner.
			if dx := int(shown>>12&0xfff) - int(ahead>>12&0xfff); dx < -8 || dx > 8 {
				t.Errorf("from %d via %v: the cell's middle is source column %d, the view's is %d",
					start, route, shown>>12&0xfff, ahead>>12&0xfff)
			}
		}
	}
}

// TestTheGalleryIsNotEnteredOrLeftByAccident: the states that must be hard to
// build.
func TestTheGalleryIsNotEnteredOrLeftByAccident(t *testing.T) {
	r, _, g := beastGallery(t, 6, beastPano())
	n := NewNav(r)

	// A mode with no gallery behind it is a state with no picture.
	if err := n.SetMode(ModeGallery); !errors.Is(err, ErrNoGallery) {
		t.Errorf("SetMode(ModeGallery) err = %v, want %v", err, ErrNoGallery)
	}
	if n.Mode() != ModeRibbon {
		t.Error("a refused SetMode changed the mode anyway")
	}
	// Nothing to choose from, either.
	if err := n.Choose(); !errors.Is(err, ErrNoGallery) {
		t.Errorf("Choose with the gallery shut: err = %v, want %v", err, ErrNoGallery)
	}
	// A gallery of somebody else's ribbon would select screens that are not
	// there.
	other, _, og := beastGallery(t, 3, beastPano())
	if other == r {
		t.Fatal("the two ribbons are the same one")
	}
	if err := n.ToggleGallery(og); !errors.Is(err, ErrNotOurs) {
		t.Errorf("a gallery of another ribbon: err = %v, want %v", err, ErrNotOurs)
	}
	if n.Mode() != ModeRibbon {
		t.Error("a refused ToggleGallery opened it anyway")
	}
	// And the ordinary way in and out still works from fullscreen, twice over.
	n.ToggleFullscreen()
	for i := 0; i < 2; i++ {
		if err := n.ToggleGallery(g); err != nil {
			t.Fatal(err)
		}
		if n.Mode() != ModeGallery {
			t.Fatalf("round %d: the gallery did not open", i)
		}
		if err := n.ToggleGallery(g); err != nil {
			t.Fatal(err)
		}
		if n.Mode() != ModeFullscreen {
			t.Errorf("round %d: closing did not return to the promoted screen, but to %v", i, n.Mode())
		}
	}
	// Choosing from the gallery goes back to the BAND even when it was opened
	// from a promoted screen: the viewer asked for a screen, not for a mode.
	if err := n.ToggleGallery(g); err != nil {
		t.Fatal(err)
	}
	mustMove(t, g, Right)
	if err := n.Choose(); err != nil {
		t.Fatal(err)
	}
	if n.Mode() != ModeRibbon {
		t.Errorf("choosing from a gallery opened over fullscreen left the mode at %v", n.Mode())
	}
}

// TestChooseTurnsTheShortWayRound: Enter uses the motion Nav already has, from
// where the ribbon really is.
func TestChooseTurnsTheShortWayRound(t *testing.T) {
	r, _, g := beastGallery(t, 6, beastPano())
	n := NewNav(r)
	for from := 0; from < r.Len(); from++ {
		for to := 0; to < r.Len(); to++ {
			n.SetYaw(r.At(from).Centre)
			if err := n.ToggleGallery(g); err != nil {
				t.Fatal(err)
			}
			if err := g.Select(to); err != nil {
				t.Fatal(err)
			}
			if err := n.Choose(); err != nil {
				t.Fatal(err)
			}
			if d := math.Abs(n.target - n.yaw); d > math.Pi+1e-9 {
				t.Errorf("%d -> %d turned %.1f°, more than half a turn", from, to, deg(d))
			}
			settleNav(t, n, 1.0/60)
			if n.Focus() != to || !closeTo(n.Yaw(), r.At(to).Centre) {
				t.Errorf("%d -> %d arrived on screen %d at %.6f°, want %d at %.6f°",
					from, to, n.Focus(), deg(n.Yaw()), to, deg(r.At(to).Centre))
			}
		}
	}
}
