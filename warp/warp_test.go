package warp

import (
	"testing"

	"github.com/go-xrkit/xrkit/pose"
	"github.com/go-xrkit/xrkit/projection"
	"github.com/go-xrkit/xrkit/stereo"
)

// fullFrame describes a whole frame as one eye's image, i.e. monoscopic.
func fullFrame(w, h int) Source {
	return Source{Width: w, Height: h, Stride: w, Eye: stereo.Rect{W: w, H: h}}
}

func TestMapSize(t *testing.T) {
	m := Build(projection.Viewport{Width: 7, Height: 5, FOVyDeg: 90},
		projection.Sphere360, pose.Identity(), fullFrame(16, 8))
	if w, h := m.Size(); w != 7 || h != 5 {
		t.Errorf("Size() = %dx%d, want 7x5", w, h)
	}
	if len(m.off) != 35 {
		t.Errorf("table has %d entries, want 35", len(m.off))
	}
}

func TestBuildRefusesDegenerateViewport(t *testing.T) {
	for _, vp := range []projection.Viewport{
		{Width: 0, Height: 4, FOVyDeg: 90},
		{Width: 4, Height: 0, FOVyDeg: 90},
		{Width: -1, Height: -1, FOVyDeg: 90},
	} {
		m := Build(vp, projection.Sphere360, pose.Identity(), fullFrame(8, 8))
		if m.off != nil {
			t.Errorf("%+v produced a table", vp)
		}
		if m.Covered() != 0 {
			t.Errorf("%+v reports coverage", vp)
		}
		// Apply on an empty map must be a no-op, not a panic.
		m.Apply(make([]uint32, 4), make([]uint32, 4), 2, 0, 0)
	}
}

// TestBuildCentrePixelSamplesTheCentre pins the one correspondence that must
// hold for any projection with the viewer facing forward.
func TestBuildCentrePixelSamplesTheCentre(t *testing.T) {
	const srcW, srcH = 64, 32
	// Odd output size so there is an exact centre pixel.
	vp := projection.Viewport{Width: 9, Height: 9, FOVyDeg: 90}
	m := Build(vp, projection.Sphere360, pose.Identity(), fullFrame(srcW, srcH))
	off := m.off[4*9+4]
	wantX, wantY := srcW/2, srcH/2
	if got := int(off); got != wantY*srcW+wantX {
		t.Errorf("centre pixel samples offset %d (x=%d,y=%d), want %d (x=%d,y=%d)",
			got, got%srcW, got/srcW, wantY*srcW+wantX, wantX, wantY)
	}
}

// TestBuildRespectsTheEyeRect is what makes one map work for every layout: the
// left eye must never sample a pixel from the right eye's half.
func TestBuildRespectsTheEyeRect(t *testing.T) {
	const srcW, srcH = 64, 32
	f := stereo.Format{Layout: stereo.SideBySide}
	vp := projection.Viewport{Width: 8, Height: 8, FOVyDeg: 90}

	for _, eye := range []stereo.Eye{stereo.Left, stereo.Right} {
		r := f.EyeRect(eye, srcW, srcH)
		m := Build(vp, projection.Hemisphere180, pose.Identity(), Source{
			Width: srcW, Height: srcH, Stride: srcW, Eye: r,
		})
		if m.Covered() == 0 {
			t.Fatalf("%v eye: nothing covered", eye)
		}
		for i, o := range m.off {
			if o == outside {
				continue
			}
			x, y := int(o)%srcW, int(o)/srcW
			if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
				t.Fatalf("%v eye: output pixel %d samples (%d,%d), outside its rect %+v",
					eye, i, x, y, r)
				return
			}
		}
	}
}

func TestBuildMarksWhatHasNoSource(t *testing.T) {
	// A narrow flat screen inside a wide view: the edges of the view have no
	// content, and must be marked rather than clamped to the screen's border.
	vp := projection.Viewport{Width: 64, Height: 64, FOVyDeg: 120}
	m := Build(vp, projection.Projection{Kind: projection.Flat, HSpanDeg: 20, VSpanDeg: 20},
		pose.Identity(), fullFrame(32, 32))
	cov := m.Covered()
	if cov == 0 {
		t.Fatal("a screen straight ahead covered nothing")
	}
	if cov == vp.Width*vp.Height {
		t.Fatal("a 20-degree screen covered a 120-degree view entirely")
	}
	// The very corners of a 120-degree view cannot see a 20-degree screen.
	if m.off[0] != outside {
		t.Error("the top-left corner has source behind it; it should not")
	}
}

func TestBuildDegenerateEyeRectIsAllOutside(t *testing.T) {
	m := Build(projection.Viewport{Width: 4, Height: 4, FOVyDeg: 90},
		projection.Sphere360, pose.Identity(),
		Source{Width: 8, Height: 8, Stride: 8, Eye: stereo.Rect{W: 0, H: 0}})
	if m.Covered() != 0 {
		t.Errorf("an empty eye rect covered %d pixels", m.Covered())
	}
}

// TestBuildNeverPointsOutsideTheFrame is the safety property Apply relies on.
func TestBuildNeverPointsOutsideTheFrame(t *testing.T) {
	for _, p := range []projection.Projection{
		projection.Sphere360, projection.Hemisphere180,
		projection.Fisheye180, projection.Screen,
	} {
		for _, srcDim := range [][2]int{{64, 32}, {33, 17}, {2, 2}, {1, 1}} {
			w, h := srcDim[0], srcDim[1]
			m := Build(projection.Viewport{Width: 20, Height: 20, FOVyDeg: 100},
				p, pose.FromEulerZXY(pose.Euler{Yaw: 25, Pitch: -10}), fullFrame(w, h))
			for i, o := range m.off {
				if o == outside {
					continue
				}
				if o < 0 || int(o) >= w*h {
					t.Fatalf("%v %dx%d: pixel %d points at offset %d, frame holds %d",
						p.Kind, w, h, i, o, w*h)
				}
			}
		}
	}
}

func TestBuildFollowsOrientation(t *testing.T) {
	const srcW, srcH = 64, 32
	vp := projection.Viewport{Width: 9, Height: 9, FOVyDeg: 90}
	fwd := Build(vp, projection.Sphere360, pose.Identity(), fullFrame(srcW, srcH))
	// Yawing right must move the sampled column right in an equirect source.
	right := Build(vp, projection.Sphere360,
		pose.FromEulerZXY(pose.Euler{Yaw: -60}), fullFrame(srcW, srcH))
	cf := int(fwd.off[4*9+4]) % srcW
	cr := int(right.off[4*9+4]) % srcW
	if cr <= cf {
		t.Errorf("after yawing right the centre samples column %d, was %d; it should have moved right", cr, cf)
	}
}

func TestApply(t *testing.T) {
	// A 4x2 source with recognisable values.
	src := []uint32{
		10, 11, 12, 13,
		20, 21, 22, 23,
	}
	// A map that reverses the first row and marks the second as absent.
	m := &Map{W: 4, H: 2, off: []int32{
		3, 2, 1, 0,
		outside, outside, outside, outside,
	}}
	dst := make([]uint32, 4*2)
	m.Apply(src, dst, 4, 0, 0xdead)
	want := []uint32{13, 12, 11, 10, 0xdead, 0xdead, 0xdead, 0xdead}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst = %v, want %v", dst, want)
		}
	}
}

// TestApplyWritesIntoASubWindow is what lets two eyes share one framebuffer.
func TestApplyWritesIntoASubWindow(t *testing.T) {
	src := []uint32{1, 2, 3, 4}
	left := &Map{W: 2, H: 2, off: []int32{0, 1, 2, 3}}
	right := &Map{W: 2, H: 2, off: []int32{3, 2, 1, 0}}

	const stride = 4
	dst := make([]uint32, stride*2)
	left.Apply(src, dst, stride, 0, 0)
	right.Apply(src, dst, stride, 2, 0)

	want := []uint32{
		1, 2, 4, 3,
		3, 4, 2, 1,
	}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst = %v, want %v", dst, want)
		}
	}
}

// TestApplyGuardsAgainstAMismatchedFrame covers the case where a table built for
// one frame size is applied to a smaller one — which would otherwise read past
// the end of the decoder's buffer.
func TestApplyGuardsAgainstAMismatchedFrame(t *testing.T) {
	m := &Map{W: 2, H: 1, off: []int32{0, 999}}
	src := []uint32{7, 8}
	dst := make([]uint32, 2)
	m.Apply(src, dst, 2, 0, 0xbad)
	if dst[0] != 7 || dst[1] != 0xbad {
		t.Errorf("dst = %v, want [7 %d]", dst, 0xbad)
	}
}

func TestCovered(t *testing.T) {
	m := &Map{W: 3, H: 1, off: []int32{0, outside, 2}}
	if got := m.Covered(); got != 2 {
		t.Errorf("Covered() = %d, want 2", got)
	}
	if got := (&Map{}).Covered(); got != 0 {
		t.Errorf("Covered() of an empty map = %d, want 0", got)
	}
}

// TestSampleIndex covers the mapping and its guards directly. The clamps are
// unreachable through Build — a viewport ray goes through a pixel centre and so
// never yields exactly 1 — but they still have to be right, because a caller can
// hand over an eye rectangle that does not fit the frame.
func TestSampleIndex(t *testing.T) {
	// A 16x8 frame, the right half being this eye.
	src := Source{Width: 16, Height: 8, Stride: 16, Eye: stereo.Rect{X: 8, Y: 0, W: 8, H: 8}}

	for _, tc := range []struct {
		name string
		u, v float64
		want int32
	}{
		{"top-left of the eye", 0, 0, 0*16 + 8},
		{"centre of the eye", 0.5, 0.5, 4*16 + 12},
		// u=1 would land at x=16, one past the eye's last column: clamped to 15.
		{"u exactly 1 clamps to the last column", 1, 0, 0*16 + 15},
		{"v exactly 1 clamps to the last row", 0, 1, 7*16 + 8},
		{"both exactly 1", 1, 1, 7*16 + 15},
	} {
		if got := sampleIndex(tc.u, tc.v, src); got != tc.want {
			t.Errorf("%s: sampleIndex(%v,%v) = %d, want %d", tc.name, tc.u, tc.v, got, tc.want)
		}
	}

	// A degenerate eye rectangle has nothing to sample.
	for _, r := range []stereo.Rect{{W: 0, H: 8}, {W: 8, H: 0}, {W: -1, H: -1}} {
		if got := sampleIndex(0.5, 0.5, Source{Width: 16, Height: 8, Stride: 16, Eye: r}); got != outside {
			t.Errorf("eye rect %+v gave %d, want outside", r, got)
		}
	}

	// An eye rectangle that does not fit the frame must be refused, not read.
	tooFar := Source{Width: 16, Height: 8, Stride: 16, Eye: stereo.Rect{X: 12, Y: 0, W: 8, H: 8}}
	if got := sampleIndex(0.99, 0, tooFar); got != outside {
		t.Errorf("an eye rect running past the frame gave %d, want outside", got)
	}
	negative := Source{Width: 16, Height: 8, Stride: 16, Eye: stereo.Rect{X: -4, Y: 0, W: 2, H: 8}}
	if got := sampleIndex(0, 0, negative); got != outside {
		t.Errorf("an eye rect starting before the frame gave %d, want outside", got)
	}
	below := Source{Width: 16, Height: 8, Stride: 16, Eye: stereo.Rect{X: 0, Y: -4, W: 8, H: 2}}
	if got := sampleIndex(0, 0, below); got != outside {
		t.Errorf("an eye rect above the frame gave %d, want outside", got)
	}
	past := Source{Width: 16, Height: 8, Stride: 16, Eye: stereo.Rect{X: 0, Y: 6, W: 8, H: 8}}
	if got := sampleIndex(0, 0.99, past); got != outside {
		t.Errorf("an eye rect running below the frame gave %d, want outside", got)
	}
}
