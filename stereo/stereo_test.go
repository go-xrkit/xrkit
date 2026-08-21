package stereo

import "testing"

func TestEyeString(t *testing.T) {
	for _, tc := range []struct {
		e    Eye
		want string
	}{{Left, "left"}, {Right, "right"}, {Eye(7), "Eye(7)"}} {
		if got := tc.e.String(); got != tc.want {
			t.Errorf("Eye(%d).String() = %q, want %q", int(tc.e), got, tc.want)
		}
	}
}

func TestEyeOther(t *testing.T) {
	if Left.Other() != Right || Right.Other() != Left {
		t.Error("Other() does not swap the eyes")
	}
	// Anything that is not Left is treated as Right, so Other is Left.
	if Eye(9).Other() != Left {
		t.Error("Other() of an unknown eye should be Left")
	}
}

func TestLayoutString(t *testing.T) {
	for _, tc := range []struct {
		l    Layout
		want string
	}{
		{Mono, "mono"},
		{SideBySide, "side-by-side"},
		{OverUnder, "over-under"},
		{Layout(9), "Layout(9)"},
	} {
		if got := tc.l.String(); got != tc.want {
			t.Errorf("Layout(%d).String() = %q, want %q", int(tc.l), got, tc.want)
		}
	}
}

func TestEyeRect(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    Format
		eye  Eye
		w, h int
		want Rect
	}{
		// Mono hands the whole frame to both eyes.
		{"mono left", Format{Layout: Mono}, Left, 1920, 1080, Rect{0, 0, 1920, 1080}},
		{"mono right", Format{Layout: Mono}, Right, 1920, 1080, Rect{0, 0, 1920, 1080}},

		// The case that matters here: the glasses' own 3D mode.
		{"sbs left of 3840x1080", Format{Layout: SideBySide}, Left, 3840, 1080, Rect{0, 0, 1920, 1080}},
		{"sbs right of 3840x1080", Format{Layout: SideBySide}, Right, 3840, 1080, Rect{1920, 0, 1920, 1080}},

		{"over-under top", Format{Layout: OverUnder}, Left, 1920, 2160, Rect{0, 0, 1920, 1080}},
		{"over-under bottom", Format{Layout: OverUnder}, Right, 1920, 2160, Rect{0, 1080, 1920, 1080}},

		// Swapped material: the left half holds the right eye.
		{"swapped sbs left", Format{Layout: SideBySide, Swapped: true}, Left, 3840, 1080, Rect{1920, 0, 1920, 1080}},
		{"swapped sbs right", Format{Layout: SideBySide, Swapped: true}, Right, 3840, 1080, Rect{0, 0, 1920, 1080}},
		{"swapped over-under left", Format{Layout: OverUnder, Swapped: true}, Left, 1920, 2160, Rect{0, 1080, 1920, 1080}},
		// Swapping a mono frame changes nothing, since there is one image.
		{"swapped mono", Format{Layout: Mono, Swapped: true}, Left, 640, 480, Rect{0, 0, 640, 480}},

		// An odd dimension cannot be halved: the split floors and the middle
		// line goes unread, rather than one eye borrowing a line of the other.
		{"odd width sbs left", Format{Layout: SideBySide}, Left, 1921, 1080, Rect{0, 0, 960, 1080}},
		{"odd width sbs right", Format{Layout: SideBySide}, Right, 1921, 1080, Rect{961, 0, 960, 1080}},
		{"odd height ou left", Format{Layout: OverUnder}, Left, 1920, 1081, Rect{0, 0, 1920, 540}},
		{"odd height ou right", Format{Layout: OverUnder}, Right, 1920, 1081, Rect{0, 541, 1920, 540}},

		// An unknown layout falls back to the whole frame: flat is more
		// recoverable than half an image stretched over the view.
		{"unknown layout", Format{Layout: Layout(42)}, Left, 800, 600, Rect{0, 0, 800, 600}},

		// Degenerate sizes must not produce negative rectangles.
		{"zero frame", Format{Layout: SideBySide}, Left, 0, 0, Rect{}},
		{"negative frame", Format{Layout: SideBySide}, Right, -10, -10, Rect{}},
	} {
		if got := tc.f.EyeRect(tc.eye, tc.w, tc.h); got != tc.want {
			t.Errorf("%s: EyeRect = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestEyeRectsTileTheFrame asserts the two eyes never overlap and never exceed
// the frame — the invariant that stops one eye sampling the other's pixels.
func TestEyeRectsTileTheFrame(t *testing.T) {
	for _, l := range []Layout{Mono, SideBySide, OverUnder} {
		for _, swapped := range []bool{false, true} {
			f := Format{Layout: l, Swapped: swapped}
			for _, dim := range [][2]int{{3840, 1080}, {1920, 2160}, {1921, 1081}, {2, 2}, {1, 1}} {
				w, h := dim[0], dim[1]
				lr := f.EyeRect(Left, w, h)
				rr := f.EyeRect(Right, w, h)
				for _, r := range []Rect{lr, rr} {
					if r.X < 0 || r.Y < 0 || r.W < 0 || r.H < 0 {
						t.Errorf("%v %dx%d: negative rect %+v", f, w, h, r)
					}
					if r.X+r.W > w || r.Y+r.H > h {
						t.Errorf("%v %dx%d: rect %+v leaves the frame", f, w, h, r)
					}
				}
				if l.Stereoscopic() {
					overlapX := lr.X < rr.X+rr.W && rr.X < lr.X+lr.W
					overlapY := lr.Y < rr.Y+rr.H && rr.Y < lr.Y+lr.H
					if overlapX && overlapY && lr.W > 0 && lr.H > 0 {
						t.Errorf("%v %dx%d: eye rects overlap: %+v and %+v", f, w, h, lr, rr)
					}
				}
			}
		}
	}
}

func TestStereoscopic(t *testing.T) {
	for _, tc := range []struct {
		l    Layout
		want bool
	}{{Mono, false}, {SideBySide, true}, {OverUnder, true}, {Layout(9), false}} {
		if got := tc.l.Stereoscopic(); got != tc.want {
			t.Errorf("Layout(%d).Stereoscopic() = %v, want %v", int(tc.l), got, tc.want)
		}
	}
}

func TestAspectCorrection(t *testing.T) {
	for _, tc := range []struct {
		l     Layout
		wantH float64
		wantV float64
	}{
		{Mono, 1, 1},
		{SideBySide, 2, 1},
		{OverUnder, 1, 2},
		{Layout(9), 1, 1},
	} {
		h, v := tc.l.AspectCorrection()
		if h != tc.wantH || v != tc.wantV {
			t.Errorf("Layout(%d).AspectCorrection() = (%v,%v), want (%v,%v)",
				int(tc.l), h, v, tc.wantH, tc.wantV)
		}
	}
}
