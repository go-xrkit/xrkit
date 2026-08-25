package ribbon

import (
	"testing"

	"github.com/go-xrkit/xrkit/projection"
)

// benchRibbon is a plausible desk: six captured displays of assorted shapes,
// spread round the full circle.
func benchRibbon(b *testing.B) *Ribbon {
	b.Helper()
	r, err := Place([]Screen{
		{ID: "laptop", W: 2560, H: 1600},
		{ID: "main", W: 3840, H: 2160},
		{ID: "side", W: 1920, H: 1080},
		{ID: "portrait", W: 1080, H: 1920},
		{ID: "old", W: 1280, H: 1024},
		{ID: "wide", W: 3440, H: 1440},
	}, Layout{DensityDeg: 22, GapDeg: 3, FullWidthDeg: 110, Arrangement: Spread})
	if err != nil {
		b.Fatal(err)
	}
	return r
}

// benchPano is the panorama the warp map reads: the field of view plus a margin,
// at a resolution that does not throw away the glasses' pixels.
var benchPano = Pano{W: 2048, H: 1024, Window: projection.Projection{
	Kind: projection.Equirect, HSpanDeg: 140, VSpanDeg: 70}}

// BenchmarkFrame measures the whole per-frame question — which screens are in
// view, and where every one of their pixels goes — at a yaw that moves, so the
// answer is never the one already in cache.
//
// It has to be cheap in an absolute sense, not merely cheaper than what it
// replaces: it runs inside the same 16.6 ms as a 2.8 ms warp gather and a screen
// capture. Anything in the microseconds is free; anything that allocates is not,
// because sixty collections a second is a stutter rather than a cost.
func BenchmarkFrame(b *testing.B) {
	r := benchRibbon(b)
	c, err := NewCompositor(r, benchPano)
	if err != nil {
		b.Fatal(err)
	}
	blits := c.Frame(nil, 0)
	yaw := 0.0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		yaw += 0.017 // a degree a frame, so no two frames are alike
		blits = c.Frame(blits[:0], yaw)
	}
	b.StopTimer()
	perFrame := b.Elapsed().Seconds() / float64(b.N)
	b.ReportMetric(perFrame*1e6, "µs/frame")
	b.ReportMetric(perFrame/0.0166*100, "%of-budget")
	b.ReportMetric(float64(len(blits)), "blits")
}

// BenchmarkFrameAndBlit puts that number in context by doing the work the blits
// describe: clearing the panorama and painting every visible screen into it.
// This is the pass the ribbon actually costs the application, and the query is
// the part of it this package is responsible for.
func BenchmarkFrameAndBlit(b *testing.B) {
	r := benchRibbon(b)
	c, err := NewCompositor(r, benchPano)
	if err != nil {
		b.Fatal(err)
	}
	src := make([][]uint32, r.Len())
	for i := range src {
		src[i] = make([]uint32, r.At(i).W*r.At(i).H)
	}
	pano := make([]uint32, benchPano.W*benchPano.H)
	blits := c.Frame(nil, 0)
	yaw := 0.0
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		yaw += 0.017
		blits = c.Frame(blits[:0], yaw)
		clear(pano)
		for _, bl := range blits {
			sw := r.At(bl.Screen).W
			for j := 0; j < bl.Dst.H; j++ {
				out := pano[(bl.Dst.Y+j)*benchPano.W+bl.Dst.X:][:bl.Dst.W]
				in := src[bl.Screen][int(bl.SrcY[j])*sw:][:sw]
				sx, dx := bl.SrcX, bl.SrcXStep
				for i := range out {
					out[i] = in[sx>>fracBits]
					sx += dx
				}
			}
		}
	}
	b.StopTimer()
	perFrame := b.Elapsed().Seconds() / float64(b.N)
	b.ReportMetric(perFrame*1000, "ms/frame")
	b.ReportMetric(perFrame/0.0166*100, "%of-budget")
}

// BenchmarkVisible measures the cheaper question on its own, for an application
// that only wants to know which screens to capture this frame.
func BenchmarkVisible(b *testing.B) {
	r := benchRibbon(b)
	vis := make([]int, 0, r.Len())
	yaw := 0.0
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		yaw += 0.017
		vis = r.Visible(yaw, 140, vis[:0])
	}
}

// BenchmarkAdvance measures the motion step, which runs once a frame whether or
// not anything is moving.
func BenchmarkAdvance(b *testing.B) {
	n := NewNav(benchRibbon(b))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !n.Moving() {
			if err := n.GoTo(i % n.r.Len()); err != nil {
				b.Fatal(err)
			}
		}
		n.Advance(1.0 / 60)
	}
}

// BenchmarkNewCompositor measures the startup cost — the tangent per row per
// screen that buys the per-frame work being horizontal only. It happens once,
// and again if the viewer resizes the panorama, so it has to stay well under the
// 56.5 ms the warp map itself takes to build.
func BenchmarkNewCompositor(b *testing.B) {
	r := benchRibbon(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewCompositor(r, benchPano); err != nil {
			b.Fatal(err)
		}
	}
}

// benchGallery is the same desk seen all at once, in the view of a pair of
// glasses that shows 51.57° of it.
func benchGallery(b *testing.B) *Gallery {
	b.Helper()
	c, err := NewCompositor(benchRibbon(b), benchPano)
	if err != nil {
		b.Fatal(err)
	}
	g, err := NewGallery(c, View{HDeg: 51.57, VDeg: 28.38})
	if err != nil {
		b.Fatal(err)
	}
	return g
}

// BenchmarkGalleryFrame measures a gallery frame against the ribbon frame it
// replaces. It has less to do — the cells never move, so there is no yaw to
// resolve and no screen to leave out — and it must cost the same kind of number,
// because it is drawn in the same 16.6 ms by the same renderer.
//
// It has no yaw argument, so the answer is the same every frame. That is not the
// benchmark cheating: it is the reason a gallery is cheap, and an application
// that cached the slice would pay nothing at all.
func BenchmarkGalleryFrame(b *testing.B) {
	g := benchGallery(b)
	blits := g.Frame(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blits = g.Frame(blits[:0])
	}
	b.StopTimer()
	perFrame := b.Elapsed().Seconds() / float64(b.N)
	b.ReportMetric(perFrame*1e6, "µs/frame")
	b.ReportMetric(perFrame/0.0166*100, "%of-budget")
	b.ReportMetric(float64(len(blits)), "blits")
}

// BenchmarkGalleryMove measures the selection step, which happens once per
// keypress and is here so that a later implementation reaching for a map or a
// search cannot do it unnoticed.
func BenchmarkGalleryMove(b *testing.B) {
	g := benchGallery(b)
	dirs := []Direction{Right, Down, Left, Up}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := g.Move(dirs[i%len(dirs)]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNewGallery measures the startup cost: choosing the grid shape, which
// is quadratic in the number of screens, and a tangent per row per cell. It
// happens once, beside the 56.5 ms the warp map takes to build.
func BenchmarkNewGallery(b *testing.B) {
	c, err := NewCompositor(benchRibbon(b), benchPano)
	if err != nil {
		b.Fatal(err)
	}
	v := View{HDeg: 51.57, VDeg: 28.38}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewGallery(c, v); err != nil {
			b.Fatal(err)
		}
	}
}
