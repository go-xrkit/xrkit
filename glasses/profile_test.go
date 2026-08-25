// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package glasses

import (
	"math"
	"sort"
	"testing"
)

// TestFOVRecomposesToTheDiagonal is the assertion that matters, and it does not
// contain a single hand-computed angle.
//
// The horizontal and vertical fields of view are derived FROM the diagonal, so
// the honest check is that they go back TO it: tan(D/2)² must equal
// tan(H/2)² + tan(V/2)². A test full of magic numbers copied out of a
// calculator proves only that the calculator and the code agree, and both could
// be using the same wrong formula.
func TestFOVRecomposesToTheDiagonal(t *testing.T) {
	for _, diag := range []float64{30, 46, 50, 52, 58, 90, 120} {
		for _, aspect := range []float64{16.0 / 10, 16.0 / 9, 4.0 / 3, 1, 21.0 / 9, 0.5} {
			p := Profile{DiagonalFOV: diag}
			h, v, ok := p.FOV(aspect)
			if !ok {
				t.Fatalf("diag=%g aspect=%g: FOV reported not-ok for a valid pair", diag, aspect)
			}
			if h <= 0 || v <= 0 || h >= 180 || v >= 180 {
				t.Fatalf("diag=%g aspect=%g: H=%g V=%g is not a field of view", diag, aspect, h, v)
			}
			th, tv := math.Tan(rad(h)/2), math.Tan(rad(v)/2)
			got := deg(2 * math.Atan(math.Hypot(th, tv)))
			if math.Abs(got-diag) > 1e-9 {
				t.Errorf("diag=%g aspect=%g: H=%g and V=%g recompose to %g, not the diagonal",
					diag, aspect, h, v, got)
			}
			// And the derived pair must have the aspect that was asked for,
			// which is the other half of the claim.
			if gotAspect := th / tv; math.Abs(gotAspect-aspect) > 1e-9 {
				t.Errorf("diag=%g aspect=%g: derived tangents have aspect %g", diag, aspect, gotAspect)
			}
		}
	}
}

// TestFOVIsWiderThanItIsTall pins the orientation down. A sign or a reciprocal
// slipped in the derivation would still recompose to the diagonal, so the
// round-trip alone cannot catch it.
func TestFOVIsWiderThanItIsTall(t *testing.T) {
	p := Profile{DiagonalFOV: 58}
	h, v, ok := p.FOV(16.0 / 10)
	if !ok {
		t.Fatal("FOV not ok for a published diagonal")
	}
	if h <= v {
		t.Errorf("H=%g V=%g: a 16:10 view must be wider than it is tall", h, v)
	}
	// The diagonal is the largest of the three, always.
	if h >= 58 || v >= 58 {
		t.Errorf("H=%g V=%g: neither may reach the 58° diagonal", h, v)
	}

	// A square view must come out square.
	h, v, _ = p.FOV(1)
	if math.Abs(h-v) > 1e-9 {
		t.Errorf("a square view gave H=%g V=%g", h, v)
	}
}

func TestFOVRefusesWhatItCannotKnow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		p      Profile
		aspect float64
	}{
		{"no published diagonal", Profile{}, 1.6},
		{"negative diagonal", Profile{DiagonalFOV: -10}, 1.6},
		{"degenerate diagonal", Profile{DiagonalFOV: 180}, 1.6},
		{"zero aspect", Profile{DiagonalFOV: 52}, 0},
		{"negative aspect", Profile{DiagonalFOV: 52}, -1.6},
		{"infinite aspect", Profile{DiagonalFOV: 52}, math.Inf(1)},
	} {
		h, v, ok := tc.p.FOV(tc.aspect)
		if ok {
			t.Errorf("%s: reported ok", tc.name)
		}
		if h != 0 || v != 0 {
			t.Errorf("%s: returned H=%g V=%g instead of zeroes", tc.name, h, v)
		}
	}
}

func TestKnownSeparatesAFigureFromAFamily(t *testing.T) {
	beast, ok := Identify("VITURE Beast")
	if !ok || !beast.Known() {
		t.Fatalf("VITURE Beast: found=%v known=%v, want a published figure", ok, beast.Known())
	}
	// A family entry identifies the hardware without claiming to know its optics.
	fam, ok := Identify("VITURE Pro XR")
	if !ok {
		t.Fatal("a VITURE display was not identified at all")
	}
	if fam.Known() {
		t.Errorf("%q claims a field of view it has no source for", fam.Model)
	}
	if _, _, ok := fam.FOV(1.6); ok {
		t.Error("a family profile handed out a field of view")
	}
}

func TestEyeAspect(t *testing.T) {
	if got := (Profile{EyeWidth: 1920, EyeHeight: 1200}).EyeAspect(); math.Abs(got-1.6) > 1e-12 {
		t.Errorf("EyeAspect = %g, want 1.6", got)
	}
	for _, p := range []Profile{{}, {EyeWidth: 1920}, {EyeHeight: 1200}, {EyeWidth: -1, EyeHeight: 2}} {
		if got := p.EyeAspect(); got != 0 {
			t.Errorf("%+v: EyeAspect = %g, want 0 for an unknown panel", p, got)
		}
	}
}

// TestIdentifyPrefersTheLongestMatch is the reason the catalogue's order is not
// load-bearing. "XREAL One S" contains "xreal", so a first-match rule would
// classify a known model as an unknown family, and the failure would be a
// missing field of view rather than an error.
func TestIdentifyPrefersTheLongestMatch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  string
		known bool
	}{
		{"XREAL One S", "XREAL One S", true},
		{"XREAL 1S", "XREAL One S", true},
		{"xreal one s (USB-C)", "XREAL One S", true},
		{"XREAL One Pro", "XREAL glasses", false},
		{"XREAL Air 2", "XREAL glasses", false},
		{"nreal air", "XREAL glasses", false},
		{"VITURE Beast", "VITURE Beast", true},
		{"VITURE Luma Ultra", "VITURE Luma Ultra", true},
		{"VITURE Luma Pro", "VITURE glasses", false},
		{"Rokid Max", "Rokid glasses", false},
		{"TCL NXTWEAR S", "RayNeo glasses", false},
		{"RayNeo X2", "RayNeo glasses", false},
		{"INMO Air 2", "INMO glasses", false},
		{"Even Realities G1", "Even Realities glasses", false},
		{"Brilliant Frame", "Brilliant Labs glasses", false},
	} {
		p, ok := Identify(tc.name)
		if !ok {
			t.Errorf("%q was not identified", tc.name)
			continue
		}
		if p.Model != tc.want {
			t.Errorf("%q identified as %q, want %q", tc.name, p.Model, tc.want)
		}
		if p.Known() != tc.known {
			t.Errorf("%q: Known() = %v, want %v", tc.name, p.Known(), tc.known)
		}
	}
}

func TestIdentifyRejectsOrdinaryMonitors(t *testing.T) {
	for _, name := range []string{"", "DELL U2720Q", "Built-in Retina Display", "LG UltraFine"} {
		if p, ok := Identify(name); ok {
			t.Errorf("%q was identified as %q", name, p.Model)
		}
	}
}

func TestModelsAreListedInOrder(t *testing.T) {
	got := Models()
	if len(got) != len(catalogue) {
		t.Fatalf("Models() listed %d of %d entries", len(got), len(catalogue))
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("Models() is not sorted: %v", got)
	}
}
