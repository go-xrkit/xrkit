// Package stereo describes how a video frame packs one or two eye images, and
// where each eye's pixels are.
//
// It is deliberately just the packing. Nothing here knows about projection,
// decoding or displays: a frame is a rectangle, a layout says how it is divided,
// and the answer is a sub-rectangle. That separation is what lets the same
// 3840x1080 frame be read as two 1920x1080 eyes whether its content is a flat
// film, a 180-degree hemisphere or a full sphere.
package stereo

import "fmt"

// Eye selects one of the viewer's eyes.
type Eye int

// The two eyes.
const (
	Left Eye = iota
	Right
)

// String names the eye.
func (e Eye) String() string {
	switch e {
	case Left:
		return "left"
	case Right:
		return "right"
	}
	return fmt.Sprintf("Eye(%d)", int(e))
}

// Other returns the opposite eye.
func (e Eye) Other() Eye {
	if e == Left {
		return Right
	}
	return Left
}

// Layout is how a frame packs its eye images.
type Layout int

// The layouts encountered in real material.
const (
	// Mono is a single image; both eyes are given the whole frame and see the
	// same thing. Monoscopic 360 video is this.
	Mono Layout = iota
	// SideBySide splits the frame left half / right half. This is what the XR
	// glasses' own 3D display mode expects, and what most stereoscopic material
	// ships as.
	SideBySide
	// OverUnder splits the frame top half / bottom half. Preferred by some
	// encoders because it keeps the horizontal resolution intact, which matters
	// more than the vertical for a wide field of view.
	OverUnder
)

// String names the layout.
func (l Layout) String() string {
	switch l {
	case Mono:
		return "mono"
	case SideBySide:
		return "side-by-side"
	case OverUnder:
		return "over-under"
	}
	return fmt.Sprintf("Layout(%d)", int(l))
}

// Rect is a sub-rectangle of a frame, in pixels, with the origin at the top
// left.
type Rect struct {
	X, Y, W, H int
}

// Format fully describes how to read a frame.
type Format struct {
	Layout Layout
	// Swapped says the eye images are the other way round: the left half holds
	// the RIGHT eye. It happens, it is not detectable from the pixels, and
	// getting it wrong inverts the depth of the whole scene — near objects read
	// as far — which viewers report as eye strain rather than as a wrong image.
	// So it is an explicit flag, never a guess.
	Swapped bool
}

// EyeRect returns the region of a frameW x frameH frame holding eye's image.
//
// An odd frame dimension cannot be halved exactly. Rather than round and let one
// eye silently read a column of the other, the split floors: with a 1921-wide
// side-by-side frame each eye gets 960 pixels and the middle column is left
// unread. Losing one column is invisible; a column of the wrong eye is not.
func (f Format) EyeRect(eye Eye, frameW, frameH int) Rect {
	if frameW < 0 {
		frameW = 0
	}
	if frameH < 0 {
		frameH = 0
	}
	if f.Swapped {
		eye = eye.Other()
	}
	switch f.Layout {
	case SideBySide:
		half := frameW / 2
		x := 0
		if eye == Right {
			x = frameW - half
		}
		return Rect{X: x, Y: 0, W: half, H: frameH}
	case OverUnder:
		half := frameH / 2
		y := 0
		if eye == Right {
			y = frameH - half
		}
		return Rect{X: 0, Y: y, W: frameW, H: half}
	default:
		// Mono, and any unknown layout: both eyes get the whole frame. Falling
		// back to the whole frame shows the content flat rather than showing
		// half of it stretched, which is the more recoverable failure.
		return Rect{W: frameW, H: frameH}
	}
}

// Stereoscopic reports whether the layout carries two distinct eye images.
func (l Layout) Stereoscopic() bool { return l == SideBySide || l == OverUnder }

// AspectCorrection is what an eye image's width must be multiplied by to undo
// the layout's anamorphic squeeze, so the content is measured in the same units
// whatever the packing.
//
// Full-size side-by-side halves the horizontal resolution of each eye without
// changing what it depicts: a 3840x1080 side-by-side frame is two 1920x1080 eye
// images, each still covering the full field of view. So the pixels are not
// square any more, and a projection that assumes they are gets the geometry
// wrong by a factor of two. Over-under does the same to the vertical.
func (l Layout) AspectCorrection() (horizontal, vertical float64) {
	switch l {
	case SideBySide:
		return 2, 1
	case OverUnder:
		return 1, 2
	default:
		return 1, 1
	}
}
