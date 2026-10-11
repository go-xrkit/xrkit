// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package headflow

import (
	"image"
	"math"
)

// ColumnsPerDegree is how far the picture slides for one degree of turn, on a
// VITURE Beast's camera.
//
// ⭐ MEASURED, NOT LOOKED UP, AND IT CALIBRATES ITSELF. A full turn ends where
// it began -- the person checks that by eye against a landmark -- so the travel
// accumulated over one IS 360 degrees expressed in columns, with no protractor
// and no vendor number to trust. Three turns, two of them at half the speed of
// the other, gave 9628, 9332 and 9640 columns: agreement within 3%.
//
// The horizontal field of view it implies, 1920/26.5 ≈ 72 degrees, is the free
// control on the whole method. It was not supplied, it came out -- and a broken
// calculation would have produced something absurd instead.
//
// ⛔ A FOURTH TURN READ 10848 AND IS EXCLUDED. It had the tidiest frame-to-frame
// agreement of the four, and reasoning from it gave a calibration 13% wrong and
// a repeatability five times too pessimistic. Three concordant measurements
// decide; the tidiest one does not. (The person confirmed they may have gone
// past their landmark on it, which would account for exactly that excess.)
const ColumnsPerDegree = 26.5

// ReferenceWidth is the frame width that calibration was taken at.
//
// ⛔⛔ IT HAS TO BE NAMED, BECAUSE THE CALIBRATION IS NOT A PROPERTY OF THE
// LENS. [ColumnsPerDegree] counts COLUMNS, and columns are a property of the
// sensor READOUT: the same camera read out at 640 puts the same field of view
// into a third as many of them, so the same physical turn slides a third as
// far. A yaw computed from columns alone, on a 640-wide frame, comes out THREE
// TIMES TOO SMALL — with no error and no loss of confidence. A plausible
// number, silently wrong, which is the worst kind.
//
// It was not a live defect while one camera fed this through one path. It became
// one when go-xrkit/android negotiated 640x480 on the Beast's camera — 25 fps
// there against 5 at 1920, and latency is what head tracking is bounded by —
// while the macOS path kept feeding 1920. Both end up in this Tracker.
const ReferenceWidth = 1920

// HorizontalFOV is the camera's horizontal field of view, in degrees.
//
// ⭐ IT IS A RESTATEMENT OF THE MEASUREMENT ABOVE AND NOT A NEW NUMBER. The
// three turns gave columns; dividing by the width they were counted at gives
// degrees, which is what the lens actually has and what does not change with
// readout. 1920/26.5 is about 72.45 degrees — the figure [ColumnsPerDegree]'s
// own documentation already derived as the free control on the whole method.
const HorizontalFOV = float64(ReferenceWidth) / ColumnsPerDegree

// ColumnsPerDegreeAt is how far the picture slides for one degree of turn, in a
// frame of the given width.
//
// It returns 0 for a width of 0 rather than an infinity: such a frame is refused
// upstream by [Profile], and the arithmetic must not be the thing that catches
// it — dividing by zero here gives a yaw of ±Inf, which an application will
// render without complaint.
func ColumnsPerDegreeAt(width int) float64 {
	if width <= 0 {
		return 0
	}
	return float64(width) / HorizontalFOV
}

// MinConfidence is where a match stops being worth believing.
//
// ⛔ IT IS A REFUSAL, NOT A FILTER. Below this there is nothing wrong with the
// arithmetic -- the picture simply has nothing in it to match, which is what a
// dark room or a blank wall looks like. A tracker that quietly returned its last
// value, or zero, would be indistinguishable from one that was working. Saying
// so is what lets an application tell somebody "it is too dark to follow your
// head" instead of drifting silently.
const MinConfidence = 0.3

// A Tracker turns a stream of frames into how far the view has turned.
//
// It is not safe for concurrent use: feed it from one goroutine.
type Tracker struct {
	prev []float64
	// yaw is radians since the last Recenter, positive to the right.
	yaw float64
	// conf is the confidence of the most recent frame.
	conf float64
	// tracking is false once a frame was refused, until one is accepted.
	tracking bool
	// unusable counts frames refused for want of confidence, so an application
	// can tell a moment's blur from a room that has gone dark.
	unusable int
	// reacquire is set after a refusal: the next frame becomes the new
	// reference and reports no motion of its own.
	//
	// ⛔⛔ BECAUSE THE MOTION DURING THE BLINDNESS IS UNKNOWABLE. The reference
	// left behind is the picture that could not be read, so matching against it
	// would make a number out of nothing; and matching the first good frame
	// against the last good one BEFORE the gap would measure a jump of unknown
	// duration as though it were a single frame. Neither is a measurement.
	// Standing one frame down and saying so is the only honest option, and it
	// costs about 40 milliseconds.
	reacquire bool
}

// Feed offers a frame and reports the yaw after it, in radians, and whether the
// frame could be used at all.
//
// ⛔ A REFUSED FRAME DOES NOT MOVE THE YAW, and it does not reset it either. The
// view is wherever it was; this simply could not see it move. An application
// should keep showing the last position and say that tracking is lost, rather
// than treat "no information" as "no motion" -- they look the same in a single
// number and mean opposite things.
func (t *Tracker) Feed(im *image.RGBA) (yaw float64, ok bool) {
	cur := Profile(im)
	if len(cur) == 0 {
		return t.yaw, false
	}
	// ⛔⛔ A FRAME IS JUDGED ON ITSELF BEFORE IT IS JUDGED AGAINST ANOTHER. An
	// earlier version only ever asked how well two frames MATCHED, so the path
	// that re-establishes a reference happily accepted a blank one -- and a room
	// that stayed dark reported Unusable of ZERO, which is precisely the reading
	// an application would take for healthy tracking. A picture with no
	// structure cannot be a reference for anything.
	if spreadOf(cur) <= 0 {
		t.conf = 0
		t.tracking = false
		t.unusable++
		t.reacquire = true
		return t.yaw, false
	}
	if t.prev == nil || t.reacquire {
		// Establishing a reference, which moves nothing. The picture can be read
		// again -- that was just checked -- but it still has nothing to be
		// compared against.
		t.prev, t.reacquire, t.unusable = cur, false, 0
		return t.yaw, false
	}
	bins, conf := Shift(t.prev, cur)
	t.conf = conf
	t.prev = cur
	if conf < MinConfidence {
		t.tracking = false
		t.unusable++
		t.reacquire = true
		return t.yaw, false
	}
	t.tracking = true
	t.unusable = 0
	// ⭐ THE SIGN. A picture that slides RIGHT means the view turned LEFT, which
	// is why this subtracts. Getting it backwards is invisible in any test that
	// only checks magnitudes, so it has one of its own.
	//
	// ⛔ AND THE SCALE IS THIS FRAME'S, NOT THE CALIBRATION FRAME'S. bins*BinWidth
	// is columns of the frame just fed; see [ReferenceWidth] for why using the
	// measured constant directly made a 640-wide stream read three times too
	// small.
	// ⚠ AND IT IS NOT GUARDED HERE, deliberately. A zero width would divide to
	// an infinity, but [Profile] returned bins above, so the frame has columns:
	// a branch no input can reach is dead code, which this package's coverage
	// gate finds and which has been the right answer twice before. The guard
	// lives in [ColumnsPerDegreeAt], where a caller may pass anything.
	t.yaw -= float64(bins*BinWidth) / ColumnsPerDegreeAt(im.Bounds().Dx()) * math.Pi / 180
	return t.yaw, true
}

// Yaw is how far the view has turned since the last [Tracker.Recenter], in
// radians, positive to the right.
func (t *Tracker) Yaw() float64 { return t.yaw }

// Confidence is how well the last frame matched, from 0 to 1.
func (t *Tracker) Confidence() float64 { return t.conf }

// Tracking reports whether the last frame could be used.
func (t *Tracker) Tracking() bool { return t.tracking }

// Unusable is how many frames in a row were refused. One or two is a blur; a
// steady count is a room with nothing to see in it.
func (t *Tracker) Unusable() int { return t.unusable }

// Recenter makes the current view the origin.
//
// ⭐ IT IS THE ANSWER TO DRIFT, AND IT IS CHEAP. Error accumulates only while
// the view is moving -- stationary, this was measured accumulating exactly
// nothing over five minutes -- so an application that recentres whenever it
// knows the true heading, such as when a gaze settles on a screen whose position
// it knows, never lets the error run further than a single journey.
func (t *Tracker) Recenter() { t.yaw = 0 }

// SetYaw moves the origin so that the view reads yaw right now. It is Recenter
// with somewhere other than zero to land on.
func (t *Tracker) SetYaw(yaw float64) { t.yaw = yaw }
