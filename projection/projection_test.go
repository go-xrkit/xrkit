package projection

import (
	"math"
	"testing"

	"github.com/go-xrkit/xrkit/pose"
)

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

var (
	forward = pose.Vec3{X: 0, Y: 0, Z: -1}
	back    = pose.Vec3{X: 0, Y: 0, Z: 1}
	right   = pose.Vec3{X: 1, Y: 0, Z: 0}
	left    = pose.Vec3{X: -1, Y: 0, Z: 0}
	up      = pose.Vec3{X: 0, Y: 1, Z: 0}
	down    = pose.Vec3{X: 0, Y: -1, Z: 0}
)

// dirAt builds a direction yawDeg to the right of and pitchDeg above straight
// ahead, independently of the pose package, so these tests do not inherit its
// conventions.
func dirAt(yawDeg, pitchDeg float64) pose.Vec3 {
	y, p := rad(yawDeg), rad(pitchDeg)
	return pose.Vec3{
		X: math.Sin(y) * math.Cos(p),
		Y: math.Sin(p),
		Z: -math.Cos(y) * math.Cos(p),
	}
}

func TestKindString(t *testing.T) {
	for _, tc := range []struct {
		k    Kind
		want string
	}{
		{Flat, "flat"},
		{Equirect, "equirectangular"},
		{Fisheye, "fisheye"},
		{Kind(9), "Kind(9)"},
	} {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(tc.k), got, tc.want)
		}
	}
}

func TestSampleRefusesTheZeroDirection(t *testing.T) {
	for _, p := range []Projection{Sphere360, Hemisphere180, Fisheye180, Screen} {
		if _, _, ok := p.Sample(pose.Vec3{}); ok {
			t.Errorf("%v accepted the zero vector, which names no direction", p.Kind)
		}
	}
}

func TestSampleEquirect360(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dir    pose.Vec3
		u, v   float64
		wantOK bool
	}{
		{"straight ahead is the centre", forward, 0.5, 0.5, true},
		{"right is a quarter across", right, 0.75, 0.5, true},
		{"left is a quarter back", left, 0.25, 0.5, true},
		{"straight up is the top edge", up, 0.5, 0, true},
		{"straight down is the bottom edge", down, 0.5, 1, true},
		{"behind lands on the seam", back, 1, 0.5, true},
		{"45 right", dirAt(45, 0), 0.625, 0.5, true},
		{"45 up", dirAt(0, 45), 0.5, 0.25, true},
	} {
		u, v, ok := Sphere360.Sample(tc.dir)
		if ok != tc.wantOK {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.wantOK)
			continue
		}
		if !closeTo(u, tc.u) || !closeTo(v, tc.v) {
			t.Errorf("%s: (u,v) = (%v,%v), want (%v,%v)", tc.name, u, v, tc.u, tc.v)
		}
	}
}

// TestSampleEquirect180 is the case that distinguishes a hemisphere from a
// sphere: what is behind the viewer must be reported as absent, not wrapped
// round to the far edge of the picture.
func TestSampleEquirect180(t *testing.T) {
	if u, _, ok := Hemisphere180.Sample(forward); !ok || !closeTo(u, 0.5) {
		t.Errorf("straight ahead = (%v, ok=%v), want the centre", u, ok)
	}
	if u, _, ok := Hemisphere180.Sample(right); !ok || !closeTo(u, 1) {
		t.Errorf("right = (%v, ok=%v), want the right edge", u, ok)
	}
	if u, _, ok := Hemisphere180.Sample(left); !ok || !closeTo(u, 0) {
		t.Errorf("left = (%v, ok=%v), want the left edge", u, ok)
	}
	if _, _, ok := Hemisphere180.Sample(back); ok {
		t.Error("behind the viewer was accepted by a 180-degree projection")
	}
	if _, _, ok := Hemisphere180.Sample(dirAt(120, 0)); ok {
		t.Error("120 degrees round was accepted by a 180-degree projection")
	}
	if u, _, ok := Hemisphere180.Sample(dirAt(45, 0)); !ok || !closeTo(u, 0.75) {
		t.Errorf("45 right = (%v, ok=%v), want 0.75", u, ok)
	}
}

func TestSampleEquirectRejectsDegenerateSpans(t *testing.T) {
	for _, p := range []Projection{
		{Kind: Equirect, HSpanDeg: 0, VSpanDeg: 180},
		{Kind: Equirect, HSpanDeg: 360, VSpanDeg: 0},
		{Kind: Equirect, HSpanDeg: -360, VSpanDeg: 180},
	} {
		if _, _, ok := p.Sample(forward); ok {
			t.Errorf("%+v accepted a sample despite a non-positive span", p)
		}
	}
}

func TestSampleFisheye(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dir    pose.Vec3
		u, v   float64
		wantOK bool
	}{
		{"straight ahead is the centre", forward, 0.5, 0.5, true},
		{"right is the right edge, mid height", right, 1, 0.5, true},
		{"left is the left edge", left, 0, 0.5, true},
		{"up is the top, mid width", up, 0.5, 0, true},
		{"down is the bottom", down, 0.5, 1, true},
		// Equidistant: half the angle is half the radius. 45 degrees up in a
		// 180-degree fisheye is halfway from centre to edge.
		{"45 up is halfway to the edge", dirAt(0, 45), 0.5, 0.25, true},
		{"45 right is halfway across", dirAt(45, 0), 0.75, 0.5, true},
		{"behind is outside the circle", back, 0, 0, false},
		{"120 degrees off axis is outside", dirAt(120, 0), 0, 0, false},
	} {
		u, v, ok := Fisheye180.Sample(tc.dir)
		if ok != tc.wantOK {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.wantOK)
			continue
		}
		if ok && (!closeTo(u, tc.u) || !closeTo(v, tc.v)) {
			t.Errorf("%s: (u,v) = (%v,%v), want (%v,%v)", tc.name, u, v, tc.u, tc.v)
		}
	}
}

// TestFisheyeIsEquidistantNotRectilinear pins the defining property. A tangent
// mapping would agree at the centre and be wrong everywhere else, which is
// exactly the sort of error that looks plausible in a still.
func TestFisheyeIsEquidistantNotRectilinear(t *testing.T) {
	radiusAt := func(deg float64) float64 {
		u, v, ok := Fisheye180.Sample(dirAt(deg, 0))
		if !ok {
			t.Fatalf("%v degrees was rejected", deg)
		}
		return math.Hypot(u-0.5, v-0.5) * 2
	}
	for _, deg := range []float64{15, 30, 45, 60, 75, 90} {
		if got, want := radiusAt(deg), deg/90; !closeTo(got, want) {
			t.Errorf("radius at %v deg = %v, want %v (proportional to the angle)", deg, got, want)
		}
	}
	// And it is NOT the rectilinear mapping, at any angle off centre.
	if got, tanLike := radiusAt(60), math.Tan(rad(60))/math.Tan(rad(90-1e-9)); closeTo(got, tanLike) {
		t.Error("the mapping matched a tangent law; it is not equidistant")
	}
}

func TestSampleFisheyeWideLens(t *testing.T) {
	// A 200-degree lens still sees a little past the horizon.
	wide := Projection{Kind: Fisheye, HSpanDeg: 200}
	if _, _, ok := wide.Sample(dirAt(95, 0)); !ok {
		t.Error("a 200-degree fisheye rejected 95 degrees off axis")
	}
	if _, _, ok := wide.Sample(dirAt(120, 0)); ok {
		t.Error("a 200-degree fisheye accepted 120 degrees off axis")
	}
	// Its centre is still the centre, and the horizon is no longer the edge.
	if u, v, ok := wide.Sample(forward); !ok || !closeTo(u, 0.5) || !closeTo(v, 0.5) {
		t.Errorf("centre = (%v,%v,%v)", u, v, ok)
	}
	if r := func() float64 {
		u, v, _ := wide.Sample(right)
		return math.Hypot(u-0.5, v-0.5) * 2
	}(); !closeTo(r, 90.0/100.0) {
		t.Errorf("the horizon sits at radius %v in a 200-degree lens, want 0.9", r)
	}
}

func TestSampleFisheyeRejectsDegenerateSpan(t *testing.T) {
	for _, span := range []float64{0, -180} {
		if _, _, ok := (Projection{Kind: Fisheye, HSpanDeg: span}).Sample(forward); ok {
			t.Errorf("a fisheye of span %v accepted a sample", span)
		}
	}
}

func TestSampleFlat(t *testing.T) {
	p := Projection{Kind: Flat, HSpanDeg: 60, VSpanDeg: 60}
	if u, v, ok := p.Sample(forward); !ok || !closeTo(u, 0.5) || !closeTo(v, 0.5) {
		t.Errorf("straight ahead = (%v,%v,ok=%v), want the centre", u, v, ok)
	}
	// The screen edge is at half the field of view.
	if u, _, ok := p.Sample(dirAt(30, 0)); !ok || !closeTo(u, 1) {
		t.Errorf("30 degrees right = (%v, ok=%v), want the right edge", u, ok)
	}
	if u, _, ok := p.Sample(dirAt(-30, 0)); !ok || !closeTo(u, 0) {
		t.Errorf("30 degrees left = (%v, ok=%v), want the left edge", u, ok)
	}
	if _, v, ok := p.Sample(dirAt(0, 30)); !ok || !closeTo(v, 0) {
		t.Errorf("30 degrees up = (%v, ok=%v), want the top edge", v, ok)
	}
	// Beyond the screen there is nothing to sample.
	if _, _, ok := p.Sample(dirAt(45, 0)); ok {
		t.Error("45 degrees was accepted by a 60-degree-wide screen")
	}
	// Flat is rectilinear, so a tangent law: half the tangent is half across.
	if u, _, ok := p.Sample(dirAt(math.Atan(math.Tan(rad(30))/2)*180/math.Pi, 0)); !ok || !closeTo(u, 0.75) {
		t.Errorf("half a tangent across = %v, want 0.75", u)
	}
}

// TestSampleFlatRefusesWhatIsBehind is the guard against the perspective divide
// folding the world behind the viewer onto the screen, mirrored.
func TestSampleFlatRefusesWhatIsBehind(t *testing.T) {
	p := Projection{Kind: Flat, HSpanDeg: 60, VSpanDeg: 34}
	for _, name := range []struct {
		n string
		d pose.Vec3
	}{
		{"straight behind", back},
		{"behind and to the side", pose.Vec3{X: 0.1, Y: 0, Z: 1}},
		{"exactly sideways", right},
		{"straight up", up},
	} {
		if _, _, ok := p.Sample(name.d); ok {
			t.Errorf("%s was accepted by a flat screen", name.n)
		}
	}
}

func TestSampleFlatRejectsDegenerateSpans(t *testing.T) {
	for _, p := range []Projection{
		{Kind: Flat, HSpanDeg: 0, VSpanDeg: 34},
		{Kind: Flat, HSpanDeg: 60, VSpanDeg: 0},
		{Kind: Flat, HSpanDeg: -60, VSpanDeg: 34},
	} {
		if _, _, ok := p.Sample(forward); ok {
			t.Errorf("%+v accepted a sample despite a non-positive span", p)
		}
	}
	// The default branch of Sample is Flat, so an unknown Kind lands here too.
	if _, _, ok := (Projection{Kind: Kind(42), HSpanDeg: 60, VSpanDeg: 34}).Sample(forward); !ok {
		t.Error("an unknown Kind should fall back to the flat projection")
	}
}

func TestViewportRay(t *testing.T) {
	// An odd size has an exact centre pixel, which must look straight ahead.
	vp := Viewport{Width: 101, Height: 101, FOVyDeg: 90}
	c := vp.Ray(50, 50)
	if !closeTo(c.X, 0) || !closeTo(c.Y, 0) || !closeTo(c.Z, -1) {
		t.Errorf("centre pixel ray = %+v, want straight ahead", c)
	}

	// Rays are taken through pixel CENTRES, so the first pixel is half a pixel
	// inside the edge, not on it.
	tl := vp.Ray(0, 0)
	if tl.X >= 0 || tl.Y <= 0 {
		t.Errorf("top-left ray = %+v, want left of and above centre", tl)
	}
	if want := (2*0.5/101 - 1) * math.Tan(rad(45)); !closeTo(tl.X, want) {
		t.Errorf("top-left X = %v, want %v (through the pixel centre)", tl.X, want)
	}

	// Opposite corners must be mirror images: proof the sampling grid is centred.
	br := vp.Ray(100, 100)
	if !closeTo(tl.X, -br.X) || !closeTo(tl.Y, -br.Y) {
		t.Errorf("corners are not symmetric: %+v vs %+v", tl, br)
	}

	// The vertical field of view is what was asked for.
	top := vp.Ray(50, 0)
	if got := math.Atan2(top.Y, -top.Z) * 180 / math.Pi; got <= 44 || got >= 45 {
		t.Errorf("top row sits at %v degrees, want just under half of 90", got)
	}

	// A wide viewport widens HORIZONTALLY only: the vertical field of view is
	// what was asked for, whatever the aspect. Compared at the SAME height, so
	// the pixel-centre offset is identical and only the aspect differs.
	wide := Viewport{Width: 202, Height: 101, FOVyDeg: 90}
	if got, want := wide.Ray(201, 50).X, (2*201.5/202-1)*math.Tan(rad(45))*2; !closeTo(got, want) {
		t.Errorf("wide viewport rightmost X = %v, want %v", got, want)
	}
	if !closeTo(wide.Ray(101, 0).Y, vp.Ray(50, 0).Y) {
		t.Error("aspect ratio changed the vertical field of view")
	}
	// Doubling the aspect must double the horizontal extent, exactly. Isolating
	// that means keeping the WIDTH fixed and halving the height: same pixel grid
	// across, so the half-pixel centring cancels and only the aspect differs.
	// Comparing two different widths instead would fold in the fact that the last
	// pixel centre of a 202-wide image is not at twice the fraction of a
	// 101-wide one -- which is a property of pixel grids, not of the projection.
	square := Viewport{Width: 202, Height: 202, FOVyDeg: 90}
	tall := Viewport{Width: 202, Height: 101, FOVyDeg: 90}
	for _, x := range []int{0, 37, 100, 201} {
		if got, want := tall.Ray(x, 50).X, 2*square.Ray(x, 50).X; !closeTo(got, want) {
			t.Errorf("x=%d: doubling the aspect gave X=%v, want %v", x, got, want)
		}
	}
}

func TestViewportRayRefusesDegenerateSizes(t *testing.T) {
	for _, vp := range []Viewport{
		{Width: 0, Height: 100, FOVyDeg: 90},
		{Width: 100, Height: 0, FOVyDeg: 90},
		{Width: -1, Height: -1, FOVyDeg: 90},
	} {
		if got := vp.Ray(0, 0); got != (pose.Vec3{}) {
			t.Errorf("%+v gave a ray %+v, want the zero vector", vp, got)
		}
	}
}

// TestLookRayComposesOrientation is the per-pixel operation of the whole
// renderer: an output pixel plus a head orientation gives a world direction,
// which the projection then turns into a place in the picture.
func TestLookRayComposesOrientation(t *testing.T) {
	vp := Viewport{Width: 101, Height: 101, FOVyDeg: 90}
	// Looking 90 degrees right, the centre pixel must sample 90 degrees right.
	q := pose.FromEulerZXY(pose.Euler{Yaw: -90}) // negative yaw looks right
	dir := vp.LookRay(q, 50, 50).Unit()
	if !closeTo(dir.X, 1) || !closeTo(dir.Y, 0) || !closeTo(dir.Z, 0) {
		t.Fatalf("centre ray after yawing right = %+v, want +X", dir)
	}
	// And through a full sphere that is a quarter across the picture.
	if u, v, ok := Sphere360.Sample(dir); !ok || !closeTo(u, 0.75) || !closeTo(v, 0.5) {
		t.Errorf("sampled (%v,%v,ok=%v), want (0.75,0.5)", u, v, ok)
	}
	// With no rotation, the same pixel samples the centre.
	if u, _, ok := Sphere360.Sample(vp.LookRay(pose.Identity(), 50, 50)); !ok || !closeTo(u, 0.5) {
		t.Errorf("unrotated centre sampled u=%v, want 0.5", u)
	}
}

func TestInUnit(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want bool
	}{{-0.001, false}, {0, true}, {0.5, true}, {1, true}, {1.001, false}} {
		if got := inUnit(tc.in); got != tc.want {
			t.Errorf("inUnit(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestClampUnit(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{0, 0}, {1, 1}, {-1, -1}, {0.25, 0.25},
		{1 + 1e-16, 1}, {2, 1}, {-1 - 1e-16, -1}, {-2, -1},
	} {
		if got := clampUnit(tc.in); got != tc.want {
			t.Errorf("clampUnit(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRad(t *testing.T) {
	if !closeTo(rad(180), math.Pi) {
		t.Errorf("rad(180) = %v, want pi", rad(180))
	}
	if rad(0) != 0 {
		t.Error("rad(0) is not 0")
	}
}
