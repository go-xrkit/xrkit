// Package warp turns a projection into a lookup table, so that reshaping a video
// frame for one eye costs a copy rather than a computation.
//
// The arithmetic in [github.com/go-xrkit/xrkit/projection] is per-pixel
// trigonometry: fine for reasoning about, far too slow for four million pixels
// sixty times a second. But it only depends on the viewer's ORIENTATION, and
// when that is fixed — no head tracking, a screen anchored in front of the
// viewer — the answer for every output pixel is the same on every frame. So it
// is computed once into a table of source offsets, and each frame becomes a
// gather.
//
// A Map is tied to the geometry it was built for. Change the orientation, the
// output size, the projection or the source dimensions and it must be rebuilt.
package warp

import (
	"github.com/go-xrkit/xrkit/pose"
	"github.com/go-xrkit/xrkit/projection"
	"github.com/go-xrkit/xrkit/stereo"
)

// outside marks an output pixel with nothing behind it. Stored in the table
// rather than tested for separately, so the inner loop has one branch and no
// arithmetic.
const outside = -1

// Map is a precomputed sampling table for one eye.
type Map struct {
	// W and H are the output size in pixels.
	W, H int
	// off[y*W+x] is the index, in PIXELS, into the source frame for that output
	// pixel, or [outside].
	off []int32
}

// Size reports the output dimensions the map was built for.
func (m *Map) Size() (w, h int) { return m.W, m.H }

// Source describes the frame a Map samples from: the whole decoded frame, plus
// which part of it belongs to this eye.
type Source struct {
	// Width and Height are the WHOLE frame's dimensions.
	Width, Height int
	// Stride is the frame's row length in PIXELS (not bytes).
	Stride int
	// Eye is the sub-rectangle holding this eye's image, from
	// [stereo.Format.EyeRect].
	Eye stereo.Rect
}

// Build computes the table for one eye.
//
// vp is the output viewport, p the source geometry, q the viewer's fixed
// orientation, and src where this eye's pixels live. Nearest-neighbour sampling:
// the output of an immersive viewer is already a magnification of the source, so
// the visible artefact is the magnification itself, not the interpolation.
func Build(vp projection.Viewport, p projection.Projection, q pose.Quat, src Source) *Map {
	m := &Map{W: vp.Width, H: vp.Height}
	if vp.Width <= 0 || vp.Height <= 0 {
		return m
	}
	m.off = make([]int32, vp.Width*vp.Height)
	i := 0
	for y := 0; y < vp.Height; y++ {
		for x := 0; x < vp.Width; x++ {
			u, v, ok := p.Sample(vp.LookRay(q, x, y))
			if !ok {
				m.off[i] = outside
				i++
				continue
			}
			m.off[i] = sampleIndex(u, v, src)
			i++
		}
	}
	return m
}

// sampleIndex turns normalised eye-image coordinates into an index into the
// frame, or [outside].
//
// It is a function of its own rather than three lines inside Build because its
// guards are otherwise untestable: u or v of exactly 1 lands one pixel past the
// edge, and a viewport ray never produces exactly 1 -- rays go through pixel
// centres. The guard still has to be right, because a caller can supply an eye
// rectangle that does not fit the frame, and reading one row past the end of a
// decoder buffer is not a bug that announces itself.
func sampleIndex(u, v float64, src Source) int32 {
	ew, eh := src.Eye.W, src.Eye.H
	if ew <= 0 || eh <= 0 {
		return outside
	}
	// u,v are normalised within the EYE image, so they scale by the eye
	// rectangle and are offset by its origin: that is what makes one map work for
	// side-by-side, over-under and mono alike.
	sx := src.Eye.X + int(u*float64(ew))
	sy := src.Eye.Y + int(v*float64(eh))
	if sx >= src.Eye.X+ew {
		sx = src.Eye.X + ew - 1
	}
	if sy >= src.Eye.Y+eh {
		sy = src.Eye.Y + eh - 1
	}
	if sx < 0 || sy < 0 || sx >= src.Width || sy >= src.Height {
		return outside
	}
	return int32(sy*src.Stride + sx)
}

// Covered reports how many output pixels have source behind them. It is the
// honest measure of how much of the view a piece of content actually fills — a
// 180-degree video in a 100-degree view covers everything, the same video looked
// at sideways covers nothing.
func (m *Map) Covered() int {
	n := 0
	for _, o := range m.off {
		if o != outside {
			n++
		}
	}
	return n
}

// Apply gathers src into dst through the table.
//
// Both are 32-bit pixels — the format is whatever the decoder produced and this
// does not care, since it moves whole pixels and never looks inside one. Strides
// are in PIXELS. Output pixels with no source are set to bg.
//
// dst may be a sub-window of a larger buffer: dstOff is where this eye's top-left
// corner goes, which is what lets two eyes be written into one framebuffer
// side by side.
func (m *Map) Apply(src []uint32, dst []uint32, dstStride, dstOff int, bg uint32) {
	if m.off == nil {
		return
	}
	n := int32(len(src))
	for y := 0; y < m.H; y++ {
		row := m.off[y*m.W : (y+1)*m.W]
		out := dst[dstOff+y*dstStride:]
		out = out[:m.W]
		for x, o := range row {
			// One branch, no arithmetic: the bounds test also covers a table
			// built against a differently-sized frame, which would otherwise
			// read out of the buffer.
			if o < 0 || o >= n {
				out[x] = bg
				continue
			}
			out[x] = src[o]
		}
	}
}
