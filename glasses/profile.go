// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package glasses

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Axis says which angle a published field-of-view figure spans.
//
// It exists because almost nobody says. Manufacturers print "FOV: 52°" with no
// axis at all, and a horizontal figure read as a diagonal comes out too narrow
// everywhere — which is not a crash, it is a picture in the wrong place. So the
// axis is recorded next to the number, and a figure whose axis nobody stated is
// treated as no figure: see [Profile.Known].
type Axis int

// How a published figure is measured.
const (
	// AxisUnstated is the honest default: a number was published and nothing
	// said which angle it spans.
	AxisUnstated Axis = iota
	// AxisDiagonal is corner to corner, which is what the industry means when
	// it says so, and what the entries here have been checked to be.
	AxisDiagonal
	// AxisHorizontal is left to right.
	AxisHorizontal
)

// String names the axis.
func (a Axis) String() string {
	switch a {
	case AxisUnstated:
		return "unstated"
	case AxisDiagonal:
		return "diagonal"
	case AxisHorizontal:
		return "horizontal"
	}
	return fmt.Sprintf("Axis(%d)", int(a))
}

// Confidence says how far an entry has been checked against real hardware, as
// opposed to against a web page.
//
// It is a separate question from whether the figures are sourced. A
// specification sheet can be quoted perfectly and still describe a device whose
// display name, panel modes or USB identity are nothing like what the entry
// claims — nobody publishes those, and the only way to know them is to plug the
// thing in.
type Confidence int

// How much of an entry has been seen rather than read.
const (
	// Published means the entry comes from a published specification and
	// nothing else. Nobody here has held this model.
	Published Confidence = iota
	// Enumerated means the device has been seen on a machine's USB bus here —
	// its vendor, product and product string are first-hand — but its DISPLAY
	// name and modes have not been observed.
	Enumerated
	// Observed means this exact model was connected as a display, its display
	// name and its modes were seen, and it was rendered to.
	Observed
)

// String names the confidence.
func (c Confidence) String() string {
	switch c {
	case Published:
		return "published specification"
	case Enumerated:
		return "enumerated over USB"
	case Observed:
		return "observed as a display"
	}
	return fmt.Sprintf("Confidence(%d)", int(c))
}

// USB is what a USB enumeration says about an attached device. It is the subset
// this package can identify a model from, declared here rather than taken from
// a platform package so the matching can be tested against devices that do not
// exist.
type USB struct {
	// Vendor and Product are the USB idVendor and idProduct.
	Vendor, Product uint16
	// Name is the device's product string, as iProduct reports it — for
	// example "VITURE Luma Ultra XR GLASSES".
	Name string
}

// How says which evidence identified a profile, so a caller can tell a model it
// knows from a brand it guessed.
type How int

// The evidence, weakest last.
const (
	// NotIdentified means nothing matched.
	NotIdentified How = iota
	// ByUSBProduct means a USB product id or product string named the model.
	// This is the strongest answer: product ids distinguish models, and for
	// VITURE and RayNeo panels they are the ONLY thing that does.
	ByUSBProduct
	// ByDisplayName means the display's own name named it.
	ByDisplayName
	// ByUSBVendor means only the USB vendor matched, so the brand is known and
	// the model is not.
	ByUSBVendor
)

// String names the evidence.
func (h How) String() string {
	switch h {
	case NotIdentified:
		return "not identified"
	case ByUSBProduct:
		return "USB product"
	case ByDisplayName:
		return "display name"
	case ByUSBVendor:
		return "USB vendor"
	}
	return fmt.Sprintf("How(%d)", int(h))
}

// Profile is what is known about one model of glasses.
//
// The only field a renderer truly needs is the field of view, and it is the one
// field nobody publishes in a usable form: manufacturers quote a single angle,
// usually without saying which one, while a projection needs the horizontal and
// vertical ones. So the published figure is stored exactly as published, next to
// the [Axis] it was established to span, and the other two are derived. Storing
// a horizontal angle nobody can source would be a number invented to look
// precise.
type Profile struct {
	// Model is the name to show a person.
	Model string

	// match are lower-case substrings of the DISPLAY name that identify this
	// model. Longer, more specific entries win; see Identify.
	match []string

	// exact are lower-case display names that identify this model only when
	// they are the WHOLE name. Short EDID names live here: a headset really can
	// report itself as "Air", and "air" as a substring would also claim an
	// "AirPlay Display" and a ThinkVision "T24 Air".
	exact []string

	// usbVendor is the USB idVendor of this model's maker, or 0. usbProducts
	// are the idProduct values that are this model. usbMatch are lower-case
	// substrings of the USB product string.
	usbVendor   uint16
	usbProducts []uint16
	usbMatch    []string

	// PublishedFOV is the field of view the manufacturer published, in degrees,
	// or 0 when it is not known. Axis says which angle it spans, and is
	// AxisUnstated when nobody said — in which case the number is NOT usable
	// and Known reports false. Zero, and unstated, are honest, and the caller
	// must handle them: see FOV.
	PublishedFOV float64
	Axis         Axis

	// EyeWidth and EyeHeight are the native panel pixels for ONE eye, or 0 when
	// not known.
	EyeWidth, EyeHeight int

	// Confidence says how far this entry has been checked against hardware
	// rather than against a specification sheet.
	Confidence Confidence

	// Source is where the figures came from, so the next person can check them
	// instead of trusting them.
	Source string
}

// Known returns whether the profile carries a field of view a renderer can
// actually use.
//
// A family fallback — "some pair of VITURE glasses" — does not. Neither does an
// entry whose figure was published without saying which angle it spans: a
// number whose axis nobody stated is not more usable than no number, it is more
// dangerous, because it looks usable.
func (p Profile) Known() bool {
	return p.PublishedFOV > 0 && p.PublishedFOV < 180 &&
		(p.Axis == AxisDiagonal || p.Axis == AxisHorizontal)
}

// FOV returns the horizontal and vertical field of view in degrees for a view of
// the given aspect ratio (width divided by height), derived from the published
// figure.
//
// The relation is the one that falls out of a flat virtual screen at any
// distance: with half-width w, half-height h and distance d,
//
//	tan(H/2) = w/d, tan(V/2) = h/d, tan(D/2) = hypot(w,h)/d
//
// so tan(D/2)² = tan(H/2)² + tan(V/2)², and with aspect = w/h that gives
//
//	tan(H/2) = tan(D/2) / sqrt(1 + 1/aspect²)
//
// A horizontal figure needs no such step; only the vertical is derived from it.
//
// ok is false when [Profile.Known] is false or the aspect is not positive, in
// which case both angles are zero. A caller that gets false must ask the user
// rather than guess: an assumed field of view puts everything in the wrong
// place, consistently and invisibly.
func (p Profile) FOV(aspect float64) (horizontal, vertical float64, ok bool) {
	if !p.Known() || aspect <= 0 || math.IsInf(aspect, 0) {
		return 0, 0, false
	}
	var th float64
	switch p.Axis {
	case AxisDiagonal:
		th = math.Tan(rad(p.PublishedFOV)/2) / math.Sqrt(1+1/(aspect*aspect))
	case AxisHorizontal:
		th = math.Tan(rad(p.PublishedFOV) / 2)
	}
	tv := th / aspect
	return deg(2 * math.Atan(th)), deg(2 * math.Atan(tv)), true
}

// EyeAspect is the aspect ratio of one eye's panel, or 0 when the panel size is
// not known.
func (p Profile) EyeAspect() float64 {
	if p.EyeWidth <= 0 || p.EyeHeight <= 0 {
		return 0
	}
	return float64(p.EyeWidth) / float64(p.EyeHeight)
}

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }

// catalogue is every model this package can name.
//
// Each published figure is one a specification sheet states, with the URL it
// came from on the entry. Where a model's field of view could not be sourced —
// or was published without saying which angle it spans — the entry carries no
// usable figure: it still identifies the display as glasses, which is what
// display selection needs, and it honestly reports that the geometry is
// unknown. Adding a made-up angle would be worse than admitting the gap,
// because a wrong field of view fails silently — everything renders, in the
// wrong place.
//
// # On the axis
//
// Almost nobody labels the angle they publish. Three things were used to
// establish it, in descending order of strength, and each entry's brand comment
// says which applied:
//
//  1. The manufacturer writes "diagonal". Only VITURE does, and only for the
//     Beast.
//  2. The manufacturer's OWN equivalent-screen-size claim recomputes to the
//     published angle when read as a diagonal, and not when read as a
//     horizontal. A screen of D inches at R metres subtends
//     2*atan((D*0.0254/2)/R) on its diagonal, and that sum closes to 0.01° for
//     XREAL and to under a degree for VITURE, Rokid, RayNeo and TCL.
//  3. The figure sits in the SAME published table, on the same row, as one that
//     (1) or (2) settled. A brand does not change axis halfway down its own
//     specification sheet.
//
// Anything none of those reached keeps AxisUnstated and is not usable, which is
// the point of having the field.
//
// # On identification
//
// Display names and USB identities were both taken from decoded EDID binaries,
// kernel logs and compositor configurations, and both are third-party unless an
// entry's Confidence says otherwise. They do not behave the same way from brand
// to brand: XREAL's EDID names the model, VITURE's says only "VITURE", and
// every TCL/RayNeo panel says "SmartGlasses" and shares one USB product id. So
// some models can only be told apart over USB, and some cannot be told apart at
// all — which is why a family entry is a real answer here and not a placeholder.
//
// No macOS artifact was read for any of these. The EDID bytes are the same
// whatever the host, so the string almost certainly is too, but "almost
// certainly" is not this catalogue's standard: treat any macOS display name as
// unverified until one is read here.
var catalogue = []Profile{
	// VITURE, USB vendor 0x35ca. The Beast's blog states the axis outright —
	// "The VITURE Beast has a 58° diagonal FOV" — and VITURE's screen-size
	// claims all recompute at 4 m as diagonals: Beast 174" = 57.8° against 58°,
	// Luma Ultra 152" = 51.5° against 52°, Luma 146" = 49.7° against 50°. So the
	// whole line's bare "FOV" figures are read as diagonals.
	//
	// Every pre-Beast VITURE panel reports the bare EDID name "VITURE", so the
	// model-specific display matches below only fire where a host surfaces
	// something richer; USB is what actually separates these models.
	{Model: "VITURE Beast", match: []string{"viture beast"},
		usbVendor: 0x35ca, usbProducts: []uint16{0x1201, 0x1211}, usbMatch: []string{"viture beast"},
		PublishedFOV: 58, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1200, Confidence: Observed,
		Source: "https://www.viture.com/blog/engineered-reality-01-the-biggest-brightest-xr-glasses-how-viture-beast-works"},
	{Model: "VITURE Luma Ultra", match: []string{"viture luma ultra", "luma ultra"},
		usbVendor: 0x35ca, usbProducts: []uint16{0x1104}, usbMatch: []string{"viture luma ultra"},
		PublishedFOV: 52, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1200, Confidence: Enumerated,
		Source: "https://shop.viture.com/products/viture-luma-ultra-xr-glasses"},
	{Model: "VITURE Luma Pro", match: []string{"viture luma pro", "luma pro"},
		usbMatch:     []string{"viture luma pro"},
		PublishedFOV: 52, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1200,
		Source: "https://shop.viture.com/products/viture-luma-pro-xr-glasses"},
	{Model: "VITURE Luma", match: []string{"viture luma"},
		usbMatch:     []string{"viture luma"},
		PublishedFOV: 50, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1200,
		Source: "https://shop.viture.com/products/viture-luma-xr-glasses"},
	// The Pro and the One Lite are delisted; their figures are VITURE's own
	// specification tables as archived, which state the panel per eye outright:
	// "Resolution HD 1920(H) x 1080(V) per eye / Optics FOV 46°".
	{Model: "VITURE Pro", match: []string{"viture pro"},
		usbMatch:     []string{"viture pro"},
		PublishedFOV: 46, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://web.archive.org/web/20241203000000/https://www.viture.com/product/viture-pro-xr-glasses"},
	{Model: "VITURE One Lite", match: []string{"viture one lite", "one lite"},
		usbMatch:     []string{"viture one lite"},
		PublishedFOV: 43, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://web.archive.org/web/20241203000000/https://www.viture.com/product/viture-one-lite-xr-glasses"},
	// The Pro 2 carries NO figure, and the entry exists mainly so that it
	// cannot inherit one: "viture pro" is a substring of "VITURE Pro 2", so
	// without this line a Pro 2 would silently answer with the Pro's 46°. A
	// VITURE blog post mentions "50° FOV" in passing, which is not a
	// specification sheet, so it is recorded here and not in the field.
	{Model: "VITURE Pro 2", match: []string{"viture pro 2"},
		usbMatch: []string{"viture pro 2"},
		Source:   "https://www.viture.com/blog/ultraclarity-3-0-engineered-for-your-eyes-not-the-spec-sheet"},

	// XREAL, USB vendor 0x3318. XREAL never writes "diagonal", but its own two
	// figures prove it: the One Pro is sold as "57° FoV with up to a 171 inch
	// screen" at "a 4-meter distance", and 171 inches diagonal at 4 m subtends
	// 57.00°. The One closes the same way, 147" at 4 m = 50.0° against 50°.
	// UploadVR states it in as many words as well.
	//	https://www.xreal.com/us/one-pro
	//	https://www.uploadvr.com/xreal-one-announcement-preorders/
	// The angles and panels are from the one XREAL table whose row is labelled
	// "Resolution Per Eye", so no per-eye-versus-binocular guesswork is in them.
	//
	// XREAL's EDID DOES name the model, unlike VITURE's, and firmware revisions
	// report both a bare and a prefixed form — "One" and "XREAL One". The bare
	// forms are in exact rather than match, because "air" as a substring would
	// also claim an "AirPlay Display" and a ThinkVision "T24 Air", both real.
	{Model: "XREAL One Pro", match: []string{"xreal one pro"}, exact: []string{"one pro"},
		usbVendor: 0x3318, usbProducts: []uint16{0x0435, 0x0436},
		PublishedFOV: 57, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://docs.xreal.com/XREALDevices/XREAL%20Glasses"},
	{Model: "XREAL One", match: []string{"xreal one"}, exact: []string{"one"},
		usbVendor: 0x3318, usbProducts: []uint16{0x0437, 0x0438},
		PublishedFOV: 50, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://docs.xreal.com/XREALDevices/XREAL%20Glasses"},
	// XREAL's own name for this one is "XREAL 1S", and that is the string its
	// EDID reports. "xreal one s" is a COURTESY ALIAS for a person typing it by
	// hand; no artifact anywhere shows hardware saying it.
	{Model: "XREAL 1S", match: []string{"xreal 1s", "xreal one s"}, exact: []string{"1s"},
		usbVendor: 0x3318, usbProducts: []uint16{0x043d, 0x043e},
		PublishedFOV: 52, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1200,
		Source: "https://docs.xreal.com/XREALDevices/XREAL%20Glasses"},
	{Model: "XREAL Air 2 Ultra", match: []string{"xreal air 2 ultra"}, exact: []string{"air 2 ultra"},
		usbVendor: 0x3318, usbProducts: []uint16{0x0426},
		PublishedFOV: 52, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://docs.xreal.com/XREALDevices/XREAL%20Glasses"},
	{Model: "XREAL Air 2 Pro", match: []string{"xreal air 2 pro"}, exact: []string{"air 2 pro"},
		usbVendor: 0x3318, usbProducts: []uint16{0x0432},
		PublishedFOV: 46, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://docs.xreal.com/XREALDevices/XREAL%20Glasses"},
	{Model: "XREAL Air 2", match: []string{"xreal air 2"}, exact: []string{"air 2"},
		usbVendor: 0x3318, usbProducts: []uint16{0x0428},
		PublishedFOV: 46, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://docs.xreal.com/XREALDevices/XREAL%20Glasses"},
	{Model: "XREAL Air", match: []string{"xreal air", "nreal air"}, exact: []string{"air"},
		usbVendor: 0x3318, usbProducts: []uint16{0x0424},
		PublishedFOV: 46, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://docs.xreal.com/XREALDevices/XREAL%20Glasses"},
	{Model: "XREAL Light", match: []string{"xreal light", "nreal light"}, exact: []string{"light"},
		PublishedFOV: 52, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://docs.xreal.com/XREALDevices/XREAL%20Glasses"},
	// The ASUS-branded variant. Its page gives 57° and 1920x1080 but does NOT
	// say whether that resolution is per eye, and it is absent from the
	// "Resolution Per Eye" table, so the panel is left unknown rather than
	// assumed from the One Pro it shares an optic with.
	{Model: "ROG XREAL R1", match: []string{"rog xreal r1", "xreal r1"},
		PublishedFOV: 57, Axis: AxisDiagonal,
		Source: "https://tutorials.xreal.com/docs/glasses/r1/spec"},

	// Rokid. The AR Joy page pairs "Optical Display FoV — 50°" with "Virtual
	// Screen Size — 360" at a distance of 10 meters", and 360" at 10 m is
	// 49.1° diagonal — where a horizontal 50° on this 16:10 panel would need
	// 433". The same optic and the same 50° run through the whole Max line.
	{Model: "Rokid Max", match: []string{"rokid max"},
		PublishedFOV: 50, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://web.archive.org/web/20230323133947/https://global.rokid.com/pages/rokid-max-specs"},
	{Model: "Rokid Max 2", match: []string{"rokid max 2"},
		PublishedFOV: 50, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1200,
		Source: "https://global.rokid.com/blogs/max-2/what-is-the-display-resolution-of-the-rokid-max-2"},
	// AR Spatial is the Max 2 glasses sold with a Station 2. Its own page says
	// "1200p" without saying per eye, so the panel is left out rather than
	// carried over from the Max 2 it is probably the same as.
	{Model: "Rokid AR Spatial", match: []string{"rokid ar spatial", "ar spatial"},
		PublishedFOV: 50, Axis: AxisDiagonal,
		Source: "https://web.archive.org/web/20250711070128/https://global.rokid.com/pages/rokid-ar-spatial"},

	// RayNeo. Every Air page's hero block reads "FoV 46°,201"", and 201 inches
	// at the 6 m RayNeo's own comparison data gives is 46.09° diagonal — 47°
	// would need 205". RayNeo's comparison object separately says "47 degree"
	// for the Air 3s and Air 3s Pro, contradicting its own hero block on the
	// same site; 46° is the figure its screen-size claim supports, so 46° is
	// what is recorded and the disagreement is recorded here.
	//
	// None of these will be picked out by display name in practice: every
	// TCL/RayNeo panel reports the EDID name "SmartGlasses", and the whole line
	// shares USB 1bbb:af50. The model matches are here for hosts that surface
	// more, and the family entry is what will usually answer.
	{Model: "RayNeo Air 2", match: []string{"rayneo air 2"},
		PublishedFOV: 46, Axis: AxisDiagonal,
		Source: "https://www.rayneo.com/products/rayneo-air-2-xr-glasses"},
	{Model: "RayNeo Air 2s", match: []string{"rayneo air 2s"},
		PublishedFOV: 46, Axis: AxisDiagonal,
		Source: "https://www.rayneo.com/products/rayneo-air_2s"},
	{Model: "RayNeo Air 3s", match: []string{"rayneo air 3s"},
		PublishedFOV: 46, Axis: AxisDiagonal,
		Source: "https://www.rayneo.com/products/rayneo-air-3s-xr-glasses"},
	{Model: "RayNeo Air 3s Pro", match: []string{"rayneo air 3s pro"},
		PublishedFOV: 46, Axis: AxisDiagonal,
		Source: "https://www.rayneo.com/products/ar-glasses-rayneo-air-3s-pro-features"},

	// TCL. "Viewing size: 130 inches @4 meters" against "FOV: 45°" is 44.86°
	// diagonal, which settles the axis for the NXTWEAR line; the G and AIR
	// quote 140" at 4 m against 47°, which is 47.9° — a degree out, in the
	// direction that a diagonal reading explains and a horizontal one does not.
	// The panel is per eye because the same row gives the 3D mode as twice the
	// width: "1920 x 1080 at 2D, 3840 x 1080 at 3D".
	{Model: "TCL NXTWEAR S", match: []string{"tcl nxtwear s", "nxtwear s"},
		PublishedFOV: 45, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://www.tcl-eu.com/mwc2023/products_ecosystem_nxtwear_s"},
	// The S+ has no specification page anywhere, live or archived. Like the
	// VITURE Pro 2, its entry exists chiefly so that "tcl nxtwear s" cannot
	// silently lend it the S's 45°.
	{Model: "TCL NXTWEAR S+", match: []string{"tcl nxtwear s+", "nxtwear s+"}},
	{Model: "TCL NXTWEAR G", match: []string{"tcl nxtwear g", "nxtwear g"},
		PublishedFOV: 47, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://www.tcl.com/global/en/glasses/tcl-nxtwear-g/specifications"},
	{Model: "TCL NXTWEAR AIR", match: []string{"tcl nxtwear air", "nxtwear air"},
		PublishedFOV: 47, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://www.tcl.com/eu/en/glasses/tcl-nxtwear-air/specifications"},

	// Families. These identify a headset without claiming to know its optics,
	// and for VITURE and RayNeo they are what a display name alone can honestly
	// return.
	//
	// A vendor id with no product ids beside it is a BRAND FALLBACK: it is the
	// last thing tried, and it answers for any device of that vendor. So only
	// these entries carry a bare vendor id, and only for a vendor that makes
	// nothing but glasses. 0x1bbb is TCL's phone vendor id, so the RayNeo entry
	// names the product id instead and gets no fallback, and a TCL phone stays
	// a TCL phone.
	{Model: "VITURE glasses", match: []string{"viture"}, usbVendor: 0x35ca},
	{Model: "XREAL glasses", match: []string{"xreal", "nreal"}, usbVendor: 0x3318},
	{Model: "Rokid glasses", match: []string{"rokid"}, usbVendor: 0x04d2},
	{Model: "RayNeo glasses", match: []string{"rayneo", "tcl nxtwear", "smartglasses"},
		usbVendor: 0x1bbb, usbProducts: []uint16{0xaf50}, usbMatch: []string{"smart glasses human interface"}},
	// HUD glasses. These are Bluetooth devices with green monochrome
	// waveguides; they never enumerate as a DisplayPort monitor, so they carry
	// no geometry however well their figures are published.
	{Model: "INMO glasses", match: []string{"inmo"}},
	{Model: "Even Realities glasses", match: []string{"even realities"}},
	{Model: "Brilliant Labs glasses", match: []string{"brilliant"}},
}

// exactRank is added to an exact display-name match so that matching the WHOLE
// name outranks every substring match, however long. A display name is never
// anywhere near this long, so the two ranks cannot collide.
const exactRank = 1 << 16

// displayScore says how strongly a profile claims a display name, 0 for not at
// all. An EXACT match wins outright; among substrings the longest wins.
func (p Profile) displayScore(lower string) int {
	for _, e := range p.exact {
		if lower == e {
			return exactRank
		}
	}
	best := 0
	for _, m := range p.match {
		if len(m) > best && strings.Contains(lower, m) {
			best = len(m)
		}
	}
	return best
}

// usbProductScore says how strongly a profile claims a USB device as a MODEL. A
// product id is exact; failing that the product string is matched like a display
// name.
func (p Profile) usbProductScore(u USB) int {
	if p.usbVendor != 0 && p.usbVendor == u.Vendor {
		for _, id := range p.usbProducts {
			if id == u.Product {
				return exactRank
			}
		}
	}
	lower := strings.ToLower(u.Name)
	best := 0
	for _, m := range p.usbMatch {
		if len(m) > best && strings.Contains(lower, m) {
			best = len(m)
		}
	}
	return best
}

// usbVendorScore says whether a profile claims a USB device by its vendor
// alone.
//
// A vendor id with NO product ids is a brand fallback, and only those answer
// here: a model entry that lists product ids is specific to them, and a model
// entry identified by its product string carries no vendor id at all, so it
// cannot quietly become its whole brand's answer. A brand whose vendor id is
// also on its phones names a product id for that reason.
func (p Profile) usbVendorScore(u USB) int {
	if p.usbVendor != 0 && p.usbVendor == u.Vendor && len(p.usbProducts) == 0 {
		return 1
	}
	return 0
}

// scan returns the best-scoring profile in the built-in catalogue and then the
// user's, where 0 means no match.
//
// User entries are considered second and win TIES, so a locally declared model
// with the same match replaces the built-in one. That is how a figure this
// catalogue got wrong is corrected on the machine that noticed it, without
// waiting for a release. A stronger built-in match still wins, because a more
// specific model remains a better answer than a broader local one.
func scan(score func(Profile) int) (Profile, bool) {
	best, bestScore := Profile{}, 0
	for _, p := range catalogue {
		if s := score(p); s > bestScore {
			best, bestScore = p, s
		}
	}
	userMu.RLock()
	defer userMu.RUnlock()
	for _, p := range userCatalogue {
		if s := score(p); s > 0 && s >= bestScore {
			best, bestScore = p, s
		}
	}
	return best, bestScore > 0
}

// Identify reports which model a display name belongs to, for a caller that has
// nothing but the name — which is every caller that cannot read the USB bus.
//
// The LONGEST matching substring wins, so "XREAL One Pro" is recognised as
// itself rather than as the XREAL family, whatever order the catalogue happens
// to be written in. Without that rule the catalogue's order would be
// load-bearing, and adding a model could silently reclassify another.
//
// This is the WEAKER of the two identifications this package offers, and for
// some hardware it cannot do better than a brand: every VITURE panel reports
// the display name "VITURE", and every TCL and RayNeo panel reports
// "SmartGlasses". [IdentifyDevice] does better when a USB device is to hand.
func Identify(displayName string) (Profile, bool) {
	p, how := IdentifyDevice(displayName, nil)
	return p, how != NotIdentified
}

// IdentifyDevice resolves the best profile from what the caller actually knows,
// and says HOW it got there so the caller can tell a model it knows from a
// brand it guessed. usb may be nil.
//
// The order is by strength of evidence, not by convenience: a USB product id or
// product string names a MODEL, and for VITURE and RayNeo panels it is the only
// thing that does; a display name is next; a USB vendor id alone is last,
// because it names only a brand.
func IdentifyDevice(displayName string, usb *USB) (Profile, How) {
	if usb != nil {
		if p, ok := scan(func(p Profile) int { return p.usbProductScore(*usb) }); ok {
			return p, ByUSBProduct
		}
	}
	lower := strings.ToLower(strings.TrimSpace(displayName))
	if p, ok := scan(func(p Profile) int { return p.displayScore(lower) }); ok {
		return p, ByDisplayName
	}
	if usb != nil {
		if p, ok := scan(func(p Profile) int { return p.usbVendorScore(*usb) }); ok {
			return p, ByUSBVendor
		}
	}
	return Profile{}, NotIdentified
}

// Models lists every model the catalogue names, built-in and user-declared,
// sorted, for a help message. A user entry that overrides a built-in carries the
// same name and is listed once.
func Models() []string {
	userMu.RLock()
	names := make([]string, 0, len(catalogue)+len(userCatalogue))
	seen := make(map[string]bool, len(catalogue)+len(userCatalogue))
	for _, p := range catalogue {
		names, seen = appendModel(names, seen, p.Model)
	}
	for _, p := range userCatalogue {
		names, seen = appendModel(names, seen, p.Model)
	}
	userMu.RUnlock()
	sort.Strings(names)
	return names
}

// appendModel adds a name unless it has already been listed.
func appendModel(names []string, seen map[string]bool, model string) ([]string, map[string]bool) {
	if seen[model] {
		return names, seen
	}
	seen[model] = true
	return append(names, model), seen
}
