package pose

import (
	"math"
	"testing"
)

const eps = 1e-9

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func vecCloseTo(a, b Vec3) bool {
	return closeTo(a.X, b.X) && closeTo(a.Y, b.Y) && closeTo(a.Z, b.Z)
}

// forward is where a viewer looks with no rotation applied: down -Z.
var forward = Vec3{0, 0, -1}

func TestVecArithmetic(t *testing.T) {
	a, b := Vec3{1, 2, 3}, Vec3{4, 5, 6}
	if got := a.Add(b); got != (Vec3{5, 7, 9}) {
		t.Errorf("Add = %v", got)
	}
	if got := a.Sub(b); got != (Vec3{-3, -3, -3}) {
		t.Errorf("Sub = %v", got)
	}
	if got := a.Scale(2); got != (Vec3{2, 4, 6}) {
		t.Errorf("Scale = %v", got)
	}
	if got := a.Dot(b); got != 32 {
		t.Errorf("Dot = %v, want 32", got)
	}
	if got := (Vec3{3, 4, 0}).Len(); got != 5 {
		t.Errorf("Len = %v, want 5", got)
	}
	if got := (Vec3{0, 5, 0}).Unit(); !vecCloseTo(got, Vec3{0, 1, 0}) {
		t.Errorf("Unit = %v", got)
	}
	// The zero vector has no direction; it must come back unchanged, not NaN.
	if got := (Vec3{}).Unit(); got != (Vec3{}) {
		t.Errorf("Unit of zero = %v, want the zero vector", got)
	}
}

func TestCross(t *testing.T) {
	// Right-handed: X × Y = Z.
	if got := cross(Vec3{1, 0, 0}, Vec3{0, 1, 0}); !vecCloseTo(got, Vec3{0, 0, 1}) {
		t.Errorf("X x Y = %v, want +Z (the space must be right-handed)", got)
	}
}

func TestIdentityRotatesNothing(t *testing.T) {
	if got := Identity().Rotate(forward); !vecCloseTo(got, forward) {
		t.Errorf("Identity().Rotate(forward) = %v, want %v", got, forward)
	}
	if got := Identity().Angle(); !closeTo(got, 0) {
		t.Errorf("Identity().Angle() = %v, want 0", got)
	}
}

// TestAxisConventions pins the sign of each axis against an independently
// derived expectation, so a round-trip test cannot hide a self-consistent but
// wrong convention. These are the three claims the rest of the package rests on.
func TestAxisConventions(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    Euler
		in   Vec3
		want Vec3
	}{
		// Yaw +90° about +Y turns the view to the viewer's left: -Z -> -X.
		{"yaw +90 looks left", Euler{Yaw: 90}, forward, Vec3{-1, 0, 0}},
		{"yaw -90 looks right", Euler{Yaw: -90}, forward, Vec3{1, 0, 0}},
		// Pitch +90° about +X looks up: -Z -> +Y.
		{"pitch +90 looks up", Euler{Pitch: 90}, forward, Vec3{0, 1, 0}},
		{"pitch -90 looks down", Euler{Pitch: -90}, forward, Vec3{0, -1, 0}},
		// Roll +90° about +Z tips up towards -X.
		{"roll +90 tilts up-vector", Euler{Roll: 90}, Vec3{0, 1, 0}, Vec3{-1, 0, 0}},
		// Roll leaves the view direction alone, which is what makes it roll.
		{"roll does not move forward", Euler{Roll: 45}, forward, forward},
		// Yaw leaves the up-vector alone, which is what keeps the horizon level.
		{"yaw does not move up", Euler{Yaw: 33}, Vec3{0, 1, 0}, Vec3{0, 1, 0}},
	} {
		got := FromEulerZXY(tc.e).Rotate(tc.in)
		if !vecCloseTo(got, tc.want) {
			t.Errorf("%s: %+v rotating %v = %v, want %v", tc.name, tc.e, tc.in, got, tc.want)
		}
	}
}

// TestYawMustBeAppliedLast is the regression test for a bug this package had:
// the composition applied yaw FIRST, in the global frame. Every single-axis
// test still passed, because with one non-zero angle the order cannot matter.
// What broke was the combination -- pitching to 90 degrees with any yaw did not
// look straight up, so there was no gimbal lock and the horizon swung as the
// viewer raised their head.
//
// So this asserts the two orders differ AND that ours is the one where pitching
// fully up looks up whatever the yaw.
func TestYawMustBeAppliedLast(t *testing.T) {
	e := Euler{Roll: 30, Pitch: 40, Yaw: 50}
	r, p, y := rad(e.Roll), rad(e.Pitch), rad(e.Yaw)
	qz := Quat{W: math.Cos(r / 2), Z: math.Sin(r / 2)}
	qx := Quat{W: math.Cos(p / 2), X: math.Sin(p / 2)}
	qy := Quat{W: math.Cos(y / 2), Y: math.Sin(y / 2)}

	ours := FromEulerZXY(e)
	yawFirst := qz.Mul(qx).Mul(qy) // the old, wrong composition
	if vecCloseTo(ours.Rotate(forward), yawFirst.Rotate(forward)) {
		t.Error("both orders gave the same view direction; this test no longer discriminates")
	}

	// The decisive property: straight up is straight up, whatever the yaw.
	up := Vec3{0, 1, 0}
	for _, yaw := range []float64{0, 40, 137, -95} {
		if got := FromEulerZXY(Euler{Pitch: 90, Yaw: yaw}).Rotate(forward); !vecCloseTo(got, up) {
			t.Errorf("pitch 90 with yaw %v looks at %v, want straight up %v", yaw, got, up)
		}
		// And the same for straight down.
		if got := FromEulerZXY(Euler{Pitch: -90, Yaw: yaw}).Rotate(forward); !vecCloseTo(got, up.Scale(-1)) {
			t.Errorf("pitch -90 with yaw %v looks at %v, want straight down", yaw, got)
		}
	}

	// Yaw must also leave the horizon level at any pitch: the rotated right-hand
	// axis stays in the horizontal plane.
	for _, pitch := range []float64{-60, -10, 0, 25, 75} {
		right := FromEulerZXY(Euler{Pitch: pitch, Yaw: 33}).Rotate(Vec3{1, 0, 0})
		if !closeTo(right.Y, 0) {
			t.Errorf("pitch %v with yaw: right axis has Y=%v, want 0 (horizon must stay level)", pitch, right.Y)
		}
	}
}

func TestEulerRoundTrip(t *testing.T) {
	for _, e := range []Euler{
		{}, {Yaw: 45}, {Pitch: 30}, {Roll: -20},
		{Roll: 10, Pitch: 20, Yaw: 30},
		{Roll: -170, Pitch: -80, Yaw: 179},
		{Roll: 120, Pitch: 85, Yaw: -95},
	} {
		got := FromEulerZXY(e).EulerZXY()
		// Compare through the rotation, not the angles: two different triples can
		// name the same orientation, and that is not an error.
		a := FromEulerZXY(e).Rotate(forward)
		b := FromEulerZXY(got).Rotate(forward)
		up := Vec3{0, 1, 0}
		au := FromEulerZXY(e).Rotate(up)
		bu := FromEulerZXY(got).Rotate(up)
		if !vecCloseTo(a, b) || !vecCloseTo(au, bu) {
			t.Errorf("round trip of %+v gave %+v: forward %v vs %v, up %v vs %v", e, got, a, b, au, bu)
		}
	}
}

func TestEulerGimbalLock(t *testing.T) {
	// Straight up: yaw and roll are no longer distinguishable. The decomposition
	// must still return finite angles with the documented choice (roll zero).
	e := FromEulerZXY(Euler{Pitch: 90, Yaw: 40}).EulerZXY()
	if math.IsNaN(e.Roll) || math.IsNaN(e.Pitch) || math.IsNaN(e.Yaw) {
		t.Fatalf("gimbal lock produced NaN: %+v", e)
	}
	if !closeTo(e.Pitch, 90) {
		t.Errorf("pitch = %v, want 90", e.Pitch)
	}
	if e.Roll != 0 {
		t.Errorf("roll = %v, want 0 (the documented choice at lock)", e.Roll)
	}
	// And the orientation must still be the one we started from.
	if got := FromEulerZXY(e).Rotate(forward); !vecCloseTo(got, Vec3{0, 1, 0}) {
		t.Errorf("recomposed direction = %v, want straight up", got)
	}
}

func TestMulOrderAppliesRightFirst(t *testing.T) {
	yaw := FromEulerZXY(Euler{Yaw: 90})
	pitch := FromEulerZXY(Euler{Pitch: 90})
	// pitch.Mul(yaw) must equal "yaw first, then pitch".
	stepwise := pitch.Rotate(yaw.Rotate(forward))
	combined := pitch.Mul(yaw).Rotate(forward)
	if !vecCloseTo(stepwise, combined) {
		t.Errorf("Mul applied the wrong operand first: %v vs %v", combined, stepwise)
	}
}

func TestConjIsTheInverse(t *testing.T) {
	q := FromEulerZXY(Euler{Roll: 12, Pitch: -34, Yaw: 56})
	if got := q.Conj().Mul(q).Rotate(forward); !vecCloseTo(got, forward) {
		t.Errorf("q* q rotated forward to %v, want it unchanged", got)
	}
}

func TestQuatUnitAndLen(t *testing.T) {
	if got := Identity().Len(); !closeTo(got, 1) {
		t.Errorf("Identity().Len() = %v, want 1", got)
	}
	q := Quat{W: 2}
	if got := q.Unit().Len(); !closeTo(got, 1) {
		t.Errorf("Unit().Len() = %v, want 1", got)
	}
	// A zero quaternion names no rotation; it must degrade to identity, not NaN.
	if got := (Quat{}).Unit(); got != Identity() {
		t.Errorf("zero quaternion Unit() = %v, want Identity", got)
	}
	if got := (Quat{}).Len(); got != 0 {
		t.Errorf("zero quaternion Len() = %v, want 0", got)
	}
}

func TestAngle(t *testing.T) {
	for _, deg := range []float64{0, 30, 90, 180} {
		q := FromEulerZXY(Euler{Yaw: deg})
		if got := q.Angle(); !closeTo(got, rad(deg)) {
			t.Errorf("Angle of yaw %v deg = %v rad, want %v", deg, got, rad(deg))
		}
	}
	// A quaternion slightly off the unit sphere must not push acos out of domain.
	if got := (Quat{W: 1 + 1e-12}).Angle(); math.IsNaN(got) {
		t.Error("Angle produced NaN on a near-unit quaternion")
	}
}

func TestSlerpEndpointsAndClamping(t *testing.T) {
	a := FromEulerZXY(Euler{Yaw: 0})
	b := FromEulerZXY(Euler{Yaw: 90})
	if got := Slerp(a, b, 0); !vecCloseTo(got.Rotate(forward), a.Rotate(forward)) {
		t.Error("Slerp at t=0 is not the start")
	}
	if got := Slerp(a, b, 1); !vecCloseTo(got.Rotate(forward), b.Rotate(forward)) {
		t.Error("Slerp at t=1 is not the end")
	}
	if got := Slerp(a, b, -5); !vecCloseTo(got.Rotate(forward), a.Rotate(forward)) {
		t.Error("Slerp did not clamp t below 0")
	}
	if got := Slerp(a, b, 99); !vecCloseTo(got.Rotate(forward), b.Rotate(forward)) {
		t.Error("Slerp did not clamp t above 1")
	}
	// Halfway between 0 and 90 degrees of yaw is 45.
	mid := Slerp(a, b, 0.5)
	if got := mid.EulerZXY().Yaw; !closeTo(got, 45) {
		t.Errorf("Slerp midpoint yaw = %v, want 45", got)
	}
}

// TestSlerpTakesTheShortArc is the one that matters visually: a quaternion and
// its negation are the same rotation, so without the sign check the view would
// travel the long way round.
func TestSlerpTakesTheShortArc(t *testing.T) {
	a := FromEulerZXY(Euler{Yaw: 10})
	b := FromEulerZXY(Euler{Yaw: 30})
	negB := Quat{-b.W, -b.X, -b.Y, -b.Z} // same rotation, opposite sign
	viaB := Slerp(a, b, 0.5)
	viaNeg := Slerp(a, negB, 0.5)
	if !vecCloseTo(viaB.Rotate(forward), viaNeg.Rotate(forward)) {
		t.Errorf("negating an endpoint changed the path: %v vs %v",
			viaB.Rotate(forward), viaNeg.Rotate(forward))
	}
	if got := viaB.EulerZXY().Yaw; !closeTo(got, 20) {
		t.Errorf("midpoint yaw = %v, want 20 (the short way)", got)
	}
}

func TestSlerpNearlyIdentical(t *testing.T) {
	a := FromEulerZXY(Euler{Yaw: 10})
	b := FromEulerZXY(Euler{Yaw: 10 + 1e-9})
	got := Slerp(a, b, 0.5)
	if math.IsNaN(got.W) || !closeTo(got.Len(), 1) {
		t.Fatalf("Slerp of near-identical orientations = %+v, want a unit quaternion", got)
	}
	if y := got.EulerZXY().Yaw; !closeTo(y, 10) {
		t.Errorf("yaw = %v, want ~10", y)
	}
}

func TestRecentre(t *testing.T) {
	r := NewRecentre()
	q := FromEulerZXY(Euler{Yaw: 70, Pitch: 15})

	// With no reference set, Apply is a no-op.
	if got := r.Apply(q); !vecCloseTo(got.Rotate(forward), q.Rotate(forward)) {
		t.Error("Apply before Set changed the orientation")
	}
	if r.Reference() != Identity() {
		t.Errorf("Reference() = %+v, want Identity", r.Reference())
	}

	// After recentring on q, q itself must read as straight ahead.
	r.Set(q)
	if got := r.Apply(q).Rotate(forward); !vecCloseTo(got, forward) {
		t.Errorf("after Set(q), Apply(q) looks at %v, want straight ahead %v", got, forward)
	}
	// And a further rotation must read as exactly that much from the reference.
	extra := FromEulerZXY(Euler{Yaw: 20})
	if got := r.Apply(q.Mul(extra)).EulerZXY().Yaw; !closeTo(got, 20) {
		t.Errorf("relative yaw = %v, want 20", got)
	}
}

func TestSmoother(t *testing.T) {
	var s Smoother

	if q, ok := s.Current(); ok || q != Identity() {
		t.Errorf("Current() before any sample = (%+v, %v), want (Identity, false)", q, ok)
	}

	// The first sample is adopted whole: easing in from an arbitrary start would
	// swing the view on frame one.
	first := FromEulerZXY(Euler{Yaw: 40})
	if got := s.Update(first); !closeTo(got.EulerZXY().Yaw, 40) {
		t.Errorf("first Update yaw = %v, want 40 adopted as-is", got.EulerZXY().Yaw)
	}
	if q, ok := s.Current(); !ok || !closeTo(q.EulerZXY().Yaw, 40) {
		t.Errorf("Current() = (%v, %v), want the first sample", q.EulerZXY().Yaw, ok)
	}

	// Alpha 0.5 must land halfway.
	s.Alpha = 0.5
	if got := s.Update(FromEulerZXY(Euler{Yaw: 60})).EulerZXY().Yaw; !closeTo(got, 50) {
		t.Errorf("smoothed yaw = %v, want 50", got)
	}

	// An out-of-range Alpha must mean "no smoothing", never "freeze".
	for _, a := range []float64{0, -1, 2} {
		s.Reset()
		s.Update(FromEulerZXY(Euler{Yaw: 0}))
		s.Alpha = a
		if got := s.Update(FromEulerZXY(Euler{Yaw: 80})).EulerZXY().Yaw; !closeTo(got, 80) {
			t.Errorf("Alpha=%v: yaw = %v, want the new sample taken whole (80)", a, got)
		}
	}

	s.Reset()
	if q, ok := s.Current(); ok || q != Identity() {
		t.Errorf("Current() after Reset = (%+v, %v), want (Identity, false)", q, ok)
	}
}

func TestDegRadRoundTrip(t *testing.T) {
	for _, d := range []float64{-180, -1, 0, 1, 90, 360} {
		if got := deg(rad(d)); math.Abs(got-d) > eps {
			t.Errorf("deg(rad(%v)) = %v", d, got)
		}
	}
}

// TestClampUnit covers the Asin/Acos domain guard directly. It cannot be
// reached through the public API — Unit() normalises first — but the guard is
// what stops a few ulps of rounding from becoming a NaN, and a NaN here is not
// a crash: it silently becomes a hole in the rendered picture.
func TestClampUnit(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{0, 0},
		{1, 1},
		{-1, -1},
		{0.5, 0.5},
		{1 + 1e-16, 1},
		{1.5, 1},
		{-1 - 1e-16, -1},
		{-2, -1},
	} {
		if got := clampUnit(tc.in); got != tc.want {
			t.Errorf("clampUnit(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	// And the point of it: neither bound produces NaN downstream.
	if got := math.Acos(clampUnit(1 + 1e-16)); math.IsNaN(got) {
		t.Error("Acos(clampUnit(>1)) is NaN")
	}
	if got := math.Asin(clampUnit(-1 - 1e-16)); math.IsNaN(got) {
		t.Error("Asin(clampUnit(<-1)) is NaN")
	}
}
