// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package headflow

import "image"

// Defaults measured on a VITURE Beast's camera, 2026-09-07.
const (
	// BinWidth is how many columns are grouped before matching.
	//
	// ⭐ IT WIDENS THE REACH AND CUTS THE COST AT ONCE. The search steps in
	// BINS, so each step covers BinWidth columns: the same number of steps
	// reaches four times further while costing a quarter as much. A yaw moves
	// the whole picture together, so the detail inside a bin carries none of
	// the motion.
	BinWidth = 4

	// MaxShift bounds the search, in bins.
	//
	// ⛔ A BOUND THAT IS REACHED IS A BOUND THAT WAS TOO SMALL. An earlier
	// version searched 48 COLUMNS and reported a peak of exactly 48 while a
	// head was turning: the search hitting its own wall, not a measurement.
	// 64 bins is 256 columns, and a head sweeping fast was measured at 24.
	MaxShift = 64

	// RowStep is how many rows are skipped between the ones summed.
	//
	// ⛔ IT IS A MARGIN, NOT A TASTE. Sensor grain is independent between
	// frames, so what pulls the scene out of it is averaging rows -- and noise
	// falls only as the square root of how many are kept. Swept against a test
	// image whose grain is redrawn per frame, matching holds at 1, 4 and 8 and
	// FAILS at 16. Four sits two doublings below the failure.
	RowStep = 4
)

// Profile reduces a frame to one number per bin: the brightness of that slice
// of the picture, summed down a band through its middle.
//
// The middle band only, because the ceiling and the floor of a room are the
// parts most likely to be blank, and a blank contributes nothing but noise.
//
// ⛔ THE CHANNELS ARE SUMMED UNWEIGHTED, not turned into a luminance. The same
// transform is applied to both frames and only the SHIFT between them survives
// it, so the weights would be three multiplies per pixel bought for nothing.
func Profile(im *image.RGBA) []float64 {
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil
	}
	y0, y1 := b.Min.Y+h/4, b.Min.Y+3*h/4
	out := make([]float64, (w+BinWidth-1)/BinWidth)
	for y := y0; y < y1; y += RowStep {
		row := im.PixOffset(b.Min.X, y)
		for x := 0; x < w; x++ {
			i := row + x*4
			out[x/BinWidth] += float64(im.Pix[i]) + float64(im.Pix[i+1]) + float64(im.Pix[i+2])
		}
	}
	return out
}

// Shift reports how far b has slid relative to a, in BINS, and how much to
// believe it.
//
// The confidence runs from 0 to 1: how much better the winning offset fits than
// the profile's own natural variation. Below about 0.3 there is nothing to
// believe -- see [Tracker] for what to do about that.
//
// ⛔⛔ IT SEARCHES OUTWARD FROM ZERO, AND THAT IS NOT A DETAIL. Sweeping from
// -MaxShift upward keeps whichever offset was tried FIRST when several score
// alike -- and on a picture with no structure, every offset scores alike. A dark
// room was therefore once read as a slide of -64: a violent turn, from a camera
// that could see nothing, which is the most dangerous answer this can give.
// Trying 0, +1, -1, +2, -2 ... makes a tie fall to the smallest movement, so no
// information reads as no motion.
//
// ⛔ AND THE BOUND IS NOT A SATURATION SIGNAL. A slide of 84 bins against a
// 64-bin search does not come back as 64 -- it was measured coming back as 58,
// an interior value indistinguishable from a real reading. What separates them
// is the residual, which is what the confidence is made of.
func Shift(a, b []float64) (int, float64) {
	if len(a) == 0 || len(b) == 0 {
		return 0, 0
	}
	best, bestErr := 0, 0.0
	first := true
	for _, s := range searchOrder() {
		err, n := 0.0, 0
		for i := range a {
			j := i + s
			if j < 0 || j >= len(b) {
				continue
			}
			d := a[i] - b[j]
			if d < 0 {
				d = -d
			}
			err += d
			n++
		}
		if n == 0 {
			continue
		}
		err /= float64(n) // per bin, so different overlaps compare fairly
		if first || err < bestErr {
			best, bestErr, first = s, err, false
		}
	}
	spread := spreadOf(a)
	if spread <= 0 {
		return 0, 0
	}
	conf := 1 - bestErr/spread
	switch {
	case conf < 0:
		conf = 0
	case conf > 1:
		conf = 1
	}
	return best, conf
}

// searchOrder lists the offsets to try, nearest to zero first. The order is the
// whole point: see [Shift].
func searchOrder() []int {
	out := make([]int, 0, 2*MaxShift+1)
	out = append(out, 0)
	for s := 1; s <= MaxShift; s++ {
		out = append(out, s, -s)
	}
	return out
}

// spreadOf is the mean absolute deviation of a profile from its own mean: the
// error a match would leave if it lined nothing up at all.
func spreadOf(p []float64) float64 {
	if len(p) == 0 {
		return 0
	}
	var mean float64
	for _, v := range p {
		mean += v
	}
	mean /= float64(len(p))
	var sum float64
	for _, v := range p {
		d := v - mean
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return sum / float64(len(p))
}
