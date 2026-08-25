// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package glasses

import (
	"strings"
	"testing"
)

func TestDisplayString(t *testing.T) {
	got := Display{Name: "VITURE Beast", Width: 3840, Height: 1080}.String()
	if want := `"VITURE Beast" 3840x1080`; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	got = Display{Name: "Built-in", Width: 3024, Height: 1964, Primary: true}.String()
	if want := `"Built-in" 3024x1964 (primary)`; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestChooseDisplayRefusesAnEmptyMachine(t *testing.T) {
	if _, err := ChooseDisplay(nil, ""); err == nil {
		t.Fatal("choosing from no displays succeeded")
	}
}

// TestChooseDisplayPrefersGlasses is the whole point: on a laptop with a headset
// attached, the headset is what the application is for.
func TestChooseDisplayPrefersGlasses(t *testing.T) {
	displays := []Display{
		{Name: "Built-in Retina Display", Width: 3024, Height: 1964, Primary: true},
		{Name: "DELL U2720Q", Width: 3840, Height: 2160},
		{Name: "VITURE Beast", Width: 3840, Height: 1080},
	}
	got, err := ChooseDisplay(displays, "")
	if err != nil {
		t.Fatalf("ChooseDisplay = %v", err)
	}
	if got.Name != "VITURE Beast" {
		t.Errorf("chose %q; the glasses must win even against a wider monitor", got.Name)
	}
}

func TestChooseDisplayFallsBackToTheWidestExternal(t *testing.T) {
	displays := []Display{
		{Name: "Built-in Retina Display", Width: 3024, Height: 1964, Primary: true},
		{Name: "DELL U2417", Width: 1920, Height: 1200},
		{Name: "DELL U2720Q", Width: 3840, Height: 2160},
	}
	got, err := ChooseDisplay(displays, "")
	if err != nil {
		t.Fatalf("ChooseDisplay = %v", err)
	}
	if got.Name != "DELL U2720Q" {
		t.Errorf("chose %q, want the widest external", got.Name)
	}
}

func TestChooseDisplayFallsBackToThePrimary(t *testing.T) {
	displays := []Display{{Name: "Built-in", Width: 3024, Height: 1964, Primary: true}}
	got, err := ChooseDisplay(displays, "")
	if err != nil {
		t.Fatalf("ChooseDisplay = %v", err)
	}
	if got.Name != "Built-in" {
		t.Errorf("chose %q, want the primary", got.Name)
	}
}

// TestChooseDisplayTakesALoneExternal covers the machine with one display that
// is not marked primary: it still goes through the widest-external branch.
func TestChooseDisplayTakesALoneExternal(t *testing.T) {
	displays := []Display{{Name: "one", Width: 800, Height: 600}}
	got, err := ChooseDisplay(displays, "")
	if err != nil {
		t.Fatalf("ChooseDisplay = %v", err)
	}
	if got.Name != "one" {
		t.Errorf("chose %q, want the only display", got.Name)
	}
}

// TestChooseDisplayLastResort covers the machine that reports a display with no
// usable size and no primary flag. It is not a shape any real back-end should
// produce, which is exactly why the function must still hand back something
// instead of falling off the end.
func TestChooseDisplayLastResort(t *testing.T) {
	displays := []Display{{Name: "unsized"}, {Name: "other"}}
	got, err := ChooseDisplay(displays, "")
	if err != nil {
		t.Fatalf("ChooseDisplay = %v", err)
	}
	if got.Name != "unsized" {
		t.Errorf("chose %q, want the first display", got.Name)
	}
}

func TestChooseDisplayByName(t *testing.T) {
	displays := []Display{
		{Name: "Built-in Retina Display", Width: 3024, Height: 1964, Primary: true},
		{Name: "VITURE Beast", Width: 3840, Height: 1080},
	}
	got, err := ChooseDisplay(displays, "beast")
	if err != nil {
		t.Fatalf("ChooseDisplay = %v", err)
	}
	if got.Name != "VITURE Beast" {
		t.Errorf("chose %q", got.Name)
	}
}

// TestChooseDisplayRefusesAnAmbiguousName: taking over the wrong monitor full
// screen hijacks the machine the user is working on, so a coin toss is not an
// acceptable answer.
func TestChooseDisplayRefusesAnAmbiguousName(t *testing.T) {
	displays := []Display{
		{Name: "DELL U2720Q", Width: 3840, Height: 2160},
		{Name: "DELL U2417", Width: 1920, Height: 1200},
	}
	_, err := ChooseDisplay(displays, "dell")
	if err == nil {
		t.Fatal("an ambiguous name was resolved instead of refused")
	}
	if !strings.Contains(err.Error(), "matches 2 displays") {
		t.Errorf("error does not say what was ambiguous: %v", err)
	}
	// Both candidates must be named, so the user can pick.
	for _, want := range []string{"U2720Q", "U2417"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error omits candidate %q: %v", want, err)
		}
	}
}

func TestChooseDisplayReportsAMissingName(t *testing.T) {
	displays := []Display{{Name: "Built-in", Width: 3024, Height: 1964, Primary: true}}
	_, err := ChooseDisplay(displays, "beast")
	if err == nil {
		t.Fatal("a name matching nothing succeeded")
	}
	if !strings.Contains(err.Error(), "Built-in") {
		t.Errorf("error does not list what IS attached: %v", err)
	}
}

func TestScalingAdvice(t *testing.T) {
	// Glasses in a scaled mode: say so, and say what is being thrown away.
	got := ScalingAdvice(Display{Name: "VITURE Beast", Width: 2560, Height: 800, Scale: 2})
	if got == "" {
		t.Fatal("no advice for glasses in a scaled mode")
	}
	if !strings.Contains(got, "5120x1600") {
		t.Errorf("advice does not name the rendered size: %q", got)
	}

	// Nothing to say when the mode is native, or when it is not glasses.
	if s := ScalingAdvice(Display{Name: "VITURE Beast", Width: 3840, Height: 1080, Scale: 1}); s != "" {
		t.Errorf("advice offered for a native mode: %q", s)
	}
	if s := ScalingAdvice(Display{Name: "Built-in Retina Display", Width: 1512, Height: 982, Scale: 2}); s != "" {
		t.Errorf("advice offered about the user's own laptop screen: %q", s)
	}
}

func TestStereoMode(t *testing.T) {
	for _, tc := range []struct {
		name         string
		w, h         int
		stereoscopic bool
		eyeW, eyeH   int
	}{
		{"Beast 3D mode", 3840, 1080, true, 1920, 1080},
		{"Beast 2D mode", 1920, 1200, false, 1920, 1200},
		{"21:9 ultrawide is one eye", 3440, 1440, false, 3440, 1440},
		{"laptop", 3024, 1964, false, 3024, 1964},
		{"zero width", 0, 1080, false, 0, 0},
		{"zero height", 3840, 0, false, 0, 0},
		{"negative", -1, -1, false, 0, 0},
	} {
		s, w, h := StereoMode(tc.w, tc.h)
		if s != tc.stereoscopic || w != tc.eyeW || h != tc.eyeH {
			t.Errorf("%s: StereoMode(%d,%d) = (%v,%d,%d), want (%v,%d,%d)",
				tc.name, tc.w, tc.h, s, w, h, tc.stereoscopic, tc.eyeW, tc.eyeH)
		}
	}
}

// TestStereoModeKnownBlindSpot documents, as a test, the case no arithmetic on
// the panel size can get right: a genuine 32:9 monitor has the same shape as two
// 16:9 eyes. It is here so that anyone tempted to "fix" the threshold sees that
// the limitation is understood and deliberate.
func TestStereoModeKnownBlindSpot(t *testing.T) {
	if s, _, _ := StereoMode(5120, 1440); !s {
		t.Skip("threshold changed; re-read the comment on StereoMode")
	}
}
