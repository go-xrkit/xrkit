// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package glasses

import (
	"math"
	"sort"
	"strings"
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
			p := Profile{PublishedFOV: diag, Axis: AxisDiagonal}
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

// TestFOVOnAHorizontalFigureDerivesOnlyTheVertical. A horizontal figure needs no
// decomposition: it IS the horizontal angle, and only the vertical is worked
// out. Handing the same number to both axes is exactly the confusion the Axis
// field exists to stop, so the two paths are pinned against each other here.
func TestFOVOnAHorizontalFigureDerivesOnlyTheVertical(t *testing.T) {
	for _, aspect := range []float64{16.0 / 10, 16.0 / 9, 1} {
		p := Profile{PublishedFOV: 46, Axis: AxisHorizontal}
		h, v, ok := p.FOV(aspect)
		if !ok {
			t.Fatalf("aspect=%g: FOV reported not-ok for a horizontal figure", aspect)
		}
		if math.Abs(h-46) > 1e-9 {
			t.Errorf("aspect=%g: H=%g, but a horizontal figure IS the horizontal angle", aspect, h)
		}
		if got := math.Tan(rad(h)/2) / math.Tan(rad(v)/2); math.Abs(got-aspect) > 1e-9 {
			t.Errorf("aspect=%g: derived tangents have aspect %g", aspect, got)
		}
		// The same number read as a diagonal gives a NARROWER view. That gap is
		// the silent failure: it renders, in the wrong place.
		hd, _, _ := Profile{PublishedFOV: 46, Axis: AxisDiagonal}.FOV(aspect)
		if aspect != 1 && hd >= h {
			t.Errorf("aspect=%g: read as a diagonal 46° gives H=%g, not narrower than %g", aspect, hd, h)
		}
	}
}

// TestFOVIsWiderThanItIsTall pins the orientation down. A sign or a reciprocal
// slipped in the derivation would still recompose to the diagonal, so the
// round-trip alone cannot catch it.
func TestFOVIsWiderThanItIsTall(t *testing.T) {
	p := Profile{PublishedFOV: 58, Axis: AxisDiagonal}
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
		{"no published figure", Profile{}, 1.6},
		{"negative figure", Profile{PublishedFOV: -10, Axis: AxisDiagonal}, 1.6},
		{"degenerate figure", Profile{PublishedFOV: 180, Axis: AxisDiagonal}, 1.6},
		// The one that matters: a real number nobody said the axis of.
		{"axis unstated", Profile{PublishedFOV: 43.5}, 1.6},
		{"axis out of range", Profile{PublishedFOV: 52, Axis: Axis(9)}, 1.6},
		{"zero aspect", Profile{PublishedFOV: 52, Axis: AxisDiagonal}, 0},
		{"negative aspect", Profile{PublishedFOV: 52, Axis: AxisDiagonal}, -1.6},
		{"infinite aspect", Profile{PublishedFOV: 52, Axis: AxisDiagonal}, math.Inf(1)},
	} {
		h, v, ok := tc.p.FOV(tc.aspect)
		if ok {
			t.Errorf("%s: reported ok", tc.name)
		}
		if h != 0 || v != 0 {
			t.Errorf("%s: returned H=%g V=%g instead of zeroes", tc.name, h, v)
		}
		if tc.p.Known() && tc.aspect > 0 && !math.IsInf(tc.aspect, 0) {
			t.Errorf("%s: Known() is true for a figure FOV would not use", tc.name)
		}
	}
}

func TestKnownSeparatesAFigureFromAFamily(t *testing.T) {
	isolate(t)
	beast, ok := Identify("VITURE Beast")
	if !ok || !beast.Known() {
		t.Fatalf("VITURE Beast: found=%v known=%v, want a published figure", ok, beast.Known())
	}
	// A family entry identifies the hardware without claiming to know its optics.
	fam, ok := Identify("VITURE")
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

func TestEnumsName(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{AxisUnstated.String(), "unstated"},
		{AxisDiagonal.String(), "diagonal"},
		{AxisHorizontal.String(), "horizontal"},
		{Axis(9).String(), "Axis(9)"},
		{Published.String(), "published specification"},
		{Enumerated.String(), "enumerated over USB"},
		{Observed.String(), "observed as a display"},
		{Confidence(9).String(), "Confidence(9)"},
		{NotIdentified.String(), "not identified"},
		{ByUSBProduct.String(), "USB product"},
		{ByDisplayName.String(), "display name"},
		{ByUSBVendor.String(), "USB vendor"},
		{How(9).String(), "How(9)"},
	} {
		if tc.got != tc.want {
			t.Errorf("String() = %q, want %q", tc.got, tc.want)
		}
	}
}

// TestIdentifyPrefersTheLongestMatch is the reason the catalogue's order is not
// load-bearing. "XREAL One Pro" contains "xreal one", so a first-match rule
// could classify one known model as another, and the failure would be a wrong
// field of view rather than an error.
func TestIdentifyPrefersTheLongestMatch(t *testing.T) {
	isolate(t)
	for _, tc := range []struct {
		name  string
		want  string
		known bool
	}{
		// The One family all contain each other's prefixes. Each must resolve
		// to itself, and none to the family.
		{"XREAL One", "XREAL One", true},
		{"XREAL One Pro", "XREAL One Pro", true},
		{"XREAL One S", "XREAL 1S", true},
		{"XREAL 1S", "XREAL 1S", true},
		{"xreal one pro (USB-C)", "XREAL One Pro", true},
		// So does the Air family.
		{"XREAL Air", "XREAL Air", true},
		{"XREAL Air 2", "XREAL Air 2", true},
		{"XREAL Air 2 Pro", "XREAL Air 2 Pro", true},
		{"XREAL Air 2 Ultra", "XREAL Air 2 Ultra", true},
		{"nreal air", "XREAL Air", true},
		{"nreal light", "XREAL Light", true},
		{"XREAL", "XREAL glasses", false},
		{"ROG XREAL R1", "ROG XREAL R1", true},
		// VITURE: the model matches only fire where a host surfaces more than
		// the bare EDID name, but they must be right when they do.
		{"VITURE Beast", "VITURE Beast", true},
		{"VITURE Luma Ultra XR GLASSES", "VITURE Luma Ultra", true},
		{"VITURE Luma Pro", "VITURE Luma Pro", true},
		{"VITURE Luma", "VITURE Luma", true},
		{"VITURE Pro", "VITURE Pro", true},
		{"VITURE One Lite", "VITURE One Lite", true},
		{"VITURE", "VITURE glasses", false},
		{"VITURE One", "VITURE glasses", false},
		// The Pro 2 must NOT inherit the Pro's 46°, which is the whole reason
		// its entry is there.
		{"VITURE Pro 2", "VITURE Pro 2", false},
		{"Rokid Max", "Rokid Max", true},
		{"Rokid Max 2", "Rokid Max 2", true},
		{"Rokid Air", "Rokid glasses", false},
		{"RayNeo Air 3s", "RayNeo Air 3s", true},
		{"RayNeo Air 3s Pro", "RayNeo Air 3s Pro", true},
		{"SmartGlasses", "RayNeo glasses", false},
		{"TCL NXTWEAR S", "TCL NXTWEAR S", true},
		// And the S+ must not inherit the S's 45°.
		{"TCL NXTWEAR S+", "TCL NXTWEAR S+", false},
		{"TCL NXTWEAR AIR", "TCL NXTWEAR AIR", true},
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

// TestShortNamesMatchOnlyTheWholeName is the trap this catalogue must not fall
// into. Firmware really does report a display name of "Air" or "One", so those
// have to be recognised — but "air" as a SUBSTRING also claims an AirPlay
// display and a ThinkVision T24 Air, both of which are ordinary screens. Taking
// over the wrong monitor full screen hijacks the machine the user is on.
func TestShortNamesMatchOnlyTheWholeName(t *testing.T) {
	isolate(t)
	for name, want := range map[string]string{
		"Air":         "XREAL Air",
		"air":         "XREAL Air",
		"  Air  ":     "XREAL Air",
		"Air 2":       "XREAL Air 2",
		"Air 2 Pro":   "XREAL Air 2 Pro",
		"Air 2 Ultra": "XREAL Air 2 Ultra",
		"One":         "XREAL One",
		"One Pro":     "XREAL One Pro",
		"1S":          "XREAL 1S",
		"Light":       "XREAL Light",
	} {
		p, ok := Identify(name)
		if !ok || p.Model != want {
			t.Errorf("%q identified as %+v (ok=%v), want %q", name, p.Model, ok, want)
		}
	}
	for _, name := range []string{
		"AirPlay Display", "T24 Air", "ThinkVision T24 Air", "Airport Display",
		"One Plus Monitor", "LG UltraFine", "Lightroom Display",
	} {
		if p, ok := Identify(name); ok {
			t.Errorf("%q was identified as %q; an ordinary monitor must not be taken for a headset",
				name, p.Model)
		}
	}
}

func TestIdentifyRejectsOrdinaryMonitors(t *testing.T) {
	isolate(t)
	for _, name := range []string{"", "   ", "DELL U2720Q", "Built-in Retina Display", "LG UltraFine"} {
		if p, ok := Identify(name); ok {
			t.Errorf("%q was identified as %q", name, p.Model)
		}
	}
}

// TestIdentifyDeviceUsesTheStrongestEvidence. A USB product id names a MODEL,
// and for VITURE panels it is the only thing that does: every one of them
// reports the display name "VITURE".
func TestIdentifyDeviceUsesTheStrongestEvidence(t *testing.T) {
	isolate(t)
	// The measured Luma Ultra: bare display name, but the USB device is
	// unambiguous.
	p, how := IdentifyDevice("VITURE", &USB{Vendor: 0x35ca, Product: 0x1104,
		Name: "VITURE Luma Ultra XR GLASSES"})
	if how != ByUSBProduct || p.Model != "VITURE Luma Ultra" {
		t.Errorf("IdentifyDevice = %q by %v, want the Luma Ultra by USB product", p.Model, how)
	}
	if p.Confidence != Enumerated {
		t.Errorf("Luma Ultra confidence = %v, want %v", p.Confidence, Enumerated)
	}

	// A product id nobody has listed still resolves through the product string.
	p, how = IdentifyDevice("VITURE", &USB{Vendor: 0x35ca, Product: 0x9999,
		Name: "VITURE Beast XR Glasses"})
	if how != ByUSBProduct || p.Model != "VITURE Beast" {
		t.Errorf("IdentifyDevice = %q by %v, want the Beast by its product string", p.Model, how)
	}

	// Neither id nor string: the brand, and the caller is told that is all it is.
	p, how = IdentifyDevice("", &USB{Vendor: 0x35ca, Product: 0x9999, Name: "XR GLASSES"})
	if how != ByUSBVendor || p.Model != "VITURE glasses" {
		t.Errorf("IdentifyDevice = %q by %v, want the VITURE family by vendor", p.Model, how)
	}

	// A display name beats a bare vendor, because it names a model.
	p, how = IdentifyDevice("Rokid Max 2", &USB{Vendor: 0x04d2, Product: 0x1234})
	if how != ByDisplayName || p.Model != "Rokid Max 2" {
		t.Errorf("IdentifyDevice = %q by %v, want the Max 2 by display name", p.Model, how)
	}

	// Nothing at all.
	if p, how := IdentifyDevice("DELL U2720Q", &USB{Vendor: 0x1234, Product: 0x5678}); how != NotIdentified {
		t.Errorf("IdentifyDevice = %q by %v, want nothing", p.Model, how)
	}
	if _, how := IdentifyDevice("DELL U2720Q", nil); how != NotIdentified {
		t.Errorf("a monitor with no USB device was identified as %v", how)
	}
}

// TestAVendorIdAloneDoesNotMakeGlasses. 0x1bbb is TCL's vendor id for phones as
// well as headsets, so the RayNeo entry names its product id and gets no
// vendor-only fallback. A phone must not become a headset.
func TestAVendorIdAloneDoesNotMakeGlasses(t *testing.T) {
	isolate(t)
	p, how := IdentifyDevice("", &USB{Vendor: 0x1bbb, Product: 0xaf50,
		Name: "Smart Glasses Human interface"})
	if how != ByUSBProduct || p.Model != "RayNeo glasses" {
		t.Errorf("IdentifyDevice = %q by %v, want the RayNeo family by product id", p.Model, how)
	}
	if p, how := IdentifyDevice("", &USB{Vendor: 0x1bbb, Product: 0x0001, Name: "TCL Phone"}); how != NotIdentified {
		t.Errorf("a TCL phone was identified as %q by %v", p.Model, how)
	}
}

// TestCatalogueEntriesAreWellFormed is a proof about the DATA, not the code. It
// is the one place a copy-and-paste slip in a hand-written table gets caught:
// a figure with no axis, a figure with no source, a model nothing can select,
// or two entries with the same name.
func TestCatalogueEntriesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range catalogue {
		if p.Model == "" {
			t.Error("an entry has no model name")
			continue
		}
		if seen[p.Model] {
			t.Errorf("%q appears twice", p.Model)
		}
		seen[p.Model] = true
		if len(p.match)+len(p.exact)+len(p.usbProducts)+len(p.usbMatch) == 0 && p.usbVendor == 0 {
			t.Errorf("%q: nothing can select it", p.Model)
		}
		for _, m := range append(append([]string{}, p.match...), append(p.exact, p.usbMatch...)...) {
			if m != strings.ToLower(m) || strings.TrimSpace(m) != m || m == "" {
				t.Errorf("%q: match string %q is not a trimmed, lower-case, non-empty string", p.Model, m)
			}
		}
		if len(p.usbProducts) > 0 && p.usbVendor == 0 {
			t.Errorf("%q: product ids with no vendor id", p.Model)
		}
		// A vendor id with no product ids beside it is a BRAND fallback: it
		// answers for every device of that vendor. A model entry must never be
		// one, or one model's figure would answer for its whole brand.
		if p.usbVendor != 0 && len(p.usbProducts) == 0 && p.Known() {
			t.Errorf("%q: a bare vendor id would lend its %v° to every device of that brand",
				p.Model, p.PublishedFOV)
		}
		if p.PublishedFOV != 0 && !(p.PublishedFOV > 0 && p.PublishedFOV < 180) {
			t.Errorf("%q: %v is not an angle", p.Model, p.PublishedFOV)
		}
		// The rule this catalogue lives by: a figure carries the URL it came
		// from, and says which angle it is.
		if p.PublishedFOV > 0 {
			if p.Source == "" {
				t.Errorf("%q publishes %v° with no source", p.Model, p.PublishedFOV)
			}
			if p.Axis == AxisUnstated {
				t.Errorf("%q publishes %v° with no axis, so it can never be used", p.Model, p.PublishedFOV)
			}
		}
		if (p.EyeWidth == 0) != (p.EyeHeight == 0) || p.EyeWidth < 0 || p.EyeHeight < 0 {
			t.Errorf("%q: %dx%d is half a panel size", p.Model, p.EyeWidth, p.EyeHeight)
		}
	}
}

// TestEveryCatalogueEntryIdentifiesItself walks the table and feeds each of its
// own match strings back through Identify. An entry whose match strings are all
// swallowed by a longer entry is dead weight that looks alive.
func TestEveryCatalogueEntryIdentifiesItself(t *testing.T) {
	isolate(t)
	for _, p := range catalogue {
		if len(p.match)+len(p.exact) == 0 {
			continue // USB-only, covered by the device tests
		}
		reachable := false
		for _, m := range append(append([]string{}, p.match...), p.exact...) {
			if got, ok := Identify(m); ok && got.Model == p.Model {
				reachable = true
			}
		}
		if !reachable {
			t.Errorf("%q: none of its own match strings resolve to it", p.Model)
		}
	}
}

func TestModelsAreListedInOrder(t *testing.T) {
	isolate(t)
	got := Models()
	if len(got) != len(catalogue) {
		t.Fatalf("Models() listed %d of %d entries", len(got), len(catalogue))
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("Models() is not sorted: %v", got)
	}
}

func TestNamesAreAllRecognised(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("the catalogue names nothing")
	}
	for _, n := range names {
		if _, ok := Identify(n); !ok {
			t.Errorf("Names offered %q, which Identify does not recognise", n)
		}
	}
	// Sorted and without repeats: this is a list somebody reads and a list
	// something iterates.
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("Names is not sorted, or repeats: %q then %q", names[i-1], names[i])
		}
	}
	// And it is the CATALOGUE's list, not a copy: every name comes from a
	// profile that is in it.
	in := make(map[string]bool, len(names))
	for _, n := range names {
		in[n] = true
	}
	found := 0
	for _, p := range catalogue {
		for _, n := range append(append([]string{}, p.exact...), p.match...) {
			if in[n] {
				found++
				break
			}
		}
	}
	if found != len(names) {
		t.Errorf("%d of %d names come from a profile in the catalogue", found, len(names))
	}
}

func TestNamesOfCoversTheShapesTheCatalogueDoesNotHaveYet(t *testing.T) {
	// A name Identify really does place, taken from the catalogue rather than
	// written down.
	real := Names()[0]

	got := namesOf([]Profile{
		{Model: "by an exact name", exact: []string{real}},
		{Model: "by a substring", match: []string{real}},
		{Model: "named twice", exact: []string{real}},
		{Model: "named by nothing"},
		{Model: "named by something no catalogue knows", exact: []string{"zzz not a headset"}},
	})
	if len(got) != 1 || got[0] != real {
		t.Errorf("namesOf = %v, want just %q: the repeat, the nameless and the "+
			"unrecognised are all left out", got, real)
	}
}

func TestVendorsNamesEveryMakerOnce(t *testing.T) {
	got := Vendors()
	if len(got) == 0 {
		t.Fatal("Vendors named nobody, so a caller has nothing to enumerate")
	}
	seen := make(map[uint16]bool, len(got))
	for i, v := range got {
		if v == 0 {
			t.Error("Vendors named vendor 0, which is not a vendor")
		}
		if seen[v] {
			t.Errorf("vendor %#04x listed twice", v)
		}
		seen[v] = true
		if i > 0 && got[i-1] >= v {
			t.Errorf("vendor %#04x follows %#04x, so the list is not sorted", v, got[i-1])
		}
	}
	// The makers whose hardware this has been tested against must be in it, or
	// the list is not the one the catalogue is built from.
	for _, want := range []uint16{0x35ca, 0x3318} {
		if !seen[want] {
			t.Errorf("vendor %#04x is in the catalogue but not in Vendors()", want)
		}
	}
	// Every vendor listed must belong to some profile, which is the direction
	// the loop above cannot check.
	for _, v := range got {
		found := false
		for _, p := range catalogue {
			if p.usbVendor == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("vendor %#04x is listed but belongs to no built-in profile", v)
		}
	}
}

func TestVendorsIncludesUserDeclaredMakers(t *testing.T) {
	isolate(t)
	before := len(Vendors())
	// A vendor id no real maker uses, so this cannot collide with the
	// catalogue and cannot be mistaken for evidence about real hardware.
	const invented = 0xfffe
	if err := Register(Declaration{
		Model:       "Test Headset",
		USBVendor:   invented,
		USBProducts: []uint16{0x0001},
		FOV:         50,
		Axis:        AxisDiagonal,
		Source:      "invented for a test",
	}); err != nil {
		t.Fatalf("Register refused a well-formed declaration: %v", err)
	}
	got := Vendors()
	if len(got) != before+1 {
		t.Errorf("Vendors went from %d to %d entries, want one more", before, len(got))
	}
	for _, v := range got {
		if v == invented {
			return
		}
	}
	t.Errorf("a user-declared vendor %#04x is not listed", invented)
}

func TestVendorTellsTwoAttachedHeadsetsApart(t *testing.T) {
	// The situation this is for: both makers' glasses plugged in at once.
	viture, howV := IdentifyDevice("VITURE", &USB{Vendor: 0x35ca, Product: 0x1104})
	xreal, howX := IdentifyDevice("XREAL 1S", &USB{Vendor: 0x3318, Product: 0x043e})
	if howV != ByUSBProduct || howX != ByUSBProduct {
		t.Fatalf("identified by %v and %v, want the USB product for both", howV, howX)
	}
	if viture.Vendor() == xreal.Vendor() {
		t.Fatal("two makers share a vendor id, so it cannot tell them apart")
	}
	// And the brand a display name alone identifies carries the same vendor as
	// the model behind it, which is what makes the match work.
	brand, _ := IdentifyDevice("VITURE", nil)
	if brand.Vendor() != viture.Vendor() {
		t.Errorf("the brand %q carries vendor %#04x and the model %q carries %#04x",
			brand.Model, brand.Vendor(), viture.Model, viture.Vendor())
	}
	if got := (Profile{}).Vendor(); got != 0 {
		t.Errorf("an empty profile claims vendor %#04x", got)
	}
}
