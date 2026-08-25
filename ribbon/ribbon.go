// Package ribbon arranges captured screens on a band around the viewer, and
// answers, for a given yaw, where each screen's pixels belong in the panorama
// buffer.
//
// The pipeline this serves is decided by a measurement.
// [github.com/go-xrkit/xrkit/warp.Build] costs 56.5 ms for the glasses'
// 3840x1080 two-eye output; [github.com/go-xrkit/xrkit/warp.Map.Apply] costs
// 2.8 ms. So the lookup table is built ONCE, at startup, and nothing the viewer
// does may force a rebuild — a 16.6 ms frame has no room for one.
//
// That is affordable because of a single fact about equirectangular sources: a
// yaw rotation is exactly a horizontal SHIFT of the panorama. So the viewer's
// orientation is never applied to the warp. The map is built for
// [github.com/go-xrkit/xrkit/pose.Identity], the panorama buffer is a fixed
// window of longitude and latitude centred on straight ahead, and the yaw is
// applied where it is free — when the screens are composited in, each at
// longitude MINUS yaw. Turning the ribbon costs nothing but a different set of
// destination columns.
//
// The screens sit on a CYLINDER centred on the viewer, not on flat planes. On a
// cylinder the horizontal mapping is exactly linear in longitude, which is what
// lets a run of destination columns read a run of source columns at a constant
// step; a flat screen would need a tangent per column, and the panorama it
// landed in would then need resampling twice. The vertical mapping is still
// non-linear — height on the cylinder is the tangent of latitude — so it is a
// per-row table. It does not depend on yaw, so it is computed once too.
//
// Angles: fields whose names end in Deg are in DEGREES, the unit a human
// configures in. Everything else — yaw, longitude, half-spans — is in RADIANS,
// modulo 2π. Longitude increases to the RIGHT, matching the equirectangular
// convention of [github.com/go-xrkit/xrkit/projection.Projection.Sample], which
// is the opposite sign from a [github.com/go-xrkit/xrkit/pose.Euler] yaw;
// [Nav.Orientation] is the single place that conversion happens.
package ribbon

import (
	"errors"
	"fmt"
	"math"
)

// fullCircle is one turn in radians. Named, because "2 * math.Pi" appearing in
// wrapping arithmetic is exactly where a factor of two goes missing unnoticed.
const fullCircle = 2 * math.Pi

// The errors this package returns. They are sentinels because a caller that
// hands a display list to [Place] has something useful to do about each one —
// drop the screen, widen the ribbon, lower the density — and matching on a
// message is not a way to tell them apart.
var (
	ErrNoScreens    = errors.New("ribbon: no screens to place")
	ErrScreenSize   = errors.New("ribbon: screen size must be positive")
	ErrDensity      = errors.New("ribbon: angular density must be positive")
	ErrGap          = errors.New("ribbon: gap must not be negative")
	ErrFullWidth    = errors.New("ribbon: fullscreen width must be within (0, 360]")
	ErrArrangement  = errors.New("ribbon: unknown arrangement")
	ErrMode         = errors.New("ribbon: unknown mode")
	ErrCrowded      = errors.New("ribbon: screens do not fit in 360°")
	ErrIndex        = errors.New("ribbon: screen index out of range")
	ErrPanoSize     = errors.New("ribbon: panorama size must be positive")
	ErrPanoKind     = errors.New("ribbon: panorama must be equirectangular")
	ErrPanoSpan     = errors.New("ribbon: panorama span must be within (0, 360] x (0, 180]")
	ErrPanoTooShort = errors.New("ribbon: panorama does not reach the band")
)

// Screen is one captured display, before it is placed.
type Screen struct {
	// ID is an opaque label carried through to the [Placed] screen so the
	// application can find its way back to the capture it came from. Nothing
	// here interprets it; the index on the ribbon is the identity.
	ID string
	// W and H are the captured framebuffer's size in pixels. They fix the
	// screen's shape on the ribbon: the aspect ratio decides how much of the
	// circle it takes.
	W, H int
}

// Aspect is the screen's width divided by its height. A screen with no pixels
// has no shape, and reports 0 rather than dividing by zero — [Place] refuses it
// before it can reach the geometry.
func (s Screen) Aspect() float64 {
	if s.H <= 0 {
		return 0
	}
	return float64(s.W) / float64(s.H)
}

// Arrangement is how the screens share the circle.
type Arrangement int

// The two arrangements.
const (
	// Packed puts the screens shoulder to shoulder with the configured gap, and
	// centres the whole block on longitude 0 — straight ahead. The rest of the
	// circle is empty. This is what two or three screens want: a desk, not a
	// planetarium.
	Packed Arrangement = iota
	// Spread shares the leftover arc equally between the gaps, so the screens
	// fill the full 360°. The configured gap becomes a minimum. This is what
	// makes stepping from the last screen to the first a short hop forwards
	// rather than a long journey back.
	Spread
)

// String names the arrangement.
func (a Arrangement) String() string {
	switch a {
	case Packed:
		return "packed"
	case Spread:
		return "spread"
	}
	return fmt.Sprintf("Arrangement(%d)", int(a))
}

// Layout is how the ribbon is built. It is all configuration, all in degrees.
type Layout struct {
	// DensityDeg is the ribbon's angular density: the arc, in degrees, given to
	// one screen-width of a square screen. A wider screen gets proportionally
	// more, so a 16:9 panel takes 16/9 of it.
	//
	// Read the other way round, this is what makes the ribbon a ribbon: every
	// screen ends up the same HEIGHT on the cylinder, whatever its resolution or
	// shape, and only the widths differ. It is the knob that trades how big the
	// screens look against how many fit round the circle.
	DensityDeg float64
	// GapDeg is the arc left empty between neighbours. Under [Spread] it is a
	// minimum, since the leftover arc is shared out on top of it.
	GapDeg float64
	// FullWidthDeg is the arc a screen is given when it is promoted to fill the
	// view. It is part of the layout rather than a per-call argument because the
	// vertical table it implies is precomputed, like the ribbon's own.
	FullWidthDeg float64
	// Arrangement is how the screens share the circle.
	Arrangement Arrangement
}

// check rejects a layout that cannot produce a ribbon. It is separate from
// [Place] so that each refusal is one line with one reason, rather than a
// preamble that has to be read past to find the geometry.
func (l Layout) check() error {
	if l.DensityDeg <= 0 {
		return fmt.Errorf("%w: %g°", ErrDensity, l.DensityDeg)
	}
	if l.GapDeg < 0 {
		return fmt.Errorf("%w: %g°", ErrGap, l.GapDeg)
	}
	if l.FullWidthDeg <= 0 || l.FullWidthDeg > 360 {
		return fmt.Errorf("%w: %g°", ErrFullWidth, l.FullWidthDeg)
	}
	if l.Arrangement != Packed && l.Arrangement != Spread {
		return fmt.Errorf("%w: %s", ErrArrangement, l.Arrangement)
	}
	return nil
}

// Placed is a screen with its position on the ribbon.
type Placed struct {
	Screen
	// Centre is the longitude of the screen's middle, in radians, in [0, 2π).
	Centre float64
	// HalfSpan is half the screen's angular width, in radians. Half rather than
	// whole because every test that matters — does this screen reach that
	// longitude, do these two overlap — is written against the half.
	HalfSpan float64
}

// Span is the screen's full angular width in radians.
func (p Placed) Span() float64 { return 2 * p.HalfSpan }

// Ribbon is a fixed arrangement of screens around the viewer.
//
// It is immutable once placed: the yaw lives in [Nav], and the per-frame answer
// in [Compositor]. Nothing about a Ribbon changes as the viewer turns, which is
// what lets the vertical tables be computed once.
type Ribbon struct {
	lay     Layout
	screens []Placed
	// halfH is half the band's height on the unit cylinder — the same for every
	// screen, which is what "ribbon" means here.
	halfH float64
	// gap is the arc actually left between neighbours, in radians. Under Spread
	// it is wider than the configured minimum.
	gap float64
}

// Place arranges screens around the viewer, in the order given.
//
// The order given is the order round the circle, and the arrangement is
// symmetric about longitude 0, so appending a screen never reorders the others:
// it shifts them, which the viewer sees as the ribbon settling, and does not
// scramble which screen is to the left of which.
func Place(screens []Screen, lay Layout) (*Ribbon, error) {
	if err := lay.check(); err != nil {
		return nil, err
	}
	if len(screens) == 0 {
		return nil, ErrNoScreens
	}
	density := rad(lay.DensityDeg)
	gap := rad(lay.GapDeg)

	spans := make([]float64, len(screens))
	total := gap * float64(len(screens))
	for i, s := range screens {
		if s.W <= 0 || s.H <= 0 {
			return nil, fmt.Errorf("%w: screen %d is %dx%d", ErrScreenSize, i, s.W, s.H)
		}
		// Same height on the cylinder for everyone; the aspect ratio decides the
		// width. That is what keeps the pixels square: an arc of α radians on the
		// unit cylinder is α long, so a screen α wide and α/aspect tall has the
		// same shape as its framebuffer.
		spans[i] = density * s.Aspect()
		total += spans[i]
	}
	// One gap per screen, not one per pair: the extra gap is the one across the
	// seam, and counting it here is what makes the last screen and the first
	// keep their distance when the block wraps all the way round.
	if total > fullCircle {
		return nil, fmt.Errorf("%w: %.1f° of screens and gaps", ErrCrowded, deg(total))
	}
	if lay.Arrangement == Spread {
		gap += (fullCircle - total) / float64(len(screens))
		total = fullCircle
	}

	r := &Ribbon{lay: lay, screens: make([]Placed, len(screens)), halfH: density / 2, gap: gap}
	// Lay the strip out as a run of (half-gap, screen, half-gap) cells and centre
	// it on straight ahead. Centring rather than starting at 0 is what makes a
	// single screen land in front of the viewer and a symmetric set stay
	// symmetric, in both arrangements, with one rule.
	at := -total / 2
	for i, s := range screens {
		at += gap / 2
		r.screens[i] = Placed{Screen: s, Centre: wrap(at + spans[i]/2), HalfSpan: spans[i] / 2}
		at += spans[i] + gap/2
	}
	return r, nil
}

// Len is how many screens are on the ribbon.
func (r *Ribbon) Len() int { return len(r.screens) }

// At returns the i'th screen. It indexes, so an out-of-range i panics, like a
// slice: the index came from [Ribbon.Len] or from [Nav.Focus], and a caller that
// invented one has a bug rather than a condition to handle.
func (r *Ribbon) At(i int) Placed { return r.screens[i] }

// Layout returns the layout the ribbon was placed with.
func (r *Ribbon) Layout() Layout { return r.lay }

// Gap is the arc actually left between neighbours, in radians. Under [Spread]
// this is wider than the configured minimum, and it is the honest number to
// report to a user wondering why the screens are further apart than they asked.
func (r *Ribbon) Gap() float64 { return r.gap }

// BandHalfAngle is how far above the horizon the band reaches, in radians.
//
// It is an arc-tangent, not half the density: the band has a constant HEIGHT on
// the cylinder, and height maps to latitude through a tangent. A panorama window
// whose vertical span is narrower than twice this clips the screens.
func (r *Ribbon) BandHalfAngle() float64 { return math.Atan(r.halfH) }

// Bearing is the signed angle from yaw to screen i, taking the short way round:
// in [-π, π), positive to the right.
func (r *Ribbon) Bearing(i int, yaw float64) float64 {
	return wrapSigned(r.screens[i].Centre - yaw)
}

// Visible appends the indices of the screens intersecting an arc of spanDeg
// degrees centred on yaw, in ribbon order, and returns the extended slice.
//
// Passing a slice with spare capacity — the same one every frame — is what keeps
// this free of allocation. A screen only PARTLY inside the arc counts, including
// one straddling the seam: the caller has to clip it, not skip it.
func (r *Ribbon) Visible(yaw, spanDeg float64, dst []int) []int {
	half := rad(spanDeg) / 2
	for i := range r.screens {
		// Interval overlap on a circle, done on the signed difference so the seam
		// is not a special case: a screen centred at 355° and a viewer at 5° are
		// 10° apart here, not 350°.
		if math.Abs(wrapSigned(r.screens[i].Centre-yaw)) <= half+r.screens[i].HalfSpan {
			dst = append(dst, i)
		}
	}
	return dst
}

// Nearest is the screen the viewer is most nearly facing. Ties go to the lower
// index, so the answer is stable rather than flickering between two screens the
// viewer is exactly between.
func (r *Ribbon) Nearest(yaw float64) int {
	best, bestD := 0, math.Inf(1)
	for i := range r.screens {
		if d := math.Abs(wrapSigned(r.screens[i].Centre - yaw)); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// wrap folds an angle into [0, 2π).
//
// The final guard is not paranoia. For a tiny negative angle, Mod returns that
// same tiny negative, and adding 2π rounds to exactly 2π — which is outside the
// interval this function promises, and would put a screen's centre at a
// longitude that compares as greater than every other.
func wrap(a float64) float64 {
	a = math.Mod(a, fullCircle)
	if a < 0 {
		a += fullCircle
	}
	if a >= fullCircle {
		return 0
	}
	return a
}

// wrapSigned folds an angle into [-π, π), the form in which "how far, and which
// way" is one number and the seam is not a special case.
func wrapSigned(a float64) float64 {
	a = wrap(a)
	if a >= math.Pi {
		a -= fullCircle
	}
	return a
}

// shortest is the signed angle from one longitude to another, the short way
// round. dir breaks the tie at exactly half a turn, where the two ways round are
// the same length: +1 takes the forward one, -1 the backward one.
//
// The tie is worth handling. Without it, two screens exactly opposite each other
// would both be reached by turning the same way, so "next" and "previous" would
// spin the viewer in the same direction — and a full lap of the ribbon would not
// come back to where it started.
func shortest(from, to float64, dir int) float64 {
	d := wrapSigned(to - from)
	if d == -math.Pi && dir >= 0 {
		return math.Pi
	}
	return d
}

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }
