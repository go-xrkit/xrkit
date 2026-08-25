package ribbon

import (
	"errors"
	"math"
	"testing"
)

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// desk is the arrangement most of these tests use: three screens of different
// shapes, so that a bug that only shows on unequal spans has somewhere to show.
var desk = []Screen{
	{ID: "left", W: 1920, H: 1080},
	{ID: "middle", W: 2560, H: 1440},
	{ID: "right", W: 1280, H: 1024},
}

func mustPlace(t *testing.T, screens []Screen, lay Layout) *Ribbon {
	t.Helper()
	r, err := Place(screens, lay)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	return r
}

func packed(gap float64) Layout {
	return Layout{DensityDeg: 30, GapDeg: gap, FullWidthDeg: 100, Arrangement: Packed}
}

func spread(gap float64) Layout {
	return Layout{DensityDeg: 30, GapDeg: gap, FullWidthDeg: 100, Arrangement: Spread}
}

func TestScreenAspect(t *testing.T) {
	if got := (Screen{W: 1920, H: 1080}).Aspect(); !closeTo(got, 16.0/9.0) {
		t.Errorf("Aspect = %v, want 16/9", got)
	}
	// A screen with no pixels has no shape. It must not divide by zero: Place
	// refuses it, but Aspect is public and reachable before Place is called.
	if got := (Screen{W: 1920}).Aspect(); got != 0 {
		t.Errorf("Aspect of a zero-height screen = %v, want 0", got)
	}
}

func TestArrangementString(t *testing.T) {
	for _, tc := range []struct {
		a    Arrangement
		want string
	}{{Packed, "packed"}, {Spread, "spread"}, {Arrangement(7), "Arrangement(7)"}} {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("Arrangement(%d).String() = %q, want %q", int(tc.a), got, tc.want)
		}
	}
}

func TestPlaceRejectsWhatItCannotArrange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		screens []Screen
		lay     Layout
		want    error
	}{
		{"no density", desk, Layout{GapDeg: 2, FullWidthDeg: 100}, ErrDensity},
		{"negative gap", desk, Layout{DensityDeg: 30, GapDeg: -1, FullWidthDeg: 100}, ErrGap},
		{"no fullscreen width", desk, Layout{DensityDeg: 30}, ErrFullWidth},
		{"fullscreen wider than the world", desk,
			Layout{DensityDeg: 30, FullWidthDeg: 361}, ErrFullWidth},
		{"unknown arrangement", desk,
			Layout{DensityDeg: 30, FullWidthDeg: 100, Arrangement: Arrangement(9)}, ErrArrangement},
		{"no screens", nil, packed(2), ErrNoScreens},
		{"zero-width screen", []Screen{{W: 0, H: 1080}}, packed(2), ErrScreenSize},
		{"zero-height screen", []Screen{{W: 1920, H: 0}}, packed(2), ErrScreenSize},
		// Twelve 16:9 screens at 30° of density is 640° of screen before the gaps
		// are counted. There is no arrangement of that; refusing is the only
		// honest answer, because the alternative is screens on top of each other.
		{"too crowded", make([]Screen, 12), packed(2), ErrCrowded},
	} {
		screens := tc.screens
		if tc.name == "too crowded" {
			screens = make([]Screen, 12)
			for i := range screens {
				screens[i] = Screen{W: 1920, H: 1080}
			}
		}
		r, err := Place(screens, tc.lay)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if r != nil {
			t.Errorf("%s: got a ribbon as well as an error", tc.name)
		}
	}
}

// TestScreensNeverOverlap is the invariant the whole arrangement exists to keep.
// It is checked on the circle, so two screens either side of the seam are tested
// against each other like any other pair.
func TestScreensNeverOverlap(t *testing.T) {
	dense := func(gap float64, a Arrangement) Layout {
		return Layout{DensityDeg: 14, GapDeg: gap, FullWidthDeg: 100, Arrangement: a}
	}
	for _, lay := range []Layout{dense(0, Packed), dense(4, Packed), dense(0, Spread), dense(4, Spread)} {
		for n := 1; n <= 8; n++ {
			screens := make([]Screen, n)
			for i := range screens {
				// Deliberately unequal: 4:3, 16:9 and 21:9 in turn, so the spans
				// differ and an arrangement that only works for equal widths fails.
				screens[i] = []Screen{{W: 1024, H: 768}, {W: 1920, H: 1080}, {W: 2560, H: 1080}}[i%3]
			}
			r := mustPlace(t, screens, lay)
			for i := 0; i < n; i++ {
				for j := i + 1; j < n; j++ {
					a, b := r.At(i), r.At(j)
					d := math.Abs(wrapSigned(a.Centre - b.Centre))
					if want := a.HalfSpan + b.HalfSpan + r.Gap(); d < want-1e-9 {
						t.Errorf("%s n=%d: screens %d and %d are %.3f° apart, want at least %.3f°",
							lay.Arrangement, n, i, j, deg(d), deg(want))
					}
				}
			}
		}
	}
}

// TestSpreadFillsTheCircle: under Spread the gaps take up the slack exactly, so
// the screens and gaps add up to one turn and no more.
func TestSpreadFillsTheCircle(t *testing.T) {
	r := mustPlace(t, desk, spread(2))
	total := r.Gap() * float64(r.Len())
	for i := 0; i < r.Len(); i++ {
		total += r.At(i).Span()
	}
	if !closeTo(total, fullCircle) {
		t.Errorf("spread total = %.6f rad, want %.6f", total, fullCircle)
	}
	if r.Gap() < rad(2) {
		t.Errorf("spread gap %.3f° is below the configured minimum of 2°", deg(r.Gap()))
	}
	// Packed leaves the slack alone: the gap is exactly what was asked for.
	if p := mustPlace(t, desk, packed(2)); !closeTo(p.Gap(), rad(2)) {
		t.Errorf("packed gap = %.3f°, want 2°", deg(p.Gap()))
	}
}

// TestArrangementIsCentredAndOrdered pins the two things the application relies
// on: a single screen is straight ahead, and appending a screen shifts the
// others without reordering them.
func TestArrangementIsCentredAndOrdered(t *testing.T) {
	for _, lay := range []Layout{packed(3), spread(3)} {
		one := mustPlace(t, []Screen{{W: 1920, H: 1080}}, lay)
		if got := wrapSigned(one.At(0).Centre); !closeTo(got, 0) {
			t.Errorf("%s: a lone screen sits at %.3f°, want straight ahead", lay.Arrangement, deg(got))
		}
		// Order round the circle must follow the order given, whatever the count.
		for n := 2; n <= 6; n++ {
			screens := make([]Screen, n)
			for i := range screens {
				screens[i] = desk[i%len(desk)]
			}
			r := mustPlace(t, screens, lay)
			for i := 0; i+1 < n; i++ {
				// Tie forwards: with two screens under Spread the neighbour is
				// exactly half a turn away, and both ways round are equal.
				step := shortest(r.At(i).Centre, r.At(i+1).Centre, +1)
				if step <= 0 {
					t.Errorf("%s n=%d: screen %d is not to the right of %d (step %.3f°)",
						lay.Arrangement, n, i+1, i, deg(step))
				}
			}
		}
	}
}

// TestSeamIsTenDegreesNotThreeHundredAndFifty is the shape of bug this package
// is most likely to have. It asserts against an independently computed
// expectation AND against the naive subtraction, so the test cannot pass by
// agreeing with a wrong convention.
func TestSeamIsTenDegreesNotThreeHundredAndFifty(t *testing.T) {
	near, far := rad(5), rad(355)
	if got := wrapSigned(far - near); !closeTo(got, -rad(10)) {
		t.Errorf("355° from 5° = %.3f°, want -10°", deg(got))
	}
	if got := wrapSigned(near - far); !closeTo(got, rad(10)) {
		t.Errorf("5° from 355° = %.3f°, want +10°", deg(got))
	}
	if naive := far - near; closeTo(math.Abs(naive), rad(10)) {
		t.Error("plain subtraction already gives 10°; this test no longer discriminates")
	}
}

func TestWrapStaysInsideItsInterval(t *testing.T) {
	for _, a := range []float64{0, 1, -1, math.Pi, -math.Pi, fullCircle, -fullCircle,
		7 * fullCircle, -7 * fullCircle, 1e-20, -1e-20, -1e-300, 1e12, -1e12} {
		w := wrap(a)
		if !(w >= 0 && w < fullCircle) {
			t.Errorf("wrap(%v) = %v, outside [0, 2π)", a, w)
		}
		s := wrapSigned(a)
		if !(s >= -math.Pi && s < math.Pi) {
			t.Errorf("wrapSigned(%v) = %v, outside [-π, π)", a, s)
		}
		// Wrapping must not move the angle by anything other than whole turns.
		if turns := (a - w) / fullCircle; !closeTo(turns, math.Round(turns)) {
			t.Errorf("wrap(%v) = %v moved it by %v turns", a, w, turns)
		}
	}
	// The guard that matters: a tiny negative angle plus 2π rounds UP to exactly
	// 2π, which is outside the interval wrap promises.
	if got := wrap(-1e-20); got != 0 {
		t.Errorf("wrap(-1e-20) = %v, want exactly 0 (it must not round up to 2π)", got)
	}
	if got := wrapSigned(math.Pi); !closeTo(got, -math.Pi) {
		t.Errorf("wrapSigned(π) = %v, want -π", got)
	}
}

// TestShortestBreaksTheHalfTurnTie: at exactly half a turn the two ways round
// are the same length, and which one is taken decides whether Next and Prev
// turn opposite ways.
func TestShortestBreaksTheHalfTurnTie(t *testing.T) {
	if got := shortest(0, math.Pi, +1); !closeTo(got, math.Pi) {
		t.Errorf("shortest forwards over half a turn = %v, want +π", got)
	}
	if got := shortest(0, math.Pi, -1); !closeTo(got, -math.Pi) {
		t.Errorf("shortest backwards over half a turn = %v, want -π", got)
	}
	// Away from the tie the direction hint is ignored: short is short.
	for _, dir := range []int{-1, 0, +1} {
		if got := shortest(rad(350), rad(10), dir); !closeTo(got, rad(20)) {
			t.Errorf("shortest(350°, 10°, %d) = %.3f°, want +20°", dir, deg(got))
		}
		if got := shortest(rad(10), rad(350), dir); !closeTo(got, -rad(20)) {
			t.Errorf("shortest(10°, 350°, %d) = %.3f°, want -20°", dir, deg(got))
		}
	}
}

func TestBearingAndNearest(t *testing.T) {
	r := mustPlace(t, desk, spread(3))
	for i := 0; i < r.Len(); i++ {
		c := r.At(i).Centre
		if got := r.Bearing(i, c); !closeTo(got, 0) {
			t.Errorf("bearing to screen %d from its own centre = %v, want 0", i, got)
		}
		if got := r.Nearest(c); got != i {
			t.Errorf("Nearest(centre of %d) = %d", i, got)
		}
		// And from just inside the seam, measured the short way.
		if got := r.Bearing(i, c-rad(1)); !closeTo(got, rad(1)) {
			t.Errorf("bearing to screen %d from 1° left of it = %.4f°, want 1°", i, deg(got))
		}
	}
	// A viewer exactly between two screens must get a stable answer rather than
	// one that flickers as the last bit of the yaw changes. An exact tie is not
	// reachable through Place — the arrangement arithmetic lands an ulp off
	// symmetry — so it is built directly.
	tie := &Ribbon{screens: []Placed{
		{Centre: 0.25, HalfSpan: 0.1},
		{Centre: 0.75, HalfSpan: 0.1},
	}}
	if got := tie.Nearest(0.5); got != 0 {
		t.Errorf("Nearest exactly between two screens = %d, want the first of them", got)
	}
	// And the same tie measured the other way round the circle.
	tie.screens[0].Centre, tie.screens[1].Centre = wrap(0.75+math.Pi), wrap(0.25+math.Pi)
	if got := tie.Nearest(wrap(0.5 + math.Pi)); got != 0 {
		t.Errorf("Nearest is not stable when the tie straddles the seam: %d", got)
	}
}

// TestVisibilityAgreesWithPlacement sweeps the whole circle and checks the
// answer against the definition — do the two arcs share a longitude — computed
// independently by sampling.
func TestVisibilityAgreesWithPlacement(t *testing.T) {
	r := mustPlace(t, desk, packed(4))
	var buf []int
	for _, spanDeg := range []float64{5, 60, 100, 359, 360} {
		for step := 0; step < 720; step++ {
			yaw := float64(step) * fullCircle / 720
			buf = r.Visible(yaw, spanDeg, buf[:0])
			seen := map[int]bool{}
			for _, i := range buf {
				if seen[i] {
					t.Fatalf("screen %d listed twice", i)
				}
				seen[i] = true
			}
			for i := 0; i < r.Len(); i++ {
				// Independent test: does any longitude belong to both arcs?
				p := r.At(i)
				want := math.Abs(wrapSigned(p.Centre-yaw)) <= rad(spanDeg)/2+p.HalfSpan
				if seen[i] != want {
					t.Fatalf("span %g° yaw %.2f°: screen %d visible=%v, want %v",
						spanDeg, deg(yaw), i, seen[i], want)
				}
			}
		}
	}
	// A 360° arc sees everything, from anywhere.
	if got := len(r.Visible(2.7, 360, nil)); got != r.Len() {
		t.Errorf("a full-circle view sees %d of %d screens", got, r.Len())
	}
	// Reusing the slice must not allocate.
	buf = buf[:0]
	if n := testing.AllocsPerRun(100, func() { _ = r.Visible(1.1, 90, buf[:0]) }); n != 0 {
		t.Errorf("Visible into a reused slice allocated %v times per call", n)
	}
}

func TestRibbonAccessors(t *testing.T) {
	lay := spread(3)
	r := mustPlace(t, desk, lay)
	if r.Len() != len(desk) {
		t.Errorf("Len = %d, want %d", r.Len(), len(desk))
	}
	if r.Layout() != lay {
		t.Errorf("Layout = %+v, want %+v", r.Layout(), lay)
	}
	if got := r.At(1).ID; got != "middle" {
		t.Errorf("At(1).ID = %q", got)
	}
	// The band's reach above the horizon is an arc-tangent of its height on the
	// cylinder, NOT half its angular density. Half the density would be 15°.
	want := math.Atan(rad(30) / 2)
	if got := r.BandHalfAngle(); !closeTo(got, want) {
		t.Errorf("BandHalfAngle = %.4f°, want %.4f° (an arctangent, not half the density)",
			deg(got), deg(want))
	}
	if closeTo(r.BandHalfAngle(), rad(15)) {
		t.Error("BandHalfAngle came out as half the density; the tangent is missing")
	}
	if got := r.At(0).Span(); !closeTo(got, 2*r.At(0).HalfSpan) {
		t.Errorf("Span = %v, want twice the half-span", got)
	}
	// The span follows from the aspect ratio at the configured density.
	if got := r.At(0).Span(); !closeTo(got, rad(30)*16.0/9.0) {
		t.Errorf("a 16:9 screen at 30° of density spans %.3f°, want %.3f°", deg(got), 30*16.0/9.0)
	}
}

func TestDegRad(t *testing.T) {
	if !closeTo(rad(180), math.Pi) || !closeTo(deg(math.Pi), 180) {
		t.Errorf("rad/deg disagree: rad(180)=%v deg(π)=%v", rad(180), deg(math.Pi))
	}
}
