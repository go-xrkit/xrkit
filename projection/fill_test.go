package projection

import (
	"math"
	"testing"
)

// The eye the VITURE glasses present, and the figures measured through them.
const (
	eyeW, eyeH   = 1920, 1200
	viewAspect   = float64(eyeW) / float64(eyeH)
	measuredAt90 = 0.110
	measuredAt45 = 0.643
)

// tangents returns a flat screen's half-extents at unit depth, which is the
// space the geometry works in.
func tangents(p Projection) (h, v float64) {
	return math.Tan(rad(p.HSpanDeg) / 2), math.Tan(rad(p.VSpanDeg) / 2)
}

// coverage is the fraction of a viewport a flat screen covers, the same
// quantity the player logs after building its warp maps.
func coverage(p Projection, fovyDeg, aspect float64) float64 {
	sh, sv := tangents(p)
	tv := math.Tan(rad(fovyDeg) / 2)
	th := tv * aspect
	return math.Min(sh/th, 1) * math.Min(sv/tv, 1)
}

// TestTheFixedScreenIsWrongTwice is the negative control: it witnesses both
// faults FillScreen answers, against figures MEASURED on the glasses rather
// than derived here. Should either stop being a fault, this fails and the fix
// should be reconsidered rather than kept out of habit.
func TestTheFixedScreenIsWrongTwice(t *testing.T) {
	h, v := tangents(Screen)
	const sixteenNine = 16.0 / 9.0
	if shape := h / v; math.Abs(shape-sixteenNine) < 0.01 {
		t.Errorf("Screen is %.4f:1, which IS 16:9, so it distorts nothing", shape)
	} else if stretch := shape / sixteenNine; stretch < 1.05 || stretch > 1.08 {
		t.Errorf("16:9 on Screen stretches by %.3f, want about 1.062", stretch)
	}
	for _, c := range []struct {
		fovy, want float64
	}{{90, measuredAt90}, {45, measuredAt45}} {
		if got := coverage(Screen, c.fovy, viewAspect); math.Abs(got-c.want) > 0.001 {
			t.Errorf("at %.0f degrees this geometry covers %.3f of the eye; the glasses measured %.3f", c.fovy, got, c.want)
		}
	}
}

func TestFillScreenKeepsThePictureShape(t *testing.T) {
	for _, aspect := range []float64{16.0 / 9.0, 4.0 / 3.0, 2.39, 1.0, 0.75} {
		p, ok := FillScreen(45, viewAspect, aspect)
		if !ok {
			t.Fatalf("FillScreen refused a %v picture", aspect)
		}
		if p.Kind != Flat {
			t.Errorf("FillScreen made a %v, want Flat", p.Kind)
		}
		h, v := tangents(p)
		if got := h / v; math.Abs(got-aspect) > 1e-9 {
			t.Errorf("a %v picture came back as %v", aspect, got)
		}
	}
}

// TestFillScreenFills looks THROUGH the viewport: the picture reaches the edge
// it is fitted by, and does not spill past the one it is not. Both are checked
// with Ray and Sample rather than by repeating the arithmetic under test.
func TestFillScreenFills(t *testing.T) {
	for _, aspect := range []float64{16.0 / 9.0, 2.39, 1.0, 0.75} {
		p, ok := FillScreen(45, viewAspect, aspect)
		if !ok {
			t.Fatalf("FillScreen refused a %v picture", aspect)
		}
		vp := Viewport{Width: eyeW, Height: eyeH, FOVyDeg: 45}
		onPicture := func(x, y int) bool {
			_, _, ok := p.Sample(vp.Ray(x, y))
			return ok
		}
		// One of the two tight edges is reached: a picture wider than the view
		// touches left and right, a narrower one touches top and bottom.
		if !onPicture(eyeW/2, 0) && !onPicture(0, eyeH/2) {
			t.Errorf("a %v picture reaches neither edge of the view", aspect)
		}
		// It does not spill: a picture that is not exactly the view's shape
		// leaves the corners showing background.
		if onPicture(0, 0) {
			t.Errorf("a %v picture covers the corner of a %v view, so it is cropped", aspect, viewAspect)
		}
		// And it fills EXACTLY as much as containing a picture of that shape
		// in a view of this one allows: the shorter side of the two ratios
		// over the longer, with the remainder unavoidably background.
		want := math.Min(aspect, viewAspect) / math.Max(aspect, viewAspect)
		if got := coverage(p, 45, viewAspect); math.Abs(got-want) > 1e-9 {
			t.Errorf("a %v picture fills %.4f of the view, want %.4f", aspect, got, want)
		}
	}
}

// TestFillScreenIsTheSameWhateverTheField checks the claim the documentation
// makes: filling does not depend on the field of view, because the screen grows
// with the view. Two very different fields must sample the same picture pixels.
func TestFillScreenIsTheSameWhateverTheField(t *testing.T) {
	const aspect = 16.0 / 9.0
	narrow, ok1 := FillScreen(26, viewAspect, aspect)
	wide, ok2 := FillScreen(90, viewAspect, aspect)
	if !ok1 || !ok2 {
		t.Fatal("FillScreen refused an ordinary field of view")
	}
	vpN := Viewport{Width: eyeW, Height: eyeH, FOVyDeg: 26}
	vpW := Viewport{Width: eyeW, Height: eyeH, FOVyDeg: 90}
	for _, y := range []int{0, eyeH / 3, eyeH / 2, eyeH - 1} {
		for _, x := range []int{0, eyeW / 3, eyeW / 2, eyeW - 1} {
			un, vn, okN := narrow.Sample(vpN.Ray(x, y))
			uw, vw, okW := wide.Sample(vpW.Ray(x, y))
			if okN != okW {
				t.Fatalf("pixel %d,%d is on the picture at one field and not the other", x, y)
			}
			if okN && (math.Abs(un-uw) > 1e-9 || math.Abs(vn-vw) > 1e-9) {
				t.Fatalf("pixel %d,%d samples %v,%v at 26 degrees and %v,%v at 90", x, y, un, vn, uw, vw)
			}
		}
	}
}

// TestFillScreenAvoidsBothFaults states the point of the exercise as the two
// ways the fixed screen fails, at the vertical fields of view real models
// actually have. Filling has neither fault at any of them.
func TestFillScreenAvoidsBothFaults(t *testing.T) {
	const film = 16.0 / 9.0
	want := math.Min(film, viewAspect) / math.Max(film, viewAspect)
	cropped := func(p Projection, fovy float64) bool {
		vp := Viewport{Width: eyeW, Height: eyeH, FOVyDeg: fovy}
		// A picture larger than the view leaves no background anywhere, so the
		// corner of the view is on it and the edges of the film are lost.
		_, _, ok := p.Sample(vp.Ray(0, 0))
		return ok
	}
	for _, fovy := range []float64{26, 29, 34, 90} {
		p, ok := FillScreen(fovy, viewAspect, film)
		if !ok {
			t.Fatalf("FillScreen refused a %v-degree view", fovy)
		}
		if cropped(p, fovy) {
			t.Errorf("at %v degrees the filled screen crops the film", fovy)
		}
		if got := coverage(p, fovy, viewAspect); math.Abs(got-want) > 1e-9 {
			t.Errorf("at %v degrees filling covers %.4f, want %.4f whatever the field", fovy, got, want)
		}
		// The fixed screen fails one way or the other at every one of these: it
		// crops at the narrow fields these glasses really have, and at a wide
		// one it strands the film in the middle of the eye.
		if !cropped(Screen, fovy) && coverage(Screen, fovy, viewAspect) > 0.5 {
			t.Errorf("at %v degrees the fixed screen neither crops nor strands the film, so it needed no fixing", fovy)
		}
	}
}

func TestFillScreenRefusesWhatIsNotAView(t *testing.T) {
	cases := []struct {
		name                            string
		fovy, viewAspect, pictureAspect float64
	}{
		{"no field of view", 0, 1.6, 1.78},
		{"a field behind the viewer", -45, 1.6, 1.78},
		{"a field of a half turn", 180, 1.6, 1.78},
		{"a field past a half turn", 181, 1.6, 1.78},
		{"a field that is not a number", math.NaN(), 1.6, 1.78},
		{"an endless field", math.Inf(1), 1.6, 1.78},
		{"a view with no width", 45, 0, 1.78},
		{"a view of negative shape", 45, -1.6, 1.78},
		{"a view shape that is not a number", 45, math.NaN(), 1.78},
		{"an endless view shape", 45, math.Inf(1), 1.78},
		{"a picture with no width", 45, 1.6, 0},
		{"a picture of negative shape", 45, 1.6, -1.78},
		{"a picture shape that is not a number", 45, 1.6, math.NaN()},
		{"an endless picture shape", 45, 1.6, math.Inf(1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if p, ok := FillScreen(c.fovy, c.viewAspect, c.pictureAspect); ok {
				t.Errorf("FillScreen accepted %s and returned %v", c.name, p)
			}
		})
	}
	// The positive control: the same call with ordinary numbers is accepted, so
	// the refusals above are about the numbers and not about the function.
	if _, ok := FillScreen(45, 1.6, 1.78); !ok {
		t.Error("FillScreen refused an ordinary view, so the refusals prove nothing")
	}
}

func TestDegIsTheInverseOfRad(t *testing.T) {
	for _, d := range []float64{0, 1, 26, 34, 60, 90, 179.5} {
		if got := deg(rad(d)); math.Abs(got-d) > 1e-12 {
			t.Errorf("deg(rad(%v)) = %v", d, got)
		}
	}
}
