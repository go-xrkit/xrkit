package ribbon

import (
	"errors"
	"math"
	"testing"

	"github.com/go-xrkit/xrkit/pose"
	"github.com/go-xrkit/xrkit/projection"
)

// settleNav runs the motion to a standstill, and reports how many frames it
// took. It refuses to run forever, because "does not settle" is the failure this
// is here to catch and a hung test reports it badly.
func settleNav(t *testing.T, n *Nav, dt float64) int {
	t.Helper()
	for i := 0; i < 100000; i++ {
		if !n.Moving() {
			return i
		}
		n.Advance(dt)
	}
	t.Fatalf("yaw never settled: %.9f away from target", math.Abs(n.target-n.yaw))
	return 0
}

func TestModeString(t *testing.T) {
	for _, tc := range []struct {
		m    Mode
		want string
	}{{ModeRibbon, "ribbon"}, {ModeFullscreen, "fullscreen"}, {Mode(5), "Mode(5)"}} {
		if got := tc.m.String(); got != tc.want {
			t.Errorf("Mode(%d).String() = %q, want %q", int(tc.m), got, tc.want)
		}
	}
}

func TestNavStartsFacingTheFirstScreen(t *testing.T) {
	r := mustPlace(t, desk, spread(3))
	n := NewNav(r)
	if n.Ribbon() != r {
		t.Error("Ribbon() is not the ribbon it was given")
	}
	if n.Focus() != 0 {
		t.Errorf("Focus = %d, want 0", n.Focus())
	}
	if !closeTo(n.Yaw(), r.At(0).Centre) || !closeTo(n.Target(), r.At(0).Centre) {
		t.Errorf("starts at %.3f°, want the first screen at %.3f°", deg(n.Yaw()), deg(r.At(0).Centre))
	}
	if n.Moving() {
		t.Error("a fresh Nav is already moving")
	}
	if n.Mode() != ModeRibbon {
		t.Errorf("Mode = %v, want the ribbon", n.Mode())
	}
	if n.Tau != DefaultTau {
		t.Errorf("Tau = %v, want %v", n.Tau, DefaultTau)
	}
}

func TestGoToRejectsAnIndexThatIsNotThere(t *testing.T) {
	n := NewNav(mustPlace(t, desk, spread(3)))
	for _, i := range []int{-1, 3, 99} {
		if err := n.GoTo(i); !errors.Is(err, ErrIndex) {
			t.Errorf("GoTo(%d) err = %v, want %v", i, err, ErrIndex)
		}
	}
	if n.Focus() != 0 || n.Moving() {
		t.Error("a refused GoTo moved the viewer anyway")
	}
	if err := n.GoTo(2); err != nil {
		t.Errorf("GoTo(2): %v", err)
	}
	if n.Focus() != 2 {
		t.Errorf("Focus = %d, want 2", n.Focus())
	}
}

func TestSetMode(t *testing.T) {
	n := NewNav(mustPlace(t, desk, spread(3)))
	if err := n.SetMode(Mode(4)); !errors.Is(err, ErrMode) {
		t.Errorf("SetMode(Mode(4)) err = %v, want %v", err, ErrMode)
	}
	if n.Mode() != ModeRibbon {
		t.Error("a refused SetMode changed the mode anyway")
	}
	if err := n.SetMode(ModeFullscreen); err != nil {
		t.Fatal(err)
	}
	if n.Mode() != ModeFullscreen {
		t.Error("SetMode(ModeFullscreen) did not take")
	}
	n.ToggleFullscreen()
	if n.Mode() != ModeRibbon {
		t.Error("toggling out of fullscreen did not return to the ribbon")
	}
	n.ToggleFullscreen()
	if n.Mode() != ModeFullscreen {
		t.Error("toggling into fullscreen did not promote the screen")
	}
	// Promoting a screen must not move the viewer: leaving fullscreen has to put
	// them back where they were, not somewhere else.
	if n.Moving() {
		t.Error("changing mode set the ribbon turning")
	}
}

// TestStepAcrossTheSeamIsAShortHop is the headline behaviour: on a ribbon that
// fills the circle, going from the last screen to the first turns FORWARDS by a
// gap, not backwards by a lap. It asserts the naive answer too, so a wrong
// implementation cannot pass by agreeing with itself.
func TestStepAcrossTheSeamIsAShortHop(t *testing.T) {
	r := mustPlace(t, desk, spread(3))
	n := NewNav(r)
	if err := n.GoTo(r.Len() - 1); err != nil {
		t.Fatal(err)
	}
	settleNav(t, n, 1.0/60)
	before := n.yaw
	n.Next()
	step := n.target - before
	if step <= 0 {
		t.Errorf("last screen to first turned %.1f°, want a forwards hop", deg(step))
	}
	if math.Abs(step) > math.Pi {
		t.Errorf("last screen to first turned %.1f°, want the short way round", deg(step))
	}
	// The naive difference is the long way round; if it were not, this test
	// would pass without the wrapping being right.
	naive := r.At(0).Centre - wrap(before)
	if math.Abs(naive) <= math.Pi {
		t.Skip("this arrangement does not straddle the seam; the test proves nothing")
	}
	if closeTo(naive, step) {
		t.Error("the naive difference already gives the short hop")
	}
}

// TestNextAndPrevAreInverse, and a full lap of the ribbon comes back exactly
// where it started — the sequence property, not a single step.
func TestAFullLapReturnsToTheStart(t *testing.T) {
	for _, lay := range []Layout{packed(4), spread(4)} {
		for _, screens := range [][]Screen{desk, {{W: 1920, H: 1080}, {W: 1920, H: 1080}}} {
			r := mustPlace(t, screens, lay)
			n := NewNav(r)
			start := n.Yaw()
			for i := 0; i < r.Len(); i++ {
				n.Next()
				settleNav(t, n, 1.0/60)
			}
			if n.Focus() != 0 {
				t.Errorf("%s x%d: a full lap of Next ended on screen %d", lay.Arrangement, r.Len(), n.Focus())
			}
			if got := n.Yaw(); !closeTo(got, start) {
				t.Errorf("%s x%d: a full lap of Next ended at %.9f°, started at %.9f°",
					lay.Arrangement, r.Len(), deg(got), deg(start))
			}
			// And the same backwards.
			for i := 0; i < r.Len(); i++ {
				n.Prev()
				settleNav(t, n, 1.0/60)
			}
			if n.Focus() != 0 || !closeTo(n.Yaw(), start) {
				t.Errorf("%s x%d: a full lap of Prev ended on screen %d at %.9f°",
					lay.Arrangement, r.Len(), n.Focus(), deg(n.Yaw()))
			}
			// Next then Prev is a round trip, from anywhere.
			for i := 0; i < r.Len(); i++ {
				if err := n.GoTo(i); err != nil {
					t.Fatal(err)
				}
				settleNav(t, n, 1.0/60)
				here := n.Yaw()
				n.Next()
				n.Prev()
				settleNav(t, n, 1.0/60)
				if n.Focus() != i || !closeTo(n.Yaw(), here) {
					t.Errorf("%s: Next then Prev from screen %d landed on %d at %.6f°",
						lay.Arrangement, i, n.Focus(), deg(n.Yaw()))
				}
			}
		}
	}
}

// TestGoToTakesTheShortWayRound: from any screen to any other, never more than
// half a turn.
func TestGoToTakesTheShortWayRound(t *testing.T) {
	r := mustPlace(t, desk, spread(0))
	n := NewNav(r)
	for i := 0; i < r.Len(); i++ {
		for j := 0; j < r.Len(); j++ {
			if err := n.GoTo(i); err != nil {
				t.Fatal(err)
			}
			settleNav(t, n, 1.0/60)
			from := n.yaw
			if err := n.GoTo(j); err != nil {
				t.Fatal(err)
			}
			if d := math.Abs(n.target - from); d > math.Pi+1e-9 {
				t.Errorf("%d -> %d turned %.1f°, more than half a turn", i, j, deg(d))
			}
			settleNav(t, n, 1.0/60)
			if !closeTo(n.Yaw(), r.At(j).Centre) {
				t.Errorf("%d -> %d arrived at %.6f°, want %.6f°", i, j, deg(n.Yaw()), deg(r.At(j).Centre))
			}
		}
	}
}

// TestMotionIsMonotoneAndSettles is the sequence test for the easing: no
// overshoot, no oscillation, no creep, and it stops.
func TestMotionIsMonotoneAndSettles(t *testing.T) {
	r := mustPlace(t, desk, spread(2))
	n := NewNav(r)
	if err := n.GoTo(1); err != nil {
		t.Fatal(err)
	}
	target := n.target
	prev := n.yaw
	sign := math.Copysign(1, target-prev)
	frames := 0
	for n.Moving() {
		n.Advance(1.0 / 60)
		frames++
		d := n.yaw - prev
		if d*sign < 0 {
			t.Fatalf("frame %d: the yaw went backwards by %.9f", frames, d)
		}
		if (n.yaw-target)*sign > 0 {
			t.Fatalf("frame %d: overshot the target by %.9f", frames, n.yaw-target)
		}
		prev = n.yaw
		if frames > 10000 {
			t.Fatal("never settled")
		}
	}
	if n.yaw != target {
		t.Errorf("settled at %.17g, want exactly %.17g", n.yaw, target)
	}
	// A keypress must be over quickly. This is a 115° turn.
	if frames > 72 {
		t.Errorf("took %d frames at 60Hz to settle; that is not a keypress, that is a journey", frames)
	}
	// Advancing again once settled does nothing at all.
	n.Advance(1.0 / 60)
	if n.yaw != target || n.Moving() {
		t.Error("a settled Nav moved again")
	}
	// And it must not CREEP: once the motion is visually over, it has to end,
	// not drift the last fraction of a degree for another second.
	n2 := NewNav(r)
	if err := n2.GoTo(1); err != nil {
		t.Fatal(err)
	}
	near, total := 0, 0
	for n2.Moving() {
		n2.Advance(1.0 / 60)
		total++
		if near == 0 && math.Abs(n2.target-n2.yaw) < rad(1) {
			near = total
		}
	}
	if tailFrames := total - near; tailFrames > 15 {
		t.Errorf("the last degree took %d frames of the %d; that is a creep, not an arrival",
			tailFrames, total)
	}

	// Time does not run backwards, and a zero-length frame is not motion.
	n.target = target + 1
	n.Advance(0)
	n.Advance(-1)
	if n.yaw != target {
		t.Error("a non-positive dt moved the yaw")
	}
}

// TestMotionIsFrameRateIndependent: the same elapsed time must arrive at the
// same place, whether it was spent in one frame or in sixteen. A per-frame
// fraction — the obvious implementation — fails this, so the test compares
// against that too.
func TestMotionIsFrameRateIndependent(t *testing.T) {
	r := mustPlace(t, desk, spread(2))
	const elapsed = 0.05

	one := NewNav(r)
	if err := one.GoTo(1); err != nil {
		t.Fatal(err)
	}
	one.Advance(elapsed)

	many := NewNav(r)
	if err := many.GoTo(1); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		many.Advance(elapsed / 16)
	}
	if math.Abs(one.yaw-many.yaw) > 1e-12 {
		t.Errorf("one %gs step landed at %.12f, sixteen at %.12f", elapsed, one.yaw, many.yaw)
	}

	// The naive alternative: a fixed fraction per frame. Sixteen small frames
	// would then travel far further than one big one, and the ribbon would turn
	// at a speed that depends on how busy the machine is.
	naive := func(steps int) float64 {
		y := one.r.At(0).Centre
		tg := one.target
		for i := 0; i < steps; i++ {
			y += (tg - y) * 0.25
		}
		return y
	}
	if math.Abs(naive(1)-naive(16)) < 1e-6 {
		t.Error("the naive per-frame fraction agrees with itself; this test no longer discriminates")
	}
}

func TestTauZeroTeleports(t *testing.T) {
	r := mustPlace(t, desk, spread(2))
	n := NewNav(r)
	n.Tau = 0
	if err := n.GoTo(2); err != nil {
		t.Fatal(err)
	}
	n.Advance(1.0 / 60)
	if n.Moving() || !closeTo(n.Yaw(), r.At(2).Centre) {
		t.Errorf("Tau=0 did not arrive at once: at %.6f°, moving=%v", deg(n.Yaw()), n.Moving())
	}
	n.Tau = -1
	if err := n.GoTo(0); err != nil {
		t.Fatal(err)
	}
	n.Advance(1.0 / 60)
	if n.Moving() {
		t.Error("a negative Tau did not arrive at once either")
	}
}

// TestSetYawIsWhereATrackerWouldBeWired: an arbitrary continuous angle, taken as
// given, without disturbing the focus.
func TestSetYawIsWhereATrackerWouldBeWired(t *testing.T) {
	r := mustPlace(t, desk, spread(2))
	n := NewNav(r)
	if err := n.GoTo(2); err != nil {
		t.Fatal(err)
	}
	for _, yaw := range []float64{0, 1.5, -2.5, 40 * fullCircle, rad(359.5)} {
		n.SetYaw(yaw)
		if n.Moving() {
			t.Errorf("SetYaw(%v) left the ribbon turning", yaw)
		}
		if got := n.Yaw(); !closeTo(got, wrap(yaw)) {
			t.Errorf("SetYaw(%v) then Yaw() = %v, want %v", yaw, got, wrap(yaw))
		}
		if n.Focus() != 2 {
			t.Error("SetYaw changed the focus; it must not")
		}
	}
	// Simulating a tracker: a stream of small deltas must not accumulate a jump
	// as it crosses the seam.
	n.SetYaw(rad(359))
	prev := n.Yaw()
	for i := 0; i < 60; i++ {
		n.SetYaw(n.yaw + rad(0.5))
		if d := math.Abs(wrapSigned(n.Yaw() - prev)); d > rad(0.5)+1e-9 {
			t.Fatalf("step %d across the seam moved %.4f°, want 0.5°", i, deg(d))
		}
		prev = n.Yaw()
	}
}

// TestOrientationMatchesTheEquirectangularConvention is this package's
// equivalent of pose's TestYawMustBeAppliedLast: a composition across two
// packages, where each half is self-consistent and the join can still be
// mirrored.
//
// A ribbon longitude grows to the RIGHT; a pose yaw grows to the LEFT. So the
// quaternion Nav hands the renderer has to carry the opposite sign, and the test
// closes the loop the only way that proves it — rotate forward by the
// orientation, ask projection where that direction lands in an equirectangular
// panorama, and check it is the middle.
func TestOrientationMatchesTheEquirectangularConvention(t *testing.T) {
	r := mustPlace(t, desk, spread(3))
	n := NewNav(r)
	forward := pose.Vec3{Z: -1}
	sphere := projection.Sphere360
	for _, yaw := range []float64{0, rad(30), rad(90), rad(179), rad(181), rad(270), rad(359)} {
		n.SetYaw(yaw)
		dir := n.Orientation().Rotate(forward)
		// Longitude of that direction, read with projection's own convention:
		// u = 0.5 at longitude 0, growing to the right over 360°.
		u, v, ok := sphere.Sample(dir)
		if !ok {
			t.Fatalf("yaw %.1f°: the view direction is not on the sphere", deg(yaw))
		}
		if !closeTo(v, 0.5) {
			t.Errorf("yaw %.1f°: v = %v, want the horizon at 0.5", deg(yaw), v)
		}
		gotLon := wrap(rad((u - 0.5) * 360))
		if !closeTo(wrapSigned(gotLon-yaw), 0) {
			t.Errorf("yaw %.1f° looks at longitude %.3f°: the sign of the yaw is inverted",
				deg(yaw), deg(gotLon))
		}
	}
	// And the decisive claim, stated on its own: the pose yaw is the NEGATIVE of
	// the ribbon longitude. If it were the same sign, the whole ribbon would be
	// mirrored — which looks entirely plausible in a still frame, and sends the
	// screens the wrong way the moment the viewer scrolls.
	n.SetYaw(rad(30))
	if e := n.Orientation().EulerZXY(); !closeTo(e.Yaw, -30) {
		t.Errorf("longitude 30° gives a pose yaw of %.3f°, want -30°", e.Yaw)
	}
	if closeTo(n.Orientation().EulerZXY().Yaw, 30) {
		t.Error("the pose yaw came out with the ribbon's sign; the ribbon would be mirrored")
	}
}

// TestTurningRightMovesScreensLeft: the other half of the sign, stated as the
// viewer sees it rather than as a quaternion.
func TestTurningRightMovesScreensLeft(t *testing.T) {
	r := mustPlace(t, desk, spread(3))
	n := NewNav(r)
	n.SetYaw(r.At(1).Centre)
	before := r.Bearing(1, n.Yaw())
	n.SetYaw(n.Yaw() + rad(5)) // the viewer turns to their right
	after := r.Bearing(1, n.Yaw())
	if after >= before {
		t.Errorf("turning right moved screen 1 from %.2f° to %.2f°; it must move left",
			deg(before), deg(after))
	}
	if !closeTo(after, -rad(5)) {
		t.Errorf("after turning 5° right, screen 1 is at %.3f°, want -5°", deg(after))
	}
}
