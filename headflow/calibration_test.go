// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package headflow

import (
	"image"
	"math"
	"testing"
)

// frameAt is [frame] with the size as an argument.
//
// ⛔ THE EXISTING HELPER HARDCODES 640, so every test in this package measured
// the tracker at one width and none of them could see a width dependence. That
// is what this file exists to look at.
func frameAt(w, h int, cols []float64, shiftBins, seed int) *image.RGBA {
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

// yawFor feeds one reference frame and one shifted frame at the given size, and
// returns the yaw the tracker reports.
func yawFor(t *testing.T, w, h, shiftBins int) float64 {
	t.Helper()
	cols := noise(w)
	var tr Tracker
	tr.Feed(frameAt(w, h, cols, 0, 1))
	yaw, ok := tr.Feed(frameAt(w, h, cols, shiftBins, 2))
	if !ok {
		t.Fatalf("a clear slide of %d bins at %dx%d was refused (confidence %.3f)",
			shiftBins, w, h, tr.Confidence())
	}
	return yaw
}

// TestTheSameTurnReadsTheSameAtAnyFrameWidth.
//
// ⛔⛔ THE CALIBRATION WAS MEASURED AT 1920 AND APPLIED TO WHATEVER ARRIVES.
// [ColumnsPerDegree] is 26.5 columns a degree, and its own documentation says
// where that came from: "1920/26.5 ≈ 72 degrees" of horizontal field. The field
// of view is a property of the LENS; the columns are a property of the SENSOR
// READOUT. A camera read out at 640 puts the same 72 degrees into a third as
// many columns, so the same physical turn slides a third as far — and a yaw
// computed from columns alone comes out THREE TIMES TOO SMALL, with no error
// and no loss of confidence. A plausible number, silently wrong.
//
// It matters now because go-xrkit/android negotiates 640x480 on the Beast's
// camera: 25 fps there against 5 at 1920, and latency is what head tracking is
// bounded by. The macOS path feeds 1920. Both end up in this Tracker.
//
// THE TEST IS THE SAME PHYSICAL ROTATION AT TWO WIDTHS: a shift of one tenth of
// the frame is one tenth of the field of view whatever the width, so the two
// yaws must agree.
func TestTheSameTurnReadsTheSameAtAnyFrameWidth(t *testing.T) {
	// A tenth of the width, in bins: 1920/10/4 = 48, 640/10/4 = 16. Both are
	// within MaxShift.
	wide := yawFor(t, 1920, 1080, 48)
	narrow := yawFor(t, 640, 480, 16)

	if math.Abs(wide-narrow) > 1e-6 {
		t.Fatalf("the same tenth-of-a-field turn reads %+.5f rad (%.2f°) at 1920 and "+
			"%+.5f rad (%.2f°) at 640 — a factor of %.2f. The calibration is in "+
			"COLUMNS and columns are not a property of the lens",
			wide, wide*180/math.Pi, narrow, narrow*180/math.Pi, wide/narrow)
	}

	// And it is the RIGHT size, not merely consistent: a tenth of a 72-degree
	// field is 7.2 degrees, and the picture slid right so the view turned left.
	want := -HorizontalFOV / 10 * math.Pi / 180
	if math.Abs(wide-want) > 1e-6 {
		t.Errorf("yaw %+.5f rad (%.2f°), want %+.5f rad (%.2f°): a tenth of the field",
			wide, wide*180/math.Pi, want, want*180/math.Pi)
	}
}

// TestTheFieldOfViewAgreesWithTheMeasuredCalibration.
//
// ⭐ THE 1920 BEHAVIOUR IS THE THING THAT WAS MEASURED — three turns against a
// landmark, 9628, 9332 and 9640 columns — and it must not move. The field of
// view is a RESTATEMENT of that measurement, not a new number: at the width it
// was taken on, the two must give the same columns a degree.
func TestTheFieldOfViewAgreesWithTheMeasuredCalibration(t *testing.T) {
	if got := ColumnsPerDegreeAt(ReferenceWidth); math.Abs(got-ColumnsPerDegree) > 1e-9 {
		t.Errorf("at the reference width the derivation gives %.6f columns a degree "+
			"and the measurement gave %.6f; the field of view is a restatement of "+
			"that measurement and may not disagree with it", got, ColumnsPerDegree)
	}
	// ⛔ AND THE DERIVED FIELD MUST BE A PLAUSIBLE LENS. This is the free
	// control the calibration's own documentation names: nothing supplied 72
	// degrees, it came out — and a broken derivation gives something absurd
	// rather than something subtly wrong.
	if HorizontalFOV < 40 || HorizontalFOV > 120 {
		t.Errorf("a horizontal field of %.2f degrees is not a camera lens", HorizontalFOV)
	}
}

// TestAWidthThatCannotBeCalibratedIsRefusedRatherThanDividedBy.
//
// A zero-width frame is already refused by Profile, which returns no bins — but
// the arithmetic must not be the thing that catches it, because dividing by a
// width gives +Inf and a yaw of infinity is a number an application will happily
// render.
func TestAWidthThatCannotBeCalibratedIsRefusedRatherThanDividedBy(t *testing.T) {
	var tr Tracker
	yaw, ok := tr.Feed(image.NewRGBA(image.Rect(0, 0, 0, 0)))
	if ok {
		t.Error("an empty frame was reported as usable")
	}
	if yaw != 0 || math.IsInf(yaw, 0) || math.IsNaN(yaw) {
		t.Errorf("an empty frame moved the yaw to %v", yaw)
	}
	if got := ColumnsPerDegreeAt(0); got != 0 {
		t.Errorf("ColumnsPerDegreeAt(0) = %v, want 0 rather than an infinity", got)
	}
}
