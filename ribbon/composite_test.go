package ribbon

import (
	"errors"
	"math"
	"testing"

	"github.com/go-xrkit/xrkit/projection"
)

// sameColumn reports whether the source column a blit reads is the one the real
// position v calls for.
//
// It admits both sides of a boundary, and only within a hundred-thousandth of a
// column of one. That band is not slack, it is the contract: the stepper carries
// 2^-32 of a column and is nudged up by 2^-20 to settle exact ties, so a sample
// that lands within a millionth of a boundary may read either side of it and
// nothing else may. Measured over four million columns of sweep, one sample got
// that close — which is precisely why this is a stated tolerance rather than an
// exact comparison that passes on the machine it was written on and fails on one
// of the nine the CI runs.
//
// Anything a sign, a wrap or a scale could get wrong moves a column by far more
// than this.
func sameColumn(got int, v float64) bool {
	return got == int(math.Floor(v+1e-5)) || got == int(math.Floor(v-1e-5))
}

// ambiguous reports whether v is close enough to a boundary for sameColumn to
// have accepted two answers. Counted by the tests, so that a tolerance quietly
// growing to cover a real defect shows up as the tolerance being used.
func ambiguous(v float64) bool {
	return int(math.Floor(v+1e-5)) != int(math.Floor(v-1e-5))
}

func window(hDeg, vDeg float64, w, h int) Pano {
	return Pano{W: w, H: h, Window: projection.Projection{
		Kind: projection.Equirect, HSpanDeg: hDeg, VSpanDeg: vDeg}}
}

func mustComposite(t *testing.T, r *Ribbon, p Pano) *Compositor {
	t.Helper()
	c, err := NewCompositor(r, p)
	if err != nil {
		t.Fatalf("NewCompositor: %v", err)
	}
	return c
}

func TestPanoRejectsWhatItCannotComposite(t *testing.T) {
	r := mustPlace(t, desk, spread(3))
	for _, tc := range []struct {
		name string
		pano Pano
		want error
	}{
		{"no width", window(140, 70, 0, 256), ErrPanoSize},
		{"no height", window(140, 70, 512, 0), ErrPanoSize},
		{"negative", window(140, 70, -1, -1), ErrPanoSize},
		// A flat or fisheye window would not make a yaw a horizontal shift, and
		// the whole design rests on it doing exactly that.
		{"flat window", Pano{W: 512, H: 256, Window: projection.Screen}, ErrPanoKind},
		{"fisheye window", Pano{W: 512, H: 256, Window: projection.Fisheye180}, ErrPanoKind},
		{"no horizontal span", window(0, 70, 512, 256), ErrPanoSpan},
		{"more than a circle", window(361, 70, 512, 256), ErrPanoSpan},
		{"no vertical span", window(140, 0, 512, 256), ErrPanoSpan},
		{"more than a pole to pole", window(140, 181, 512, 256), ErrPanoSpan},
		// Two rows spanning 180° sample latitude ±45°, and the band only reaches
		// 14.7°. Not one row of the buffer falls on a screen.
		{"too few rows to reach the band", window(140, 180, 512, 2), ErrPanoTooShort},
	} {
		c, err := NewCompositor(r, tc.pano)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if c != nil {
			t.Errorf("%s: got a compositor as well as an error", tc.name)
		}
	}
	// A promoted screen has its own height, and it has to reach too. One degree
	// of fullscreen width on a 16:9 screen is a band 0.03° tall.
	thin := mustPlace(t, desk, Layout{DensityDeg: 30, GapDeg: 2, FullWidthDeg: 1, Arrangement: Spread})
	if _, err := NewCompositor(thin, window(140, 70, 512, 64)); !errors.Is(err, ErrPanoTooShort) {
		t.Errorf("a fullscreen size too thin to reach any row: err = %v, want %v", err, ErrPanoTooShort)
	}
	c := mustComposite(t, r, window(140, 70, 512, 256))
	if got := c.Pano(); got != window(140, 70, 512, 256) {
		t.Errorf("Pano() = %+v", got)
	}
}

// TestBlitsAgreeWithPerPixelGeometry is the A/B that matters: the blits are
// compared against the definition they are an optimisation of — for every pixel
// of the buffer, take its longitude and latitude, and work out by hand which
// screen it lands on and where.
//
// Nothing about the fast path is reused in the slow one. If a sign, a rounding
// rule or a wrap were wrong, this disagrees on thousands of pixels.
func TestBlitsAgreeWithPerPixelGeometry(t *testing.T) {
	r := mustPlace(t, desk, spread(3))
	type cell struct {
		screen, sy int
		sx         float64 // the REAL source position, not yet a column
	}
	for _, pano := range []Pano{
		window(150, 80, 512, 256),
		window(360, 80, 512, 256), // the seam is inside the buffer
		window(60, 30, 197, 101),  // awkward sizes, so nothing divides evenly
	} {
		c := mustComposite(t, r, pano)
		hw := rad(pano.Window.HSpanDeg) / 2
		vs := rad(pano.Window.VSpanDeg)
		var blits []Blit
		for _, yaw := range []float64{0, 0.3, 1.7, math.Pi, 3.5, 5.9, 6.2, -2.4} {
			want := make([]cell, pano.W*pano.H)
			for i := range want {
				want[i].screen = -1
			}
			unsure := 0
			for y := 0; y < pano.H; y++ {
				lat := vs * (0.5 - (float64(y)+0.5)/float64(pano.H))
				tv := 0.5 - math.Tan(lat)/(2*r.halfH)
				if tv < 0 || tv >= 1 {
					continue
				}
				for x := 0; x < pano.W; x++ {
					lon := 2*hw*((float64(x)+0.5)/float64(pano.W)) - hw
					for i := 0; i < r.Len(); i++ {
						p := r.At(i)
						d := wrapSigned(lon - wrapSigned(p.Centre-yaw))
						if d < -p.HalfSpan || d >= p.HalfSpan {
							continue
						}
						want[y*pano.W+x] = cell{i, int(tv * float64(p.H)),
							(d + p.HalfSpan) / p.Span() * float64(p.W)}
					}
				}
			}
			type read struct {
				screen, sx, sy int
			}
			got := make([]read, pano.W*pano.H)
			for i := range got {
				got[i].screen = -1
			}
			blits = c.Frame(blits[:0], yaw)
			for _, b := range blits {
				for j := 0; j < b.Dst.H; j++ {
					for i := 0; i < b.Dst.W; i++ {
						got[(b.Dst.Y+j)*pano.W+b.Dst.X+i] = read{b.Screen, b.Column(i), int(b.SrcY[j])}
					}
				}
			}
			bad, drawn := 0, 0
			for i := range want {
				w, g := want[i], got[i]
				if w.screen >= 0 {
					drawn++
					if ambiguous(w.sx) {
						unsure++
					}
				}
				ok := w.screen == g.screen && w.sy == g.sy
				if ok && w.screen >= 0 {
					ok = sameColumn(g.sx, w.sx)
				}
				if !ok {
					if bad < 6 {
						t.Errorf("%g°x%g° %dx%d yaw %.2f px (%d,%d): want screen %d col %.6f row %d, "+
							"got screen %d col %d row %d",
							pano.Window.HSpanDeg, pano.Window.VSpanDeg, pano.W, pano.H,
							deg(yaw), i%pano.W, i/pano.W, w.screen, w.sx, w.sy, g.screen, g.sx, g.sy)
					}
					bad++
				}
			}
			if bad > 0 {
				t.Fatalf("%d of %d pixels disagree with the per-pixel geometry", bad, len(want))
			}
			// If most samples were near a boundary the tolerance would be doing
			// the work instead of the geometry, and this would prove nothing.
			if unsure*20 > drawn {
				t.Fatalf("%d of %d drawn pixels sat on a column boundary; the tolerance is carrying this test",
					unsure, drawn)
			}
		}
	}
}

// TestBlitsNeverOverlapAndStayInBounds: the two things a blitter cannot survive
// being wrong about.
func TestBlitsNeverOverlapAndStayInBounds(t *testing.T) {
	r := mustPlace(t, desk, spread(0)) // no gap: neighbours meet exactly
	pano := window(360, 70, 361, 91)   // odd sizes, and the seam inside the buffer
	c := mustComposite(t, r, pano)
	hits := make([]int, pano.W*pano.H)
	var blits []Blit
	for step := 0; step < 360; step++ {
		yaw := float64(step) * fullCircle / 360
		for i := range hits {
			hits[i] = 0
		}
		blits = c.Frame(blits[:0], yaw)
		for _, b := range blits {
			if b.Dst.X < 0 || b.Dst.Y < 0 || b.Dst.X+b.Dst.W > pano.W || b.Dst.Y+b.Dst.H > pano.H {
				t.Fatalf("yaw %d°: blit %+v leaves a %dx%d buffer", step, b.Dst, pano.W, pano.H)
			}
			src := r.At(b.Screen)
			for i := 0; i < b.Dst.W; i++ {
				if col := b.Column(i); col < 0 || col >= src.W {
					t.Fatalf("yaw %d°: screen %d column %d of %d reads source column %d",
						step, b.Screen, i, b.Dst.W, col)
				}
			}
			for j := 0; j < b.Dst.H; j++ {
				if row := int(b.SrcY[j]); row < 0 || row >= src.H {
					t.Fatalf("yaw %d°: screen %d row %d reads source row %d", step, b.Screen, j, row)
				}
			}
			for j := 0; j < b.Dst.H; j++ {
				for i := 0; i < b.Dst.W; i++ {
					hits[(b.Dst.Y+j)*pano.W+b.Dst.X+i]++
				}
			}
		}
		for i, h := range hits {
			if h > 1 {
				t.Fatalf("yaw %d°: pixel (%d,%d) written %d times", step, i%pano.W, i/pano.W, h)
			}
		}
	}
}

// TestSeamScreenComesBackAsTwoContiguousPieces: with the whole circle in the
// buffer, a screen sitting on the edge appears against both, and the two halves
// have to join without losing or repeating a source column.
func TestSeamScreenComesBackAsTwoContiguousPieces(t *testing.T) {
	r := mustPlace(t, []Screen{{ID: "only", W: 640, H: 360}}, packed(4))
	pano := window(360, 60, 720, 180)
	c := mustComposite(t, r, pano)
	// Put the single screen exactly behind the viewer, straddling the buffer's
	// two edges.
	blits := c.Frame(nil, wrap(r.At(0).Centre+math.Pi))
	if len(blits) != 2 {
		t.Fatalf("a screen on the seam produced %d blits, want 2", len(blits))
	}
	left, right := blits[0], blits[1]
	if left.Dst.X != 0 {
		left, right = right, left
	}
	if left.Dst.X != 0 {
		t.Fatalf("neither piece is against the left edge: %+v %+v", blits[0].Dst, blits[1].Dst)
	}
	if right.Dst.X+right.Dst.W != pano.W {
		t.Errorf("the other piece does not reach the right edge: %+v", right.Dst)
	}
	if left.Screen != right.Screen {
		t.Error("the two pieces are not the same screen")
	}
	if left.Dst.Y != right.Dst.Y || left.Dst.H != right.Dst.H {
		t.Error("the two pieces cover different rows")
	}
	// Walk off the right edge and back on at the left. The source has to carry
	// on across the join exactly as it does anywhere else in the run: same
	// direction, same step. A seam handled as a special case shows up here as one
	// difference out of line with all the others.
	var cols []int
	for _, b := range []Blit{right, left} {
		for i := 0; i < b.Dst.W; i++ {
			cols = append(cols, b.Column(i))
		}
	}
	step := float64(r.At(0).W) / float64(len(cols))
	for i := 1; i < len(cols); i++ {
		if d := cols[i] - cols[i-1]; d < int(step) || d > int(step)+1 {
			where := "inside a piece"
			if i == right.Dst.W {
				where = "at the seam"
			}
			t.Fatalf("column %d %s stepped %d source columns, want %d or %d",
				i, where, d, int(step), int(step)+1)
		}
	}
	// And the run covers the screen end to end, losing no more than the step.
	if cols[0] > int(step)+1 {
		t.Errorf("the run starts at source column %d, not at the screen's left edge", cols[0])
	}
	if want := r.At(0).W - int(step) - 2; cols[len(cols)-1] < want {
		t.Errorf("the run ends at source column %d, short of the screen's right edge at %d",
			cols[len(cols)-1], r.At(0).W-1)
	}
}

// TestCompositePixels renders with the map and asserts on the RESULT. Numbers
// that agree with each other cannot tell you a sign is inverted; pixels can.
//
// Each screen is filled with its own identity: the pixel value encodes which
// screen it came from and which column and row of it. So every panorama pixel
// says where it came from, and the assertions are about the picture rather than
// about the arithmetic that produced it.
func TestCompositePixels(t *testing.T) {
	screens := []Screen{{ID: "a", W: 320, H: 180}, {ID: "b", W: 256, H: 256}, {ID: "c", W: 400, H: 250}}
	// Packed, so all three are inside a 180° window at once and the picture has
	// something to be wrong about at both edges.
	r := mustPlace(t, screens, packed(6))
	pano := window(180, 60, 720, 240)
	c := mustComposite(t, r, pano)

	const bg = 0
	src := make([][]uint32, len(screens))
	for i, s := range screens {
		src[i] = make([]uint32, s.W*s.H)
		for y := 0; y < s.H; y++ {
			for x := 0; x < s.W; x++ {
				// screen | column | row, each in its own field, so a wrong pixel
				// names the pixel it should have been.
				src[i][y*s.W+x] = uint32(i+1)<<24 | uint32(x)<<12 | uint32(y)
			}
		}
	}
	// The application's blitter, written here rather than in the package: the
	// package produces geometry, and this is an independent consumer of it.
	paint := func(dst []uint32, blits []Blit) {
		for _, b := range blits {
			for j := 0; j < b.Dst.H; j++ {
				row := int(b.SrcY[j])
				out := dst[(b.Dst.Y+j)*pano.W+b.Dst.X:][:b.Dst.W]
				in := src[b.Screen][row*screens[b.Screen].W:][:screens[b.Screen].W]
				for i := range out {
					out[i] = in[b.Column(i)]
				}
			}
		}
	}

	yaw := r.At(1).Centre
	dst := make([]uint32, pano.W*pano.H)
	paint(dst, c.Frame(nil, yaw))

	// 1. Every painted pixel names a screen that really is at that longitude,
	//    and a source pixel whose position agrees with where it was painted.
	hw := rad(pano.Window.HSpanDeg) / 2
	painted := map[int]int{}
	for y := 0; y < pano.H; y++ {
		for x := 0; x < pano.W; x++ {
			v := dst[y*pano.W+x]
			if v == bg {
				continue
			}
			i := int(v>>24) - 1
			sx, sy := int(v>>12)&0xfff, int(v)&0xfff
			painted[i]++
			p := r.At(i)
			lon := 2*hw*((float64(x)+0.5)/float64(pano.W)) - hw
			d := wrapSigned(lon - wrapSigned(p.Centre-yaw))
			if d < -p.HalfSpan || d >= p.HalfSpan {
				t.Fatalf("pixel (%d,%d) shows screen %d, which is not at longitude %.3f°",
					x, y, i, deg(lon))
			}
			if want := (d + p.HalfSpan) / p.Span() * float64(p.W); !sameColumn(sx, want) {
				t.Fatalf("pixel (%d,%d) shows column %d of screen %d, want %.4f", x, y, sx, i, want)
			}
			_ = sy
		}
	}
	// 2. The focused screen is centred, and its neighbours are to either side.
	centroid := func(i int) float64 {
		sum, n := 0.0, 0
		for y := 0; y < pano.H; y++ {
			for x := 0; x < pano.W; x++ {
				if int(dst[y*pano.W+x]>>24)-1 == i {
					sum += float64(x)
					n++
				}
			}
		}
		if n == 0 {
			t.Fatalf("screen %d was not painted at all", i)
		}
		return sum / float64(n)
	}
	if mid := centroid(1); math.Abs(mid-float64(pano.W)/2) > 1 {
		t.Errorf("the focused screen's centre of mass is at column %.1f, want %d", mid, pano.W/2)
	}
	if centroid(0) >= centroid(1) || centroid(1) >= centroid(2) {
		t.Errorf("the screens are not in ribbon order across the buffer: %.0f %.0f %.0f",
			centroid(0), centroid(1), centroid(2))
	}
	// 3. The focused screen is complete: it occupies one unbroken run of columns,
	//    and that run reads the screen end to end. A screen 256 pixels wide drawn
	//    into 120 columns of buffer cannot show every column, so the claim is
	//    that it reaches both edges and skips nothing in between.
	row := pano.H / 2
	var xs, srcs []int
	for x := 0; x < pano.W; x++ {
		if v := dst[row*pano.W+x]; int(v>>24)-1 == 1 {
			xs = append(xs, x)
			srcs = append(srcs, int(v>>12)&0xfff)
		}
	}
	if len(xs) == 0 {
		t.Fatal("the focused screen is not on the middle row at all")
	}
	if xs[len(xs)-1]-xs[0]+1 != len(xs) {
		t.Errorf("the focused screen has a hole in it: %d columns spread over %d",
			len(xs), xs[len(xs)-1]-xs[0]+1)
	}
	step := screens[1].W/len(srcs) + 1
	if srcs[0] > step || srcs[len(srcs)-1] < screens[1].W-1-step {
		t.Errorf("the focused screen shows source columns %d..%d of %d; it is clipped",
			srcs[0], srcs[len(srcs)-1], screens[1].W-1)
	}
	seenRows := map[int]bool{}
	for y := 0; y < pano.H; y++ {
		for x := 0; x < pano.W; x++ {
			if v := dst[y*pano.W+x]; int(v>>24)-1 == 1 {
				seenRows[int(v)&0xfff] = true
			}
		}
	}
	if len(seenRows) < screens[1].H/4 {
		t.Errorf("the focused screen showed only %d of its %d rows", len(seenRows), screens[1].H)
	}
	// 4. Columns run left to right, monotonically, on every painted row.
	for y := 0; y < pano.H; y++ {
		prev, prevScreen := -1, -1
		for x := 0; x < pano.W; x++ {
			v := dst[y*pano.W+x]
			if v == bg {
				continue
			}
			i, sx := int(v>>24)-1, int(v>>12)&0xfff
			if i == prevScreen && sx < prev {
				t.Fatalf("row %d: screen %d goes from column %d back to %d — it is mirrored",
					y, i, prev, sx)
			}
			prev, prevScreen = sx, i
		}
	}
	// 5. Rows run top to bottom.
	for x := 0; x < pano.W; x++ {
		prev, prevScreen := -1, -1
		for y := 0; y < pano.H; y++ {
			v := dst[y*pano.W+x]
			if v == bg {
				continue
			}
			i, sy := int(v>>24)-1, int(v)&0xfff
			if i == prevScreen && sy < prev {
				t.Fatalf("column %d: screen %d goes from row %d back to %d — it is upside down",
					x, i, prev, sy)
			}
			prev, prevScreen = sy, i
		}
	}
	// 6. A whole turn is no turn: the picture must be identical, pixel for pixel.
	again := make([]uint32, pano.W*pano.H)
	paint(again, c.Frame(nil, yaw+fullCircle))
	for i := range dst {
		if dst[i] != again[i] {
			t.Fatalf("after a full turn pixel (%d,%d) changed from %#x to %#x",
				i%pano.W, i/pano.W, dst[i], again[i])
		}
	}
	// 7. Turning right by exactly one panorama column slides the picture one
	//    column to the LEFT. This is the sign of the whole thing, stated in
	//    pixels: get it backwards and the ribbon scrolls the wrong way.
	shifted := make([]uint32, pano.W*pano.H)
	paint(shifted, c.Frame(nil, yaw+2*hw/float64(pano.W)))
	same, differ := 0, 0
	for y := 0; y < pano.H; y++ {
		for x := 0; x+1 < pano.W; x++ {
			if shifted[y*pano.W+x] == dst[y*pano.W+x+1] {
				same++
			} else {
				differ++
			}
		}
	}
	if differ*200 > same {
		t.Errorf("turning one column right did not slide the picture one column left: "+
			"%d pixels of %d disagree", differ, same+differ)
	}
}

// TestVisibleIsASupersetOfWhatIsDrawn: the two answers have to agree, and
// Visible is the geometric truth — a screen can be inside the arc and still be
// too narrow, right at the edge, to contain a pixel centre.
func TestVisibleIsASupersetOfWhatIsDrawn(t *testing.T) {
	r := mustPlace(t, desk, packed(4))
	pano := window(130, 60, 384, 128)
	c := mustComposite(t, r, pano)
	var blits []Blit
	var vis []int
	for step := 0; step < 720; step++ {
		yaw := float64(step) * fullCircle / 720
		blits = c.Frame(blits[:0], yaw)
		vis = r.Visible(yaw, pano.Window.HSpanDeg, vis[:0])
		seen := map[int]bool{}
		for _, i := range vis {
			seen[i] = true
		}
		for _, b := range blits {
			if !seen[b.Screen] {
				t.Fatalf("yaw %.2f°: screen %d was drawn but is not visible", deg(yaw), b.Screen)
			}
		}
	}
}

func TestFullscreenFillsTheView(t *testing.T) {
	r := mustPlace(t, desk, Layout{DensityDeg: 26, GapDeg: 3, FullWidthDeg: 120, Arrangement: Spread})
	pano := window(140, 70, 512, 256)
	c := mustComposite(t, r, pano)
	if _, err := c.Fullscreen(nil, -1); !errors.Is(err, ErrIndex) {
		t.Errorf("Fullscreen(-1) err = %v, want %v", err, ErrIndex)
	}
	if _, err := c.Fullscreen(nil, r.Len()); !errors.Is(err, ErrIndex) {
		t.Errorf("Fullscreen(past the end) err = %v, want %v", err, ErrIndex)
	}
	for i := 0; i < r.Len(); i++ {
		blits, err := c.Fullscreen(nil, i)
		if err != nil {
			t.Fatal(err)
		}
		if len(blits) != 1 {
			t.Fatalf("screen %d promoted to %d blits, want 1", i, len(blits))
		}
		b := blits[0]
		if b.Screen != i {
			t.Errorf("promoted screen %d came back as %d", i, b.Screen)
		}
		// 120° of screen in a 140° window: centred, and wider than the screen
		// ever is on the ribbon.
		mid := float64(b.Dst.X) + float64(b.Dst.W)/2
		if math.Abs(mid-float64(pano.W)/2) > 1 {
			t.Errorf("promoted screen %d is centred on column %.1f, want %d", i, mid, pano.W/2)
		}
		ribbon := c.Frame(nil, r.At(i).Centre)
		var onRibbon Blit
		for _, rb := range ribbon {
			if rb.Screen == i {
				onRibbon = rb
			}
		}
		if b.Dst.W <= onRibbon.Dst.W || b.Dst.H <= onRibbon.Dst.H {
			t.Errorf("promoted screen %d is %dx%d, no bigger than its %dx%d on the ribbon",
				i, b.Dst.W, b.Dst.H, onRibbon.Dst.W, onRibbon.Dst.H)
		}
		// It still reads the whole screen, end to end. A promoted screen is
		// smaller in the buffer than it is in its own pixels, so the run steps by
		// several source columns at a time and the ends are approached to within
		// one step, not landed on exactly.
		step := int(float64(r.At(i).W)/float64(b.Dst.W)) + 1
		if got := b.Column(0); got > step {
			t.Errorf("promoted screen %d starts at source column %d, more than one %d-column step in",
				i, got, step)
		}
		if got, want := b.Column(b.Dst.W-1), r.At(i).W-1; got < want-step {
			t.Errorf("promoted screen %d ends at source column %d, short of %d", i, got, want)
		}
	}
}

// TestFrameAllocatesNothing: it runs sixty times a second beside a 2.8 ms warp.
func TestFrameAllocatesNothing(t *testing.T) {
	r := mustPlace(t, desk, spread(3))
	c := mustComposite(t, r, window(140, 70, 1024, 512))
	blits := c.Frame(nil, 0)
	yaw := 0.0
	if n := testing.AllocsPerRun(200, func() {
		yaw += 0.01
		blits = c.Frame(blits[:0], yaw)
	}); n != 0 {
		t.Errorf("Frame allocated %v times per call", n)
	}
	if n := testing.AllocsPerRun(200, func() {
		blits, _ = c.Fullscreen(blits[:0], 1)
	}); n != 0 {
		t.Errorf("Fullscreen allocated %v times per call", n)
	}
}

// TestVerticalMappingIsATangent: the horizontal mapping is linear in longitude
// and the vertical one is not, because height on a cylinder is the tangent of
// latitude. Treating it as linear looks right in the middle of the band and
// squashes the top and bottom, which is exactly the kind of error a numeric test
// of the centre row would miss.
func TestVerticalMappingIsATangent(t *testing.T) {
	const panoH, srcH = 240, 1080
	vSpan, halfH := rad(60), rad(30)/2
	v := buildVTab(panoH, vSpan, 0, halfH, srcH)
	if len(v.rows) == 0 || v.y0 == 0 {
		t.Fatalf("the band should sit inside a 60° window, got y0=%d rows=%d", v.y0, len(v.rows))
	}
	worstTangent, worstLinear := 0.0, 0.0
	for j, row := range v.rows {
		y := v.y0 + j
		lat := vSpan * (0.5 - (float64(y)+0.5)/float64(panoH))
		tangent := float64(int((0.5 - math.Tan(lat)/(2*halfH)) * srcH))
		linear := float64(int((0.5 - lat/(2*math.Atan(halfH))) * srcH))
		worstTangent = math.Max(worstTangent, math.Abs(float64(row)-tangent))
		worstLinear = math.Max(worstLinear, math.Abs(float64(row)-linear))
	}
	if worstTangent != 0 {
		t.Errorf("the table is %v rows off the tangent mapping", worstTangent)
	}
	// A linear mapping agrees at the centre and drifts away from it; if it did
	// not, this test would not discriminate between the two.
	if worstLinear < 4 {
		t.Errorf("a linear mapping differs by only %v rows; this test proves nothing", worstLinear)
	}
	// The table is contiguous and monotone, which is what lets a blit be a
	// rectangle with one row index per row.
	for j := 1; j < len(v.rows); j++ {
		if v.rows[j] < v.rows[j-1] {
			t.Fatalf("row %d reads source row %d after %d", j, v.rows[j], v.rows[j-1])
		}
	}
	// A band taller than the window covers every row of it, top to bottom.
	full := buildVTab(panoH, rad(20), 0, rad(120)/2, srcH)
	if full.y0 != 0 || len(full.rows) != panoH {
		t.Errorf("a band taller than the window covered rows %d..%d of %d",
			full.y0, full.y0+len(full.rows), panoH)
	}
}

func TestFixedPointStepper(t *testing.T) {
	const one = int64(1) << fracBits
	// The ordinary case: start where you were told, step by what you were told,
	// and bias up by far less than half a column so nothing visible moves.
	x0, dx := stepper(3.75, 0.5, 6, 100)
	if dx != one/2 {
		t.Errorf("step 0.5 came out as %d, want %d", dx, one/2)
	}
	b := Blit{SrcX: x0, SrcXStep: dx}
	for i, want := range []int{3, 4, 4, 5, 5, 6} {
		if got := b.Column(i); got != want {
			t.Errorf("Column(%d) = %d, want %d", i, got, want)
		}
	}
	// A destination column landing exactly on a source boundary must read the
	// column to the RIGHT of it, not the one to the left. Rounding down alone
	// gets this wrong, systematically, whenever the step is a ratio of small
	// whole numbers — which is most of the time.
	x0, dx = stepper(0, 2880.0/197.0, 200, 4096)
	b = Blit{SrcX: x0, SrcXStep: dx}
	for i := 0; i < 200; i++ {
		if want := int(math.Floor(float64(i) * 2880.0 / 197.0)); b.Column(i) != want {
			t.Errorf("Column(%d) = %d, want %d (an exact boundary read the wrong side)",
				i, b.Column(i), want)
		}
	}
	// The bias must not be able to reach the next column on its own.
	if x0, _ := stepper(7, 1, 1, 100); x0>>fracBits != 7 || x0 <= 7*one {
		t.Errorf("stepper(7,...) = %d, want just above %d", x0, 7*one)
	}
	// The guards. Neither is reachable through the ribbon's own arithmetic —
	// which is exactly why they are tested by hand rather than left to it.
	if x0, _ := stepper(5, 0, 1, 5); x0>>fracBits != 4 {
		t.Errorf("a run ending one column past a 5-column source started at %d, want 4",
			x0>>fracBits)
	}
	if x0, _ := stepper(9, 1, 4, 6); x0>>fracBits != 2 {
		t.Errorf("a run overrunning a 6-column source by 7 started at %d, want 2", x0>>fracBits)
	}
	// A step that cannot be converted at all, and a run with nowhere left to
	// slide back to: both must come back reading the first column and standing
	// still, rather than reading past the end of somebody's framebuffer.
	for _, tc := range []struct{ first, step float64 }{{0, 1e30}, {0, 1e12}, {40, 1}} {
		x0, dx := stepper(tc.first, tc.step, 8, 4)
		for i := 0; i < 8; i++ {
			if col := (Blit{SrcX: x0, SrcXStep: dx}).Column(i); col < 0 || col >= 4 {
				t.Fatalf("stepper(%g, %g, 8, 4) column %d = %d, outside a 4-column source",
					tc.first, tc.step, i, col)
			}
		}
	}
	if got := ceilInt(2.0); got != 2 {
		t.Errorf("ceilInt(2.0) = %d", got)
	}
	if got := ceilInt(2.000001); got != 3 {
		t.Errorf("ceilInt(2.000001) = %d", got)
	}
	if got := ceilInt(-2.5); got != -2 {
		t.Errorf("ceilInt(-2.5) = %d", got)
	}
}
