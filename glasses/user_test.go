// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package glasses

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// isolate empties the user catalogue for one test and puts back whatever was
// there. Registration is process-wide state, so without this a test that
// declares a model would change what every later test identifies.
func isolate(t *testing.T) {
	t.Helper()
	userMu.Lock()
	saved := userCatalogue
	userCatalogue = nil
	userMu.Unlock()
	t.Cleanup(func() {
		userMu.Lock()
		userCatalogue = saved
		userMu.Unlock()
	})
}

// write puts a catalogue file in the test's own directory and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "glasses.hcl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the test catalogue: %v", err)
	}
	return path
}

func TestCataloguePathHonoursTheEnvironment(t *testing.T) {
	t.Setenv(EnvCatalogue, filepath.Join("somewhere", "else.hcl"))
	got, err := CataloguePath()
	if err != nil {
		t.Fatalf("CataloguePath = %v", err)
	}
	if got != filepath.Join("somewhere", "else.hcl") {
		t.Errorf("CataloguePath = %q, want the overridden path", got)
	}
}

func TestCataloguePathDefaultsUnderTheConfigDirectory(t *testing.T) {
	t.Setenv(EnvCatalogue, "")
	got, err := CataloguePath()
	if err != nil {
		t.Fatalf("CataloguePath = %v", err)
	}
	if want := filepath.Join("go-xrkit", "glasses.hcl"); !strings.HasSuffix(got, want) {
		t.Errorf("CataloguePath = %q, want it to end in %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("CataloguePath = %q, want an absolute path", got)
	}
}

// TestCataloguePathReportsAHomelessMachine covers the platform that cannot say
// where a user's configuration lives. Emptying all three variables the standard
// library consults makes it fail the same way on every OS the CI runs.
func TestCataloguePathReportsAHomelessMachine(t *testing.T) {
	t.Setenv(EnvCatalogue, "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", "")
	if _, err := CataloguePath(); err == nil {
		t.Fatal("CataloguePath invented a path on a machine with no home")
	} else if !strings.Contains(err.Error(), "glasses:") {
		t.Errorf("error does not name the package: %v", err)
	}
}

// TestLoadUserCatalogueTreatsNoFileAsNormal is the case nearly every run is in.
func TestLoadUserCatalogueTreatsNoFileAsNormal(t *testing.T) {
	isolate(t)
	t.Setenv(EnvCatalogue, filepath.Join(t.TempDir(), "absent.hcl"))
	if err := LoadUserCatalogue(); err != nil {
		t.Fatalf("a missing catalogue was reported as an error: %v", err)
	}
	userMu.RLock()
	defer userMu.RUnlock()
	if len(userCatalogue) != 0 {
		t.Errorf("a missing file registered %d models", len(userCatalogue))
	}
}

// TestLoadUserCatalogueReadsTheFile is the worked example from the package
// documentation, run as a test so the documentation cannot drift away from what
// the parser accepts.
func TestLoadUserCatalogueReadsTheFile(t *testing.T) {
	isolate(t)
	t.Setenv(EnvCatalogue, write(t, `
		glasses "ACME Visor 3" {
		  # Measured here: reported the display name below over USB-C.
		  source = "https://example.invalid/visor-3#specifications"

		  match = ["acme visor 3"]
		  exact = ["visor 3"]

		  usb_vendor   = "0x2b41"
		  usb_products = ["0x0110", "0x0111"]
		  usb_match    = ["acme visor 3"]

		  fov        = 46
		  fov_axis   = "diagonal"
		  eye_width  = 1920
		  eye_height = 1080
		}

		glasses "ACME glasses" {
		  # No published figure was found, so none is claimed.
		  match = ["acme"]
		}
	`))
	if err := LoadUserCatalogue(); err != nil {
		t.Fatalf("LoadUserCatalogue = %v", err)
	}
	p, ok := Identify("ACME Visor 3 XR GLASSES")
	if !ok {
		t.Fatal("a declared model was not identified")
	}
	if p.Model != "ACME Visor 3" || p.PublishedFOV != 46 || p.Axis != AxisDiagonal ||
		p.EyeWidth != 1920 || p.EyeHeight != 1080 || p.Source == "" {
		t.Errorf("loaded %+v, not what the file declares", p)
	}
	if !p.Known() {
		t.Error("a declared figure did not survive as a known one")
	}
	// exact, and both USB routes.
	if got, ok := Identify("Visor 3"); !ok || got.Model != "ACME Visor 3" {
		t.Errorf("exact match gave %q", got.Model)
	}
	for _, u := range []USB{
		{Vendor: 0x2b41, Product: 0x0111},
		{Vendor: 0x2b41, Product: 0x9999, Name: "ACME Visor 3"},
	} {
		if got, how := IdentifyDevice("", &u); how != ByUSBProduct || got.Model != "ACME Visor 3" {
			t.Errorf("%+v identified as %q by %v", u, got.Model, how)
		}
	}
	// The family block, and its unknown geometry.
	fam, ok := Identify("ACME Something Else")
	if !ok || fam.Model != "ACME glasses" || fam.Known() {
		t.Errorf("family block gave %+v (ok=%v)", fam, ok)
	}
	if _, _, ok := fam.FOV(1.6); ok {
		t.Error("a declared family handed out a field of view")
	}
}

// TestLoadUserCatalogueForwardsAPathFailure: a broken machine must not be
// silently indistinguishable from a machine with no catalogue.
func TestLoadUserCatalogueForwardsAPathFailure(t *testing.T) {
	isolate(t)
	t.Setenv(EnvCatalogue, "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", "")
	if err := LoadUserCatalogue(); err == nil {
		t.Fatal("LoadUserCatalogue succeeded with nowhere to read from")
	}
}

// TestLoadUserCatalogueForwardsABadFile: a file that exists and is wrong is an
// error even through the forgiving entry point, because the person who wrote it
// meant it to take effect.
func TestLoadUserCatalogueForwardsABadFile(t *testing.T) {
	isolate(t)
	t.Setenv(EnvCatalogue, write(t,
		"glasses \"x\" {\n  match = [\"x\"]\n  fov = 400\n  fov_axis = \"diagonal\"\n}\n"))
	if err := LoadUserCatalogue(); err == nil {
		t.Fatal("a broken file loaded without complaint")
	}
}

func TestLoadCatalogueFileRefusesWhatItCannotRead(t *testing.T) {
	isolate(t)
	if err := LoadCatalogueFile(filepath.Join(t.TempDir(), "absent.hcl")); err == nil {
		t.Fatal("reading a file that is not there succeeded")
	}
}

// TestLoadCatalogueFileRejectsBadFiles walks every way a file can be wrong. In
// each case the message must name the file, so the person can go and fix the
// line, and nothing at all must be registered: half a catalogue would leave the
// machine running on a mixture of what was written and what was meant.
func TestLoadCatalogueFileRejectsBadFiles(t *testing.T) {
	// block builds a one-model file, with each attribute on its own line: HCL
	// only allows a single argument on a block's own line.
	block := func(attrs ...string) string {
		return "glasses \"x\" {\n  " + strings.Join(attrs, "\n  ") + "\n}\n"
	}
	const sel = `match = ["x"]`
	for _, tc := range []struct {
		name, body, want string
	}{
		{"empty file", "", "declares no glasses"},
		{"not HCL", "glasses {{{", "glasses:"},
		{"no label", "glasses {\n}\n", "label"},
		{"blank label", "glasses \"  \" {\n  " + sel + "\n}\n", "Model with no name"},
		{"unknown block", "headset \"x\" {\n}\n", "headset"},
		{"misspelt attribute", block(sel, `fovaxis = "diagonal"`), "fovaxis"},
		{"nothing selects it", block(`source = "s"`), "nothing can select"},
		{"empty match list", block(`match = []`), "nothing can select"},
		{"blank match string", block(`match = ["ok", " "]`), "matches everything"},
		{"blank exact string", block(`exact = [""]`), "matches everything"},
		{"blank usb_match", block(`usb_match = [" "]`), "matches everything"},
		{"match is not a list", block(`match = "x"`), "glasses:"},
		{"zero angle", block(sel, `fov = 0`, `fov_axis = "diagonal"`), "Not a field of view"},
		{"negative angle", block(sel, `fov = -5`, `fov_axis = "diagonal"`), "Not a field of view"},
		{"degenerate angle", block(sel, `fov = 180`, `fov_axis = "diagonal"`), "Not a field of view"},
		{"angle is not a number", block(sel, `fov = "wide"`, `fov_axis = "diagonal"`), "glasses:"},
		{"angle with no axis", block(sel, `fov = 46`), "An angle with no axis"},
		{"axis with no angle", block(sel, `fov_axis = "diagonal"`), "An angle with no axis"},
		{"unknown axis", block(sel, `fov = 46`, `fov_axis = "sideways"`), "Not an axis"},
		{"axis is not a string", block(sel, `fov = 46`, `fov_axis = 3`), "glasses:"},
		{"zero panel", block(sel, `eye_width = 0`, `eye_height = 1080`), "Not a panel size"},
		{"negative panel", block(sel, `eye_width = 1920`, `eye_height = -1`), "Not a panel size"},
		{"panel is not a number", block(sel, `eye_width = "wide"`, `eye_height = 1080`), "glasses:"},
		{"half a panel", block(sel, `eye_width = 1920`), "Half a panel size"},
		{"the other half", block(sel, `eye_height = 1080`), "Half a panel size"},
		{"vendor is not an id", block(sel, `usb_vendor = "viture"`), "Not a USB id"},
		{"vendor is not hex", block(sel, `usb_vendor = "35ca"`), "Not a USB id"},
		{"vendor is not a string", block(sel, `usb_vendor = ["0x35ca"]`), "glasses:"},
		{"product is not an id", block(sel, `usb_vendor = "0x35ca"`, `usb_products = ["beast"]`), "Not a USB id"},
		{"products are not a list", block(sel, `usb_vendor = "0x35ca"`, `usb_products = "0x1"`), "glasses:"},
		{"products with no vendor", block(sel, `usb_products = ["0x1104"]`), "no vendor"},
		{"source is not a string", block(sel, `source = ["a"]`), "glasses:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			path := write(t, tc.body)
			err := LoadCatalogueFile(path)
			if err == nil {
				t.Fatalf("%s loaded without complaint", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not mention %q:\n%v", tc.want, err)
			}
			if !strings.Contains(err.Error(), filepath.Base(path)) {
				t.Errorf("error does not name the file %q:\n%v", path, err)
			}
			userMu.RLock()
			defer userMu.RUnlock()
			if len(userCatalogue) != 0 {
				t.Errorf("a rejected file still registered %d models", len(userCatalogue))
			}
		})
	}
}

// TestLoadCatalogueFileNamesTheOffendingEntryAndLine: with several models in the
// file, saying "something is wrong" is not enough to find it.
func TestLoadCatalogueFileNamesTheOffendingEntryAndLine(t *testing.T) {
	isolate(t)
	err := LoadCatalogueFile(write(t, "glasses \"Good One\" {\n  match = [\"good\"]\n}\n\n"+
		"glasses \"Bad One\" {\n  match = [\"bad\"]\n  fov = 190\n  fov_axis = \"diagonal\"\n}\n"))
	if err == nil {
		t.Fatal("a bad second entry loaded without complaint")
	}
	for _, want := range []string{"Bad One", "190", "line 7"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// TestLoadCatalogueFileReportsEveryProblemAtOnce: a person editing a file would
// rather be told about all of their mistakes than run the program four times.
func TestLoadCatalogueFileReportsEveryProblemAtOnce(t *testing.T) {
	isolate(t)
	err := LoadCatalogueFile(write(t, "glasses \"A\" {\n  match = [\"a\"]\n  fov = 400\n  fov_axis = \"diagonal\"\n}\n\n"+
		"glasses \"B\" {\n  match = [\"b\"]\n  eye_width = -1\n  eye_height = 1080\n}\n"))
	if err == nil {
		t.Fatal("two bad entries loaded without complaint")
	}
	for _, want := range []string{"400", "Not a panel size"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// TestUnstatedIsAWayToRecordAnUnusableNumber. A person who finds "43.5°" on a
// specification sheet that does not say which angle it is can write it down and
// keep the provenance, while the package refuses to use it.
func TestUnstatedIsAWayToRecordAnUnusableNumber(t *testing.T) {
	isolate(t)
	if err := LoadCatalogueFile(write(t, `
		glasses "ACME Visor" {
		  # ACME prints "Field of View 43.5" with no unit and no axis.
		  match    = ["acme visor"]
		  fov      = 43.5
		  fov_axis = "unstated"
		  source   = "https://example.invalid/visor"
		}`)); err != nil {
		t.Fatalf("LoadCatalogueFile = %v", err)
	}
	p, ok := Identify("ACME Visor")
	if !ok || p.PublishedFOV != 43.5 || p.Axis != AxisUnstated {
		t.Fatalf("loaded %+v (ok=%v)", p, ok)
	}
	if p.Known() {
		t.Error("a figure whose axis nobody stated was reported as usable")
	}
	if _, _, ok := p.FOV(1.6); ok {
		t.Error("FOV used a figure whose axis nobody stated")
	}
}

// TestLoadCatalogueFileAccumulates: two files, or two calls, add up rather than
// replacing each other.
func TestLoadCatalogueFileAccumulates(t *testing.T) {
	isolate(t)
	for _, body := range []string{`glasses "First" { match = ["first"] }`, `glasses "Second" { match = ["second"] }`} {
		if err := LoadCatalogueFile(write(t, body)); err != nil {
			t.Fatalf("loading %q: %v", body, err)
		}
	}
	for _, want := range []string{"First", "Second"} {
		if p, ok := Identify(want); !ok || p.Model != want {
			t.Errorf("%q identified as %+v, ok=%v", want, p, ok)
		}
	}
}

func TestRegister(t *testing.T) {
	isolate(t)
	if err := Register(Declaration{
		Model: "ACME Visor 3", Match: []string{"ACME V3"}, Exact: []string{"V3"},
		USBVendor: 0x2b41, USBProducts: []uint16{0x0110}, USBMatch: []string{"ACME V3"},
		FOV: 46, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1080,
		Source: "https://example.invalid/v3",
	}); err != nil {
		t.Fatalf("Register = %v", err)
	}
	// The match strings are lower-cased on the way in, so a caller does not
	// have to know that identification works in lower case.
	p, ok := Identify("acme v3 xr glasses")
	if !ok || p.Model != "ACME Visor 3" || p.PublishedFOV != 46 {
		t.Errorf("Identify = %+v, ok=%v", p, ok)
	}
	if got, how := IdentifyDevice("", &USB{Vendor: 0x2b41, Product: 0x0110}); how != ByUSBProduct ||
		got.Model != "ACME Visor 3" {
		t.Errorf("IdentifyDevice = %q by %v", got.Model, how)
	}
	// A family declaration: no figure, and it says so.
	if err := Register(Declaration{Model: "ACME glasses", Match: []string{"acme"}}); err != nil {
		t.Fatalf("Register = %v", err)
	}
	if p, ok := Identify("ACME Visor 9"); !ok || p.Model != "ACME glasses" || p.Known() {
		t.Errorf("Identify = %+v, ok=%v", p, ok)
	}
}

func TestRegisterRefusesNonsense(t *testing.T) {
	full := func(d Declaration) Declaration {
		if d.Match == nil && d.Exact == nil && d.USBMatch == nil && d.USBVendor == 0 {
			d.Match = []string{"m"}
		}
		if d.Model == "" {
			d.Model = "m"
		}
		return d
	}
	for _, tc := range []struct {
		name string
		d    Declaration
		want string
	}{
		{"no name", Declaration{Model: " ", Match: []string{"m"}}, "no name"},
		{"nothing selects it", Declaration{Model: "m"}, "no way to recognise it"},
		{"blank match", full(Declaration{Match: []string{""}}), "Match[0] is empty"},
		{"blank exact", full(Declaration{Exact: []string{" "}}), "Exact[0] is empty"},
		{"blank usb match", full(Declaration{USBMatch: []string{""}}), "USBMatch[0] is empty"},
		{"product with no vendor", full(Declaration{USBProducts: []uint16{1}}), "without a USBVendor"},
		{"negative angle", full(Declaration{FOV: -1, Axis: AxisDiagonal}), "not an angle"},
		{"degenerate angle", full(Declaration{FOV: 180, Axis: AxisDiagonal}), "not an angle"},
		{"NaN angle", full(Declaration{FOV: nan(), Axis: AxisDiagonal}), "not an angle"},
		{"negative width", full(Declaration{EyeWidth: -1, EyeHeight: 1080}), "not a panel size"},
		{"negative height", full(Declaration{EyeWidth: 1920, EyeHeight: -1}), "not a panel size"},
		{"half a panel", full(Declaration{EyeWidth: 1920}), "not a panel size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			err := Register(tc.d)
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			userMu.RLock()
			defer userMu.RUnlock()
			if len(userCatalogue) != 0 {
				t.Error("a rejected model was registered anyway")
			}
		})
	}
}

// TestRegisterAcceptsAUSBOnlyModel: a VITURE-shaped model, whose display name
// says nothing and whose USB identity says everything.
func TestRegisterAcceptsAUSBOnlyModel(t *testing.T) {
	isolate(t)
	if err := Register(Declaration{Model: "ACME USB-only", USBVendor: 0x2b41}); err != nil {
		t.Fatalf("Register = %v", err)
	}
	if p, how := IdentifyDevice("", &USB{Vendor: 0x2b41, Product: 1}); how != ByUSBVendor ||
		p.Model != "ACME USB-only" {
		t.Errorf("IdentifyDevice = %q by %v", p.Model, how)
	}
}

// nan is spelt out rather than imported, so the table above reads as data.
func nan() float64 { var zero float64; return zero / zero }

// TestUserEntriesWinTies is the point of the whole mechanism: a figure this
// package got wrong can be corrected on the machine that noticed, without
// waiting for a release.
func TestUserEntriesWinTies(t *testing.T) {
	isolate(t)
	before, ok := Identify("VITURE Beast")
	if !ok || before.PublishedFOV != 58 {
		t.Fatalf("built-in VITURE Beast is %+v, ok=%v", before, ok)
	}
	if err := Register(Declaration{Model: "VITURE Beast (corrected)", Match: []string{"viture beast"},
		FOV: 55, Axis: AxisDiagonal, EyeWidth: 1920, EyeHeight: 1200}); err != nil {
		t.Fatalf("Register = %v", err)
	}
	after, ok := Identify("VITURE Beast")
	if !ok || after.Model != "VITURE Beast (corrected)" || after.PublishedFOV != 55 {
		t.Errorf("Identify = %+v, ok=%v; the local entry must displace the built-in", after, ok)
	}
	// And the same over USB.
	if err := Register(Declaration{Model: "Luma Ultra (corrected)", USBVendor: 0x35ca,
		USBProducts: []uint16{0x1104}}); err != nil {
		t.Fatalf("Register = %v", err)
	}
	if p, _ := IdentifyDevice("", &USB{Vendor: 0x35ca, Product: 0x1104}); p.Model != "Luma Ultra (corrected)" {
		t.Errorf("IdentifyDevice = %q; the local entry must displace the built-in", p.Model)
	}
}

// TestUserEntriesDoNotBeatALongerBuiltIn: an override is for the model it
// names. A broad local entry must not swallow the specific models the catalogue
// already knows, or adding one family entry would blind the whole brand.
func TestUserEntriesDoNotBeatALongerBuiltIn(t *testing.T) {
	isolate(t)
	if err := Register(Declaration{Model: "My VITUREs", Match: []string{"viture"}}); err != nil {
		t.Fatalf("Register = %v", err)
	}
	if p, _ := Identify("VITURE Beast"); p.Model != "VITURE Beast" {
		t.Errorf("VITURE Beast identified as %q; a longer built-in match must still win", p.Model)
	}
	// The broad entry does take the displays no specific entry claims.
	if p, _ := Identify("VITURE Something Unheard Of"); p.Model != "My VITUREs" {
		t.Errorf("identified as %q, want the local family", p.Model)
	}
}

// TestTheLastRegistrationWins settles what happens when a person declares the
// same match twice: the later declaration is the one in force, so re-loading a
// file after editing it does what the person expects.
func TestTheLastRegistrationWins(t *testing.T) {
	isolate(t)
	for _, d := range []Declaration{
		{Model: "Old", Match: []string{"acme"}, FOV: 46, Axis: AxisDiagonal},
		{Model: "New", Match: []string{"acme"}, FOV: 50, Axis: AxisDiagonal},
	} {
		if err := Register(d); err != nil {
			t.Fatalf("Register(%q) = %v", d.Model, err)
		}
	}
	if p, _ := Identify("ACME"); p.Model != "New" || p.PublishedFOV != 50 {
		t.Errorf("Identify = %+v, want the later declaration", p)
	}
}

// TestModelsListsUserEntriesOnce covers both halves of the listing: a new local
// model appears, and one that overrides a built-in does not appear twice.
func TestModelsListsUserEntriesOnce(t *testing.T) {
	isolate(t)
	base := len(Models())
	for _, d := range []Declaration{
		{Model: "ACME glasses", Match: []string{"acme"}},
		{Model: "VITURE glasses", Match: []string{"viture xr"}},
	} {
		if err := Register(d); err != nil {
			t.Fatalf("Register(%q) = %v", d.Model, err)
		}
	}
	got := Models()
	if len(got) != base+1 {
		t.Errorf("Models() has %d entries, want %d: the override must not be listed twice\n%v",
			len(got), base+1, got)
	}
	found := false
	for _, m := range got {
		if m == "ACME glasses" {
			found = true
		}
	}
	if !found {
		t.Errorf("Models() omits a declared model: %v", got)
	}
}

// TestConcurrentRegisterAndIdentify is here for the race detector.
// Identification runs on whichever goroutine owns the display, a load runs on
// whichever one handles configuration, and those are not the same one.
func TestConcurrentRegisterAndIdentify(t *testing.T) {
	isolate(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := Register(Declaration{Model: "ACME glasses", Match: []string{"acme"}}); err != nil {
				t.Errorf("Register = %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			Identify("ACME Visor")
			IdentifyDevice("ACME Visor", &USB{Vendor: 0x35ca, Product: 0x1104})
			Models()
			if i == 0 {
				if _, err := CataloguePath(); err != nil {
					t.Errorf("CataloguePath = %v", err)
				}
			}
		}()
	}
	wg.Wait()
}
