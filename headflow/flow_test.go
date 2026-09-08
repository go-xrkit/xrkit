// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package headflow

import (
	"image"
	"math"
	"testing"
)

// TestShiftRecoversAKnownSlide.
//
// ⛔ THE RUN THAT PROMPTED THIS reported a peak of exactly zero over 249 frames
// while a head was turning, and nothing in the program could say whether that
// was a still picture, a dead camera or a broken search. This answers the last
// of the three on its own, with no camera and no person.
func TestShiftRecoversAKnownSlide(t *testing.T) {
	// Structure at several scales, so a wrong answer cannot be rescued by the
	// accidental self-similarity a pure sine would have.
	base := make([]float64, 480)
	for i := range base {
		x := float64(i)
		base[i] = 1000 + 300*math.Sin(x/7) + 120*math.Sin(x/31+1) + 40*math.Sin(x/3+2)
	}
	for _, want := range []int{0, 1, -1, 7, -7, 40, -40, 63, -63} {
		got, conf := Shift(base, slide(base, want))
		if got != want {
			t.Errorf("a slide of %+d was read as %+d", want, got)
		}
		if conf <= 0 {
			t.Errorf("a slide of %+d was found at confidence %.2f", want, conf)
		}
	}
}

// TestABlankPictureIsRefusedRatherThanAnswered.
//
// ⛔⛔ THE ANSWER THAT WOULD DO REAL HARM. A picture with no structure -- a dark
// room, a blank wall -- matches equally well at every offset. An earlier search
// kept whichever it tried first and reported a slide of -64: a violent turn,
// from a camera that could see nothing. It must come back as no motion AND no
// confidence, so an application can say "too dark to follow your head" instead
// of scrolling itself across the room.
func TestABlankPictureIsRefusedRatherThanAnswered(t *testing.T) {
	flat := make([]float64, 480)
	for i := range flat {
		flat[i] = 500
	}
	got, conf := Shift(flat, flat)
	if got != 0 {
		t.Errorf("a blank picture was read as a slide of %+d", got)
	}
	if conf > 0.01 {
		t.Errorf("a blank picture matched at confidence %.2f, so nothing "+
			"downstream can tell it from a tracked one", conf)
	}
}

// TestAnUnreachableSlideIsGivenAwayByItsConfidence.
//
// ⛔ NOT BY THE BOUND. A slide of MaxShift+20 does NOT come back as MaxShift --
// it was measured coming back as 58 for a true 84, an interior value no counter
// could tell from a real reading. The residual is what gives it away.
//
// ⛔⛔ AND NEITHER A PERIODIC PROFILE NOR A WRAPPING SLIDE CAN TEST IT: the first
// attempt used sin(i/7), which repeats about every 44 bins, so a slide of 84
// genuinely matched at 40 and the test failed on its own construction.
func TestAnUnreachableSlideIsGivenAwayByItsConfidence(t *testing.T) {
	base := noise(480)
	if got, conf := Shift(base, slideNoWrap(base, 30)); got != 30 || conf < 0.5 {
		t.Errorf("a reachable slide of +30 was read as %+d at confidence %.2f", got, conf)
	}
	got, conf := Shift(base, slideNoWrap(base, MaxShift+20))
	if conf > 0.5 {
		t.Errorf("an unreachable slide was read as %+d at confidence %.2f: a "+
			"sample this cannot measure is claiming to be a measurement", got, conf)
	}
}

// TestAProfileOfARealPictureStillFindsTheSlide guards [RowStep] and the
// unweighted channel sum, which are both speed bought with accuracy.
//
// ⛔⛔ TWO EARLIER VERSIONS OF THIS TEST GUARDED NOTHING, and a sabotage that
// sampled ONE row out of 540 passed both:
//
//  1. The image gave every row nearly the same columns, so one row sufficed.
//  2. The moved image was built by COPYING pixels from the first, so the grain
//     travelled with the structure, matched perfectly, and became extra signal.
//     The test got easier the noisier it was made.
//
// A camera does the opposite: the scene translates, the grain is drawn afresh
// every frame. Both pictures here therefore share only the column structure and
// each gets its own independent grain -- which is what makes averaging rows
// necessary, and what an over-eager RowStep would destroy.
func TestAProfileOfARealPictureStillFindsTheSlide(t *testing.T) {
	const w, h = 1920, 1080
	cols := noise(w)
	paint := func(shiftBins, seed int) *image.RGBA {
		im := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				base := 0
				if sx := x - shiftBins*BinWidth; sx >= 0 && sx < w {
					base = int(cols[sx]) % 50
				}
				v := byte((base + int(hash2(x*3+seed, y*5+seed)%200)) % 256)
				i := im.PixOffset(x, y)
				im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = v, v, v, 0xff
			}
		}
		return im
	}
	src := Profile(paint(0, 1))
	for _, want := range []int{0, 3, -3, 12, -12, 40} {
		got, conf := Shift(src, Profile(paint(want, 2)))
		if got != want {
			t.Errorf("a picture slid by %+d bins was read as %+d (confidence %.2f)", want, got, conf)
		}
		if conf < 0.3 {
			t.Errorf("a slide of %+d was found but only at confidence %.2f", want, conf)
		}
	}
}

func TestProfileRefusesAnEmptyImage(t *testing.T) {
	if got := Profile(image.NewRGBA(image.Rect(0, 0, 0, 0))); got != nil {
		t.Errorf("an empty image profiled to %v", got)
	}
}

// noise is a profile that does not repeat, from a fixed sequence so the test is
// the same on every run.
func noise(n int) []float64 {
	out := make([]float64, n)
	x := uint32(12345)
	for i := range out {
		x = x*1664525 + 1013904223
		out[i] = 500 + float64(x>>20)
	}
	return out
}

// hash2 is a deterministic per-pixel value with no correlation between
// neighbours.
func hash2(x, y int) uint32 {
	h := uint32(x)*2654435761 + uint32(y)*2246822519
	h ^= h >> 13
	h *= 3266489917
	h ^= h >> 16
	return h
}

// slide moves p by n, wrapping.
func slide(p []float64, n int) []float64 {
	out := make([]float64, len(p))
	for i := range p {
		out[i] = p[((i-n)%len(p)+len(p))%len(p)]
	}
	return out
}

// slideNoWrap moves p by n and leaves the vacated end empty, so no part of the
// profile can line up against a different part of itself.
func slideNoWrap(p []float64, n int) []float64 {
	out := make([]float64, len(p))
	for i := range out {
		if j := i - n; j >= 0 && j < len(p) {
			out[i] = p[j]
		}
	}
	return out
}

// TestTheSearchStartsAtZeroAndWorksOutwards.
//
// ⛔⛔ THIS IS TESTED DIRECTLY BECAUSE NOTHING ELSE TESTS IT. Restoring the old
// order -- sweeping from -MaxShift upwards -- was sabotaged into the package and
// EVERY OTHER TEST STILL PASSED, including the one about blank pictures, which
// exits through a shortcut before the order can matter. The property that keeps
// a featureless room from reading as a violent turn had no guard at all.
//
// The rule: when several offsets score alike, the one kept must be the smallest
// movement. That holds only if zero is tried first and the search expands from
// it, because the loop keeps its first winner among equals.
func TestTheSearchStartsAtZeroAndWorksOutwards(t *testing.T) {
	order := searchOrder()
	if len(order) != 2*MaxShift+1 {
		t.Fatalf("the search covers %d offsets, want %d", len(order), 2*MaxShift+1)
	}
	if order[0] != 0 {
		t.Errorf("the search starts at %+d, not 0: a tie would fall to that "+
			"offset, and on a picture with no structure EVERY offset ties", order[0])
	}
	seen := make(map[int]bool, len(order))
	last := 0
	for i, s := range order {
		if seen[s] {
			t.Fatalf("offset %+d appears twice, at position %d", s, i)
		}
		seen[s] = true
		if abs(s) < last {
			t.Fatalf("offset %+d at position %d is nearer zero than the %+d before "+
				"it: the search must never turn back inwards", s, i, last)
		}
		last = abs(s)
	}
	for s := -MaxShift; s <= MaxShift; s++ {
		if !seen[s] {
			t.Errorf("offset %+d is never tried", s)
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
