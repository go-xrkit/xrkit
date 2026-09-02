package projection

import "math"

// FillScreen returns the flat virtual screen that FILLS a viewport: the largest
// screen showing a picture of the given aspect ratio whose whole picture still
// fits inside the view.
//
// This is what a player wants and what picking spans by hand does not give.
// [Screen] is a fixed 60 by 34 degrees, chosen as a comfortable television from
// the sofa, and it is wrong twice over for a film. It is the wrong SHAPE for
// anything but a 1.889:1 picture — a 16:9 film stretches 6% across it — and it
// is the wrong SIZE for a view that is not what it assumed: a 90-degree view
// leaves that screen occupying 11% of the glasses, which is what the picture
// was measured to cover before this existed.
//
// fovyDeg is the viewport's vertical field of view and viewAspect its output
// shape; together they say how much the eye sees. pictureAspect is the shape of
// the picture, width over height, and is preserved exactly — a screen that
// filled the view by stretching would fill it with the wrong picture.
//
// Note that the RESULT of filling does not depend on the field of view: the
// screen grows with the view, so the pixels come out the same whatever fovyDeg
// is. The angle still matters for saying truthfully how large the picture is,
// which is why it is asked for rather than assumed.
//
// ok is false for a field of view at or beyond a half turn, or for an aspect or
// an angle that is not positive and finite. There is no screen to describe
// then, and none is invented.
func FillScreen(fovyDeg, viewAspect, pictureAspect float64) (p Projection, ok bool) {
	if !positiveFinite(fovyDeg) || fovyDeg >= 180 ||
		!positiveFinite(viewAspect) || !positiveFinite(pictureAspect) {
		return Projection{}, false
	}
	// Half-extents at unit depth, which is the space the geometry samples in:
	// a screen twice as wide subtends nowhere near twice the angle, so the
	// fitting cannot be done in degrees.
	tv := math.Tan(rad(fovyDeg) / 2)
	th := tv * viewAspect
	// Grow the picture until it meets whichever edge it reaches first.
	pv := math.Min(tv, th/pictureAspect)
	ph := pv * pictureAspect
	return Projection{
		Kind:     Flat,
		HSpanDeg: 2 * deg(math.Atan(ph)),
		VSpanDeg: 2 * deg(math.Atan(pv)),
	}, true
}

// positiveFinite reports whether x is a number an angle or a ratio can be.
func positiveFinite(x float64) bool { return x > 0 && !math.IsInf(x, 1) }

// deg converts radians to degrees, the inverse of [rad].
func deg(r float64) float64 { return r * 180 / math.Pi }
