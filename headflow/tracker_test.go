// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package headflow

import (
	"image"
	"math"
	"testing"
)

// frame paints a picture whose column structure is shifted by shiftBins, with
// grain of its own so that matching has to average rows for it.
func frame(cols []float64, shiftBins, seed int) *image.RGBA {
	const w, h = 640, 360
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base := 0
			if sx := x - shiftBins*BinWidth; sx >= 0 && sx < w {
				base = int(cols[sx]) % 60
			}
			v := byte((base + int(hash2(x*3+seed, y*5+seed)%120)) % 256)
			i := im.PixOffset(x, y)
			im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = v, v, v, 0xff
		}
	}
	return im
}

// TestTheFirstFrameMovesNothing: there is nothing to compare it with, and a
// tracker that reported motion on it would start every session with a jump.
func TestTheFirstFrameMovesNothing(t *testing.T) {
	cols := noise(640)
	var tr Tracker
	yaw, ok := tr.Feed(frame(cols, 0, 1))
	if ok {
		t.Error("the first frame was reported as usable")
	}
	if yaw != 0 {
		t.Errorf("the first frame moved the yaw to %v", yaw)
	}
}

// TestAPictureSlidingRightMeansTheViewTurnedLeft.
//
// ⛔⛔ THE SIGN IS INVISIBLE TO EVERY TEST THAT CHECKS MAGNITUDES, which is why
// it gets one of its own. Turn your head right and the room sweeps LEFT across
// the picture; a tracker with this backwards feels exactly as responsive and
// sends the view the wrong way.
func TestAPictureSlidingRightMeansTheViewTurnedLeft(t *testing.T) {
	cols := noise(640)
	var tr Tracker
	tr.Feed(frame(cols, 0, 1))
	// The structure moves to the RIGHT in the picture: content that was at x is
	// now at x+8 bins. The camera therefore panned LEFT.
	yaw, ok := tr.Feed(frame(cols, 8, 2))
	if !ok {
		t.Fatal("a clear slide was refused")
	}
	if yaw >= 0 {
		t.Errorf("the picture slid right and the yaw went %+.4f rad: a view that "+
			"turns the wrong way is as wrong as one that does not turn", yaw)
	}
	want := -8 * BinWidth / ColumnsPerDegree * math.Pi / 180
	if math.Abs(yaw-want) > 1e-9 {
		t.Errorf("yaw %v, want %v", yaw, want)
	}
}

// TestARefusedFrameLeavesTheYawWhereItWas.
//
// ⛔ "NO INFORMATION" AND "NO MOTION" ARE THE SAME NUMBER AND OPPOSITE FACTS. A
// tracker that zeroed on a bad frame would snap the view home every time
// somebody blinked past a blank wall.
func TestARefusedFrameLeavesTheYawWhereItWas(t *testing.T) {
	cols := noise(640)
	var tr Tracker
	tr.Feed(frame(cols, 0, 1))
	moved, ok := tr.Feed(frame(cols, 6, 2))
	if !ok {
		t.Fatal("a clear slide was refused")
	}
	// A blank picture: nothing to match at any offset.
	blank := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for i := range blank.Pix {
		blank.Pix[i] = 128
	}
	yaw, ok := tr.Feed(blank)
	if ok {
		t.Error("a blank picture was accepted as a measurement")
	}
	if yaw != moved {
		t.Errorf("a refused frame moved the yaw from %v to %v", moved, yaw)
	}
	if tr.Tracking() {
		t.Error("Tracking stayed true through a frame that could not be used")
	}
	if tr.Unusable() != 1 {
		t.Errorf("Unusable is %d after one refusal", tr.Unusable())
	}
}

// TestUnusableCountsRunsAndNotTotals: one blurred frame is not a dark room, and
// an application needs to tell them apart to know what to say.
func TestUnusableCountsRunsAndNotTotals(t *testing.T) {
	cols := noise(640)
	blank := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for i := range blank.Pix {
		blank.Pix[i] = 128
	}
	var tr Tracker
	tr.Feed(frame(cols, 0, 1))
	tr.Feed(blank)
	tr.Feed(blank)
	if tr.Unusable() != 2 {
		t.Fatalf("Unusable is %d after two refusals", tr.Unusable())
	}
	// ⛔ THE FIRST GOOD FRAME BACK IS A REFERENCE, NOT A MEASUREMENT, and this
	// test originally demanded otherwise. Nobody knows how far the view moved
	// while it could not be seen: matching against the blank would make a number
	// out of nothing, and matching across the whole gap would call a jump of
	// unknown duration one frame's worth of motion. So it stands down for one
	// frame -- about 40ms -- and says so.
	if _, ok := tr.Feed(frame(cols, 3, 3)); ok {
		t.Error("the first frame after a blind spell reported motion it could not know")
	}
	if tr.Unusable() != 0 {
		t.Errorf("Unusable is %d once the picture is readable again", tr.Unusable())
	}
	if _, ok := tr.Feed(frame(cols, 6, 4)); !ok {
		t.Error("the SECOND frame back was refused, so recovery never completes")
	}
}

// TestRecenterMakesHereTheOrigin, which is what bounds drift to one journey.
func TestRecenterMakesHereTheOrigin(t *testing.T) {
	cols := noise(640)
	var tr Tracker
	tr.Feed(frame(cols, 0, 1))
	tr.Feed(frame(cols, 10, 2))
	if tr.Yaw() == 0 {
		t.Fatal("nothing moved, so there is nothing to recentre")
	}
	tr.Recenter()
	if tr.Yaw() != 0 {
		t.Errorf("after Recenter the yaw is %v", tr.Yaw())
	}
	tr.SetYaw(1.5)
	if tr.Yaw() != 1.5 {
		t.Errorf("after SetYaw(1.5) the yaw is %v", tr.Yaw())
	}
}

// TestTurningBackAndForthReturnsNearZero: the residual is drift, and on
// synthetic frames with no parallax there should be none at all.
func TestTurningBackAndForthReturnsNearZero(t *testing.T) {
	cols := noise(640)
	var tr Tracker
	tr.Feed(frame(cols, 0, 1))
	for seed, at := range []int{4, 8, 12, 8, 4, 0} {
		if _, ok := tr.Feed(frame(cols, at, seed+2)); !ok {
			t.Fatalf("the step to %d bins was refused", at)
		}
	}
	if math.Abs(tr.Yaw()) > 1e-9 {
		t.Errorf("after going out and back the yaw is %v, not zero", tr.Yaw())
	}
}
