package ribbon

import (
	"fmt"
	"math"

	"github.com/go-xrkit/xrkit/projection"
	"github.com/go-xrkit/xrkit/stereo"
)

// fracBits is how many fraction bits the source-column stepper carries.
//
// 32 of them, in an int64, rather than the 16 a classic blitter uses. The step
// is rounded once, and a destination row can be four thousand columns wide, so
// a 16.16 step drifts by up to a twentieth of a source column across a row —
// enough to move a nearest-neighbour sample one pixel and to make a rendered
// test disagree with itself when the yaw changes by nothing that matters.
// At 32 bits the drift across the widest realistic row is under a millionth of a
// column, and there is nothing else to spend the register on.
const fracBits = 32

// tieBias is the nudge, in fixed-point units, that decides a destination column
// whose centre falls EXACTLY on the boundary between two source columns.
//
// Those ties are not a curiosity, they are the common case. The step is a ratio
// of small whole numbers whenever the panorama width and the screen's aspect
// ratio are related — 60° over 197 columns of a 16:9 screen steps by exactly
// 2880/197 — and then a whole column of the picture lands on a boundary at once.
// Rounding down, the stepper's accumulated deficit of a few times 2^-32 puts it
// a hair BELOW the boundary and it reads the column to the left, which is the
// one answer nobody would choose deliberately.
//
// So the stepper is biased up by 2^-20 of a column: enough to swallow any
// deficit the step can accumulate across a row, and far too small to reach a
// value that is genuinely below a boundary, since a rational step misses one by
// a multiple of 1/denominator and those denominators are in the hundreds.
const tieBias = 1 << (fracBits - 20)

// Pano is the equirectangular buffer the ribbon is composited into: a fixed
// window of longitude and latitude centred on wherever the viewer is looking.
//
// It is the SOURCE of the warp map, so Window is the same
// [projection.Projection] handed to warp.Build, and the buffer is that map's
// source frame. It does not have to cover 360°, and should not: it only has to
// cover the field of view plus enough margin that a screen entering the view is
// already in the buffer.
type Pano struct {
	// W and H are the buffer's size in pixels.
	W, H int
	// Window is the arc the buffer covers. It must be equirectangular; a flat or
	// fisheye window would not make yaw a horizontal shift, which is the whole
	// reason the warp map never has to be rebuilt.
	Window projection.Projection
}

// check rejects a panorama the compositing arithmetic cannot be trusted on.
func (p Pano) check() error {
	if p.W <= 0 || p.H <= 0 {
		return fmt.Errorf("%w: %dx%d", ErrPanoSize, p.W, p.H)
	}
	if p.Window.Kind != projection.Equirect {
		return fmt.Errorf("%w: %s", ErrPanoKind, p.Window.Kind)
	}
	if p.Window.HSpanDeg <= 0 || p.Window.HSpanDeg > 360 ||
		p.Window.VSpanDeg <= 0 || p.Window.VSpanDeg > 180 {
		return fmt.Errorf("%w: %g° x %g°", ErrPanoSpan, p.Window.HSpanDeg, p.Window.VSpanDeg)
	}
	return nil
}

// Blit is where one screen's pixels go in the panorama buffer, for one frame.
//
// The horizontal mapping is one linear stepper for the whole rectangle, not one
// per row: on a cylinder a screen's longitudes do not change with height, so
// every row of the destination reads the same run of source columns. Only the
// source ROW varies, and it varies through a tangent, so that is the table.
//
// The source column for the i'th destination column of Dst, and the source row
// for its j'th destination row, are
//
//	src = (SrcX + int64(i)*SrcXStep) >> 32   // == Column(i)
//	row = SrcY[j]
//
// and both are guaranteed to be inside the screen's framebuffer for every i in
// [0, Dst.W) and j in [0, Dst.H), so a blitter needs no clamping of its own.
//
// Rounding is nearest-neighbour and floored, the same as
// [github.com/go-xrkit/xrkit/warp]: a destination pixel belongs to the screen
// when its CENTRE falls inside the screen's arc, and it reads the source pixel
// its centre lands in. Sampling through centres rather than corners is what
// stops a half-pixel bias appearing as a seam where two screens meet.
type Blit struct {
	// Screen is the index of the screen on the ribbon. It is not unique within a
	// frame: a screen straddling the edge of a full-circle panorama window comes
	// back as two Blits, one against each edge.
	Screen int
	// Dst is the destination rectangle in the panorama buffer.
	Dst stereo.Rect
	// SrcX is the source column of Dst's first column, and SrcXStep the source
	// columns per destination column, both in 32.32 fixed point.
	SrcX, SrcXStep int64
	// SrcY is the source row for each destination row, indexed from Dst.Y. It
	// points into the compositor's precomputed table: it is valid for as long as
	// the compositor is, and must not be written to.
	SrcY []int32
}

// Column is the source column read by the i'th destination column of Dst.
func (b Blit) Column(i int) int { return int((b.SrcX + int64(i)*b.SrcXStep) >> fracBits) }

// vtab is a screen's vertical mapping into the panorama: the first destination
// row it covers, and the source row for each row from there on.
//
// It does not depend on the yaw — the ribbon only ever turns — so it is built
// once, and the per-frame work is horizontal only.
type vtab struct {
	y0   int
	rows []int32
}

// buildVTab maps panorama rows to source rows for a screen of srcH pixels whose
// half-height on the unit cylinder is halfH, in a window spanning vSpan radians
// of latitude over panoH rows.
func buildVTab(panoH int, vSpan, halfH float64, srcH int) vtab {
	v := vtab{}
	for y := 0; y < panoH; y++ {
		// Latitude of this row's centre, from the equirectangular convention
		// v = 0.5 - lat/vSpan. Through a pixel centre, so |lat| is strictly less
		// than vSpan/2 and the tangent below never reaches the pole.
		lat := vSpan * (0.5 - (float64(y)+0.5)/float64(panoH))
		// Height on the unit cylinder, and from there the position down the
		// screen. THIS is the non-linear step: equal angles are not equal
		// distances up a cylinder, and treating them as though they were makes
		// the screens look right in the middle and squashed at the edges.
		t := 0.5 - math.Tan(lat)/(2*halfH)
		if t < 0 {
			continue // above the screen's top edge
		}
		if t >= 1 {
			break // below its bottom edge, and rows only go further down
		}
		if len(v.rows) == 0 {
			v.y0 = y
		}
		v.rows = append(v.rows, int32(t*float64(srcH)))
	}
	return v
}

// Compositor answers the one question the renderer asks every frame: at this
// yaw, which screens are in the panorama window, and where do their pixels go.
//
// It owns the vertical tables, which is why it is a type rather than a function:
// they cost a tangent per row per screen to build and never change afterwards.
// A steady-state frame allocates nothing, provided the caller reuses the slice
// it passes in.
type Compositor struct {
	r    *Ribbon
	pano Pano
	// halfW is half the panorama window's horizontal span, in radians.
	halfW float64
	// fullHalfSpan is half the arc a promoted screen is given.
	fullHalfSpan float64
	// band and full are the vertical tables, per screen, for the ribbon and for
	// the fullscreen size.
	band, full []vtab
}

// NewCompositor precomputes the vertical mapping of every screen into pano.
func NewCompositor(r *Ribbon, pano Pano) (*Compositor, error) {
	if err := pano.check(); err != nil {
		return nil, err
	}
	c := &Compositor{
		r:            r,
		pano:         pano,
		halfW:        rad(pano.Window.HSpanDeg) / 2,
		fullHalfSpan: rad(r.lay.FullWidthDeg) / 2,
		band:         make([]vtab, r.Len()),
		full:         make([]vtab, r.Len()),
	}
	vSpan := rad(pano.Window.VSpanDeg)
	for i := range c.band {
		s := r.screens[i]
		c.band[i] = buildVTab(pano.H, vSpan, r.halfH, s.H)
		// A promoted screen keeps its shape, so its height on the cylinder
		// follows from the width it is given, exactly as on the ribbon.
		c.full[i] = buildVTab(pano.H, vSpan, c.fullHalfSpan/s.Aspect(), s.H)
		if len(c.band[i].rows) == 0 || len(c.full[i].rows) == 0 {
			// Not one row of the buffer falls on the screen. That is a
			// configuration mistake — a window too flat, or too few rows to
			// resolve the band — and it is worth refusing, because the frames it
			// produces are empty rather than wrong, which takes far longer to
			// diagnose than an error at startup.
			return nil, fmt.Errorf("%w: %d rows over %g° do not reach screen %d",
				ErrPanoTooShort, pano.H, pano.Window.VSpanDeg, i)
		}
	}
	return c, nil
}

// Pano returns the buffer geometry the compositor was built for.
func (c *Compositor) Pano() Pano { return c.pano }

// Frame appends the blits for one frame at the given yaw and returns the
// extended slice. Passing dst[:0] of the previous frame's slice reuses the
// storage, and the frame then allocates nothing at all.
//
// Screens outside the window contribute nothing; a screen partly inside is
// clipped to it. Blits come back in ribbon order.
func (c *Compositor) Frame(dst []Blit, yaw float64) []Blit {
	for i := range c.r.screens {
		p := &c.r.screens[i]
		dst = c.place(dst, i, wrapSigned(p.Centre-yaw), p.HalfSpan, &c.band[i], p.W)
	}
	return dst
}

// Fullscreen appends the blits that put one screen alone across the view,
// centred on where the viewer is looking and given Layout.FullWidthDeg of arc.
//
// It is the same geometry as a screen on the ribbon, only wider and always
// straight ahead — which is deliberate. Rendering a promoted screen through some
// other path would mean a second warp map, and building one costs 56.5 ms.
func (c *Compositor) Fullscreen(dst []Blit, screen int) ([]Blit, error) {
	if screen < 0 || screen >= c.r.Len() {
		return dst, fmt.Errorf("%w: %d", ErrIndex, screen)
	}
	return c.place(dst, screen, 0, c.fullHalfSpan, &c.full[screen], c.r.screens[screen].W), nil
}

// place emits the blits for one screen whose centre is rel radians from straight
// ahead and which is halfSpan wide either side of that.
//
// The alias loop is what makes the seam ordinary. A screen is not one interval
// on the number line but the same interval repeated every 2π, and a full-circle
// window has two of its edges in the same place — so a screen sitting on that
// edge appears against BOTH, as two blits, and the three candidates are tested
// rather than one being chosen by a case analysis that has to be got right.
func (c *Compositor) place(dst []Blit, idx int, rel, halfSpan float64, v *vtab, srcW int) []Blit {
	w := float64(c.pano.W)
	span := 2 * c.halfW
	for k := -1; k <= 1; k++ {
		l0 := rel + float64(k)*fullCircle - halfSpan
		l1 := l0 + 2*halfSpan
		// A destination column belongs to the screen when its centre, at
		// u = (x+0.5)/W, falls in the screen's arc: x >= u0*W - 0.5, and
		// x < u1*W - 0.5. Both ends therefore ceil, and the interval is
		// half-open, so two screens that meet exactly share no column.
		x0 := ceilInt((0.5+l0/span)*w - 0.5)
		x1 := ceilInt((0.5+l1/span)*w - 0.5)
		if x0 < 0 {
			x0 = 0
		}
		if x1 > c.pano.W {
			x1 = c.pano.W
		}
		if x1 <= x0 {
			continue // outside the window, or too narrow to contain a pixel centre
		}
		// Source column as a function of destination column: the longitude of
		// the column's centre, measured from the screen's left edge, in units of
		// the screen's width. Linear, because the screen is on a cylinder.
		step := (span / w) * float64(srcW) / (2 * halfSpan)
		first := ((span*(float64(x0)+0.5)/w - c.halfW) - l0) / (2 * halfSpan) * float64(srcW)
		sx, dx := stepper(first, step, x1-x0, srcW)
		dst = append(dst, Blit{
			Screen:   idx,
			Dst:      stereo.Rect{X: x0, Y: v.y0, W: x1 - x0, H: len(v.rows)},
			SrcX:     sx,
			SrcXStep: dx,
			SrcY:     v.rows,
		})
	}
	return dst
}

// stepper turns the real source mapping — start at column first, advance by step
// per destination column, n columns of them — into the fixed-point pair a
// blitter walks, and guarantees every column it produces is inside a source
// srcW columns wide.
//
// It is a function of its own, rather than four lines inside [Compositor.place],
// because its guards are otherwise unreachable and would go untested. The
// ribbon's own arithmetic leaves the last column a full DESTINATION column short
// of the screen's right edge, so nothing it produces can run past the end. That
// is an argument about the caller, and arguments about callers are how a read
// past the end of a captured framebuffer gets shipped: the guard is here, and it
// is tested by calling this directly with the arithmetic that would do it.
func stepper(first, step float64, n, srcW int) (x0, dx int64) {
	if step > float64(srcW) {
		// A run advancing a whole source per destination column would have to be
		// a screen squeezed below one column wide. Clamped anyway, because
		// converting a float that does not fit an int64 is implementation-defined
		// — it saturates on one of this repo's architectures and wraps to the
		// most negative integer on another.
		step = float64(srcW)
	}
	dx = int64(step * (1 << fracBits))
	x0 = int64(first*(1<<fracBits)) + tieBias
	if last := (x0 + int64(n-1)*dx) >> fracBits; last >= int64(srcW) {
		// Slide the whole run back by the columns it overruns by. Sliding beats
		// clamping the last column alone: a run that ends past the source is one
		// that was aimed too far all along.
		x0 -= (last - int64(srcW) + 1) << fracBits
	}
	if x0 < 0 {
		// Nothing sensible is left — the run was longer than the source it
		// reads. Standing still on the first column is wrong, and safe, and
		// unreachable from a placed ribbon.
		x0, dx = 0, 0
	}
	return x0, dx
}

// ceilInt is math.Ceil as an int. It exists so the column arithmetic reads as
// arithmetic rather than as three conversions.
func ceilInt(v float64) int { return int(math.Ceil(v)) }
