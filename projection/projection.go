// Package projection maps between directions a viewer looks in and positions in
// the source image.
//
// This is the whole of what makes immersive video immersive, and it is a pure
// function: given where the eye is pointing, say where to sample. Nothing here
// touches a GPU, a decoder or a display, so the geometry can be checked against
// known directions instead of by putting a headset on and squinting.
//
// The two directions are both needed. [Projection.Sample] answers "the eye looks
// there, where is that in the picture?", which is what a fragment shader asks
// once per output pixel. [Viewport.Ray] answers "which way is this output pixel
// looking?", which is what turns a rectangle of pixels into directions in the
// first place.
package projection

import (
	"fmt"
	"math"

	"github.com/go-xrkit/xrkit/pose"
)

// Kind is the geometry of the source image.
type Kind int

// The projections encountered in real material.
const (
	// Flat is ordinary rectilinear video: a virtual screen floating in front of
	// the viewer. Most material is this, including 3D films.
	Flat Kind = iota
	// Equirect is equirectangular: longitude across, latitude down. A full
	// sphere (360x180) or a hemisphere (180x180, "VR180") are both this, with
	// different spans.
	Equirect
	// Fisheye is an equidistant circular fisheye, where distance from the image
	// centre is proportional to the angle from straight ahead. Cameras that
	// shoot VR180 often deliver this without rectifying it.
	Fisheye
)

// String names the kind.
func (k Kind) String() string {
	switch k {
	case Flat:
		return "flat"
	case Equirect:
		return "equirectangular"
	case Fisheye:
		return "fisheye"
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Projection is a source geometry together with its extent.
type Projection struct {
	Kind Kind
	// HSpanDeg and VSpanDeg are how much of the world the image covers, in
	// degrees. For Equirect: 360x180 for a full sphere, 180x180 for VR180. For
	// Flat they are the angular size of the virtual screen. For Fisheye only
	// HSpanDeg is used, as the circular field of view (180, or 190-200 for a
	// lens that overshoots).
	HSpanDeg, VSpanDeg float64
}

// Common source geometries.
var (
	// Sphere360 is monoscopic or stereoscopic full-sphere equirectangular.
	Sphere360 = Projection{Kind: Equirect, HSpanDeg: 360, VSpanDeg: 180}
	// Hemisphere180 is VR180 equirectangular.
	Hemisphere180 = Projection{Kind: Equirect, HSpanDeg: 180, VSpanDeg: 180}
	// Fisheye180 is a circular fisheye covering a hemisphere.
	Fisheye180 = Projection{Kind: Fisheye, HSpanDeg: 180}
	// Screen is a flat virtual screen of a comfortable size: roughly what a
	// large television occupies from the sofa.
	Screen = Projection{Kind: Flat, HSpanDeg: 60, VSpanDeg: 34}
)

// Sample maps a direction to normalised coordinates in the eye image: u across
// from 0 at the left edge to 1 at the right, v down from 0 at the top. ok is
// false when the direction falls outside what the image covers — behind the
// viewer in a 180-degree piece, say — and the caller should show background
// rather than clamp, because clamping smears the edge pixels across the whole
// of the missing region.
//
// dir need not be normalised; it is normalised here. The zero vector has no
// direction and is refused.
func (p Projection) Sample(dir pose.Vec3) (u, v float64, ok bool) {
	d := dir.Unit()
	if d == (pose.Vec3{}) {
		return 0, 0, false
	}
	switch p.Kind {
	case Equirect:
		return p.sampleEquirect(d)
	case Fisheye:
		return p.sampleFisheye(d)
	default:
		return p.sampleFlat(d)
	}
}

// sampleEquirect converts longitude and latitude to image coordinates.
func (p Projection) sampleEquirect(d pose.Vec3) (u, v float64, ok bool) {
	h, vSpan := rad(p.HSpanDeg), rad(p.VSpanDeg)
	if h <= 0 || vSpan <= 0 {
		return 0, 0, false
	}
	// Longitude measured from straight ahead (-Z), increasing to the right (+X).
	//
	// At the poles longitude is undefined -- every meridian meets there -- and
	// the sign of a zero must not be allowed to decide it. When d.Z is +0, -d.Z
	// is NEGATIVE zero, and Atan2(0, -0) is pi, not 0: looking straight up would
	// sample the far edge of the image instead of its centre. Pinning the pole to
	// straight ahead keeps it continuous with the column below it.
	lon := 0.0
	if d.X != 0 || d.Z != 0 {
		lon = math.Atan2(d.X, -d.Z)
	}
	// Latitude from the vertical component; clamped because a unit vector's Y can
	// exceed 1 by an ulp and Asin would answer NaN.
	lat := math.Asin(clampUnit(d.Y))

	u = 0.5 + lon/h
	v = 0.5 - lat/vSpan
	return u, v, inUnit(u) && inUnit(v)
}

// sampleFisheye converts the angle from the optical axis to a radius.
func (p Projection) sampleFisheye(d pose.Vec3) (u, v float64, ok bool) {
	half := rad(p.HSpanDeg) / 2
	if half <= 0 {
		return 0, 0, false
	}
	// Angle away from straight ahead. Equidistant means radius is proportional
	// to this angle — that IS the fisheye mapping, and using a tangent here (as
	// a rectilinear lens would) is the classic way to get a picture that looks
	// almost right and is wrong everywhere but the centre.
	theta := math.Acos(clampUnit(-d.Z))
	r := theta / half
	if r > 1 {
		return 0, 0, false
	}
	// Azimuth around the axis. Dead centre has no azimuth; Atan2(0,0) is 0,
	// which is the right answer there because the radius is 0 anyway.
	az := math.Atan2(d.Y, d.X)
	u = 0.5 + 0.5*r*math.Cos(az)
	v = 0.5 - 0.5*r*math.Sin(az)
	return u, v, true
}

// sampleFlat projects onto a virtual screen a unit distance ahead.
func (p Projection) sampleFlat(d pose.Vec3) (u, v float64, ok bool) {
	hHalf, vHalf := rad(p.HSpanDeg)/2, rad(p.VSpanDeg)/2
	if hHalf <= 0 || vHalf <= 0 {
		return 0, 0, false
	}
	// Anything not in front of the viewer is off the screen. Without this test
	// the perspective divide by a positive Z would fold the world behind the
	// viewer onto the screen, mirrored.
	depth := -d.Z
	if depth <= 0 {
		return 0, 0, false
	}
	u = 0.5 + (d.X/depth)/(2*math.Tan(hHalf))
	v = 0.5 - (d.Y/depth)/(2*math.Tan(vHalf))
	return u, v, inUnit(u) && inUnit(v)
}

// Viewport is one eye's output rectangle and how much it sees.
type Viewport struct {
	// Width and Height are the eye's output size in pixels.
	Width, Height int
	// FOVyDeg is the vertical field of view in degrees. The horizontal follows
	// from the aspect ratio, which is what keeps the picture undistorted when
	// the output is not square — and an XR headset's per-eye view rarely is.
	FOVyDeg float64
}

// Ray returns the eye-space direction looked at by output pixel (x, y),
// measured through the pixel's CENTRE. Sampling through the corner biases the
// whole image by half a pixel, which is invisible in isolation and shows up as a
// seam where two views meet.
//
// The direction is not normalised — [Projection.Sample] normalises what it is
// given, and skipping a square root per pixel is worth having in a function
// called a few million times a frame.
func (vp Viewport) Ray(x, y int) pose.Vec3 {
	if vp.Width <= 0 || vp.Height <= 0 {
		return pose.Vec3{}
	}
	t := math.Tan(rad(vp.FOVyDeg) / 2)
	aspect := float64(vp.Width) / float64(vp.Height)
	// Normalised device coordinates through the pixel centre: -1..1 across, with
	// +Y up while pixel rows count downwards.
	ndcX := (2*(float64(x)+0.5)/float64(vp.Width) - 1) * t * aspect
	ndcY := (1 - 2*(float64(y)+0.5)/float64(vp.Height)) * t
	return pose.Vec3{X: ndcX, Y: ndcY, Z: -1}
}

// LookRay is [Viewport.Ray] turned into a world direction by the viewer's
// orientation: the one composition every renderer performs per pixel.
func (vp Viewport) LookRay(q pose.Quat, x, y int) pose.Vec3 {
	return q.Rotate(vp.Ray(x, y))
}

// inUnit reports whether t lies within the image, edges included.
func inUnit(t float64) bool { return t >= 0 && t <= 1 }

// clampUnit holds x inside [-1, 1], the domain of Asin and Acos. A unit vector's
// component can exceed 1 by an ulp, and the NaN that follows does not fail
// loudly: it becomes a hole in the picture.
func clampUnit(x float64) float64 {
	if x > 1 {
		return 1
	}
	if x < -1 {
		return -1
	}
	return x
}

func rad(d float64) float64 { return d * math.Pi / 180 }
