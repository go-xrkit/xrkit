// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package glasses

import (
	"math"
	"sort"
	"strings"
)

// Profile is what is known about one model of glasses.
//
// The only field a renderer truly needs is the field of view, and it is the one
// field nobody publishes in a usable form: manufacturers quote a DIAGONAL angle,
// while a projection needs the horizontal and vertical ones. So the diagonal is
// stored exactly as published — a figure that can be checked against a
// specification sheet — and the other two are derived. Storing a horizontal
// angle nobody can source would be a number invented to look precise.
type Profile struct {
	// Model is the name to show a person.
	Model string

	// match are lower-case substrings of the display name that identify this
	// model. Longer, more specific entries win; see Identify.
	match []string

	// DiagonalFOV is the manufacturer's published diagonal field of view, in
	// degrees, or 0 when it is not known for this model. Zero is honest and the
	// caller must handle it — see FOV.
	DiagonalFOV float64

	// EyeWidth and EyeHeight are the native panel pixels for ONE eye, or 0 when
	// not known.
	EyeWidth, EyeHeight int
}

// Known returns whether the profile carries a published field of view. A family
// fallback — "some pair of VITURE glasses" — does not.
func (p Profile) Known() bool { return p.DiagonalFOV > 0 }

// FOV returns the horizontal and vertical field of view in degrees for a view of
// the given aspect ratio (width divided by height), derived from the published
// diagonal.
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
// ok is false when the diagonal is unknown or the aspect is not positive, in
// which case both angles are zero. A caller that gets false must ask the user
// rather than guess: an assumed field of view puts everything in the wrong
// place, consistently and invisibly.
func (p Profile) FOV(aspect float64) (horizontal, vertical float64, ok bool) {
	if p.DiagonalFOV <= 0 || p.DiagonalFOV >= 180 || aspect <= 0 || math.IsInf(aspect, 0) {
		return 0, 0, false
	}
	td := math.Tan(rad(p.DiagonalFOV) / 2)
	th := td / math.Sqrt(1+1/(aspect*aspect))
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
// Each published figure is one a specification sheet states. Where a model's
// field of view could not be sourced, the entry is a FAMILY entry with no
// figure: it still identifies the display as glasses, which is what display
// selection needs, and it honestly reports that the geometry is unknown. Adding
// a made-up angle would be worse than admitting the gap, because a wrong field
// of view fails silently — everything renders, in the wrong place.
var catalogue = []Profile{
	// Published diagonal fields of view.
	{Model: "VITURE Beast", match: []string{"viture beast", "beast"}, DiagonalFOV: 58, EyeWidth: 1920, EyeHeight: 1200},
	{Model: "VITURE Luma Ultra", match: []string{"luma ultra"}, DiagonalFOV: 52, EyeWidth: 1920, EyeHeight: 1200},
	{Model: "XREAL One S", match: []string{"xreal one s", "xreal 1s"}, DiagonalFOV: 52, EyeWidth: 1920, EyeHeight: 1200},

	// Families. These identify a headset without claiming to know its optics.
	{Model: "VITURE glasses", match: []string{"viture"}},
	{Model: "XREAL glasses", match: []string{"xreal", "nreal"}},
	{Model: "Rokid glasses", match: []string{"rokid"}},
	{Model: "RayNeo glasses", match: []string{"rayneo", "tcl nxtwear"}},
	{Model: "INMO glasses", match: []string{"inmo"}},
	{Model: "Even Realities glasses", match: []string{"even realities"}},
	{Model: "Brilliant Labs glasses", match: []string{"brilliant"}},
}

// Identify reports which model a display name belongs to.
//
// The LONGEST matching substring wins, so "XREAL One S" is recognised as itself
// rather than as the XREAL family, whatever order the catalogue happens to be
// written in. Without that rule the catalogue's order would be load-bearing, and
// adding a model could silently reclassify another.
func Identify(displayName string) (Profile, bool) {
	lower := strings.ToLower(displayName)
	best, bestLen, found := Profile{}, 0, false
	for _, p := range catalogue {
		for _, m := range p.match {
			if len(m) > bestLen && strings.Contains(lower, m) {
				best, bestLen, found = p, len(m), true
			}
		}
	}
	return best, found
}

// Models lists every model the catalogue names, sorted, for a help message.
func Models() []string {
	names := make([]string, 0, len(catalogue))
	for _, p := range catalogue {
		names = append(names, p.Model)
	}
	sort.Strings(names)
	return names
}
