// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scene writes one grey PNG the way go-xrkit/android writes them: a frame of a
// room, shifted sideways by `shift` columns, with grain of its own.
//
// ⛔ THE GRAIN MATTERS. A noiseless gradient matches itself at every offset, so
// a tracker fed one reports a confident nothing — and a test built on one would
// pass whatever the tracker did. The grain is redrawn per frame so that what
// survives the match is the SHIFT and not the speckle.
func scene(t testing.TB, dir, name string, w, h, shift, seed int) {
	t.Helper()
	im := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Structure that varies down the frame as well as across it, so a
			// column profile has something to lock to.
			sx := x - shift
			v := 0
			if sx >= 0 && sx < w {
				v = (sx*7)%97 + (sx/23)*11
			}
			n := (x*31 + y*17 + seed*101) % 37
			im.SetGray(x, y, color.Gray{Y: uint8((v + n) % 200)})
		}
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// blank writes a frame with nothing in it to match: one flat value.
func blank(t testing.TB, dir, name string, w, h int) {
	t.Helper()
	im := image.NewGray(image.Rect(0, 0, w, h))
	for i := range im.Pix {
		im.Pix[i] = 0x40
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestASLIDINGSCENEREADSASATURN.
//
// ⭐ THIS IS WHAT THE TOOL IS FOR: a sequence of frames that slid sideways must
// come out as degrees. Nothing here asserts a particular number of degrees —
// that is headflow's own calibration and has its own tests — only that the
// travel is real, in the right direction, and reported.
func TestASlidingSceneReadsAsATurn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The scene slides RIGHT across the picture, so the view turned LEFT.
	for i := 0; i < 6; i++ {
		scene(t, dir, fmt.Sprintf("frame-%03d.png", i+1), 640, 240, i*12, i+1)
	}
	var out bytes.Buffer
	if err := run([]string{dir}, &out); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"replaying 6 frame(s)", "the view turned", "total travel"} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}
	// ⛔ AND THE DIRECTION, which is invisible to any check on magnitudes: a
	// scene sliding right means the view turned LEFT, so the net yaw is
	// negative. A tracker with this backwards feels exactly as responsive.
	if !strings.Contains(got, "net yaw -") {
		t.Errorf("the scene slid right and the net yaw is not negative:\n%s", got)
	}
	// ⛔⛔ THE FIRST FRAME IS A REFERENCE AND NOT A REFUSAL. It moves nothing by
	// definition — there is nothing yet to compare it against — and the tool used
	// to print "refused  confidence 0.000" for it, which is the first line
	// anybody reads and looked like a broken tracker. The counts must also add
	// up: five tracked, one reference, none refused, of six.
	if !strings.Contains(got, "5 frame(s) of 6 tracked; 1 established a reference, "+
		"0 refused") {
		t.Errorf("the census does not add up, or a frame with structure was refused:\n%s",
			got)
	}
	// The state is the last thing on a frame's line; matching the bare word
	// would find the summary line instead and prove nothing.
	if !strings.Contains(got, "  reference\n") {
		t.Errorf("the reference frame is not named as one on its own line:\n%s", got)
	}
	if strings.Contains(got, "  refused\n") {
		t.Errorf("a frame with structure in it was refused:\n%s", got)
	}
}

// TestABLANKSCENEISREFUSEDANDSAIDSO.
//
// ⛔⛔ THE WHOLE POINT OF REPLAYING IS TO TELL A TRACKER THAT CANNOT READ A SCENE
// FROM A SCENE THAT CANNOT BE TRACKED. Below headflow.MinConfidence there is
// nothing wrong with the arithmetic — the picture has nothing in it to match,
// which is what a dark room or a blank wall looks like. A tool that printed a
// confident zero for that would be indistinguishable from one that was working.
func TestABlankSceneIsRefusedAndSaidSo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		blank(t, dir, fmt.Sprintf("frame-%03d.png", i+1), 640, 240)
	}
	var out bytes.Buffer
	if err := run([]string{dir}, &out); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "NO FRAME COULD BE MATCHED") {
		t.Errorf("a blank scene was not reported as unmatchable:\n%s", got)
	}
	// ⭐ AND THEY ARE "refused", NOT "reference" — which is the other half of
	// that distinction working. A frame with nothing in it cannot become a
	// reference either: headflow judges a frame on ITSELF before it judges it
	// against another, so a blank one is refused outright and the census says
	// four refused and no reference established.
	if !strings.Contains(got, "0 frame(s) of 4 tracked; 0 established a reference, "+
		"4 refused") {
		t.Errorf("a blank scene produced usable frames, or the census does not add "+
			"up:\n%s", got)
	}
	// ⚠ THE STATE IS THE LAST THING ON A FRAME'S LINE, and that is what is
	// matched — the summary line says "established a reference" too, so looking
	// for the bare word found the summary and the check passed for the wrong
	// reason.
	if strings.Contains(got, "  reference\n") {
		t.Errorf("a blank frame was called a reference; it has nothing in it to be one "+
			"OF:\n%s", got)
	}
}

// A camera that did not move: every frame matches and none differs. Worth its
// own case, because a tracker that invented movement would show it here.
func TestAStillCameraReportsNoMovement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		// The same shift every time, and the SAME grain, so the frames are
		// identical: a still camera delivering the same picture.
		scene(t, dir, fmt.Sprintf("frame-%03d.png", i+1), 640, 240, 0, 7)
	}
	var out bytes.Buffer
	if err := run([]string{dir}, &out); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "THE VIEW DID NOT MOVE") {
		t.Errorf("identical frames were not reported as still:\n%s", got)
	}
}

func TestTheFramesAreReadInCaptureOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Ten frames, so that a lexical sort and a numeric one would differ if the
	// writer had not padded the numbers — which it does, and this is the test
	// that would notice if it stopped.
	for i := 0; i < 12; i++ {
		scene(t, dir, fmt.Sprintf("frame-%03d.png", i+1), 320, 120, i*8, i+1)
	}
	names, err := framesIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 12 {
		t.Fatalf("found %d frames, want 12", len(names))
	}
	for i, n := range names {
		if want := fmt.Sprintf("frame-%03d.png", i+1); n != want {
			t.Fatalf("frame %d is %s, want %s — the order IS the measurement, since "+
				"each frame is matched against the one before it", i, n, want)
		}
	}
	// Files that are not frames are left alone, so a directory somebody also
	// put a transcript in still replays.
	if err := os.WriteFile(filepath.Join(dir, "usb.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "frame-999.png.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	again, err := framesIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 12 {
		t.Errorf("found %d frames after adding a transcript and a directory, want 12",
			len(again))
	}
}

func TestWhatItRefusesToDo(t *testing.T) {
	t.Parallel()
	t.Run("no directory given", func(t *testing.T) {
		var out bytes.Buffer
		if err := run(nil, &out); err == nil {
			t.Error("run with no arguments was accepted")
		}
	})
	t.Run("two directories given", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"a", "b"}, &out); err == nil {
			t.Error("run with two directories was accepted")
		}
	})
	t.Run("a flag it does not have", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"-nonesuch", "x"}, &out); err == nil {
			t.Error("an unknown flag was accepted")
		}
	})
	t.Run("a directory that is not there", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{filepath.Join(t.TempDir(), "nope")}, &out); err == nil {
			t.Error("a missing directory was accepted")
		}
	})
	t.Run("one frame is not a turn", func(t *testing.T) {
		// ⛔ THE FIRST FRAME ESTABLISHES THE REFERENCE AND MOVES NOTHING, so one
		// file would print a confident zero — the shape of an answer rather than
		// one.
		dir := t.TempDir()
		scene(t, dir, "frame-001.png", 640, 240, 0, 1)
		var out bytes.Buffer
		err := run([]string{dir}, &out)
		if err == nil {
			t.Fatal("a single frame was accepted as a turn")
		}
		if !strings.Contains(err.Error(), "at least two") {
			t.Errorf("the refusal does not say why: %v", err)
		}
	})
	t.Run("a PNG that is not one", func(t *testing.T) {
		dir := t.TempDir()
		scene(t, dir, "frame-001.png", 640, 240, 0, 1)
		if err := os.WriteFile(filepath.Join(dir, "frame-002.png"),
			[]byte("not a png at all"), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err := run([]string{dir}, &out)
		if err == nil {
			t.Fatal("a file that is not a PNG was decoded")
		}
		if !strings.Contains(err.Error(), "frame-002.png") {
			t.Errorf("the error does not name the file: %v", err)
		}
	})
	// ⛔⛔ A PATH THAT IS NOT THERE, AND readFrame CALLED DIRECTLY. The first
	// version chmod'ed a frame to 0 and went through run, which cost two lanes
	// at once: Windows has no POSIX permission bits so the read SUCCEEDED, and
	// root ignores the mode so the test skipped itself in a container and took
	// the coverage with it. A missing file fails on every platform and for every
	// user, and calling the function that opens it needs no directory listing to
	// cooperate. Its propagation through run is covered by the PNG that is not
	// one, below.
	t.Run("a frame that is not there", func(t *testing.T) {
		if _, _, err := readFrame(filepath.Join(t.TempDir(), "frame-404.png")); err == nil {
			t.Error("readFrame accepted a path with no file at it")
		}
	})
}

// -quiet prints the summary and not the per-frame lines, which is what a long
// capture wants.
func TestQuietPrintsTheSummaryOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		scene(t, dir, fmt.Sprintf("frame-%03d.png", i+1), 640, 240, i*12, i+1)
	}
	var loud, quiet bytes.Buffer
	if err := run([]string{dir}, &loud); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-quiet", dir}, &quiet); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loud.String(), "frame-002.png") {
		t.Error("the loud run did not name each frame")
	}
	if strings.Contains(quiet.String(), "frame-002.png") {
		t.Error("the quiet run named a frame")
	}
	for _, want := range []string{"net yaw", "total travel"} {
		if !strings.Contains(quiet.String(), want) {
			t.Errorf("the quiet run is missing %q:\n%s", want, quiet.String())
		}
	}
}

// TestACOLOURPNGISALSOAPICTURE: the writer stores grey, but a person may point
// this at frames from anywhere, and refusing one over its colour model would be
// fussiness rather than care.
func TestAColourPNGIsAlsoAPicture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		im := image.NewRGBA(image.Rect(0, 0, 640, 240))
		for y := 0; y < 240; y++ {
			for x := 0; x < 640; x++ {
				sx := x - i*12
				v := 0
				if sx >= 0 && sx < 640 {
					v = (sx*7)%97 + (sx/23)*11
				}
				n := (x*31 + y*17 + (i+1)*101) % 37
				l := uint8((v + n) % 200)
				im.Set(x, y, color.RGBA{R: l, G: l, B: uint8(y), A: 0xFF})
			}
		}
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("frame-%03d.png", i+1)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, im); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := run([]string{dir}, &out); err != nil {
		t.Fatalf("run on colour frames: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "the view turned") {
		t.Errorf("a colour sequence did not read as a turn:\n%s", out.String())
	}
}

// readFrame's width is reported so that a reader can see which calibration
// applied: headflow derives columns-per-degree FROM THE WIDTH, so a 320-wide
// frame and a 640-wide one read the same turn as the same angle.
func TestTheWidthIsReportedBecauseTheCalibrationDependsOnIt(t *testing.T) {
	t.Parallel()
	for _, w := range []int{320, 640} {
		dir := t.TempDir()
		for i := 0; i < 3; i++ {
			// The same FRACTION of the frame each time, which is the same
			// physical turn at either width.
			scene(t, dir, fmt.Sprintf("frame-%03d.png", i+1), w, 240, i*w/40, i+1)
		}
		var out bytes.Buffer
		if err := run([]string{dir}, &out); err != nil {
			t.Fatalf("run at %d wide: %v", w, err)
		}
		if !strings.Contains(out.String(), fmt.Sprintf("%5d px wide", w)) {
			t.Errorf("the %d-wide run does not report the width:\n%s", w, out.String())
		}
	}
}

// TestMainSaysWhatWentWrongAndExitsNonZero.
//
// ⛔ A main NOBODY HAS RUN IS NOT KNOWN TO WORK, which is why this module gates
// every package and not only the libraries: an entry point can quietly stop
// working while everything under it stays green. os.Exit would end the test
// process, so it and stderr are seams — and what gets asserted is the behaviour
// that matters about a command's failure, that it NAMES the problem and exits
// non-zero rather than succeeding silently.
//
// ⚠ NOT PARALLEL: it replaces package-level variables and os.Args.
func TestMainSaysWhatWentWrongAndExitsNonZero(t *testing.T) {
	oldExit, oldErr, oldArgs := exit, stderr, os.Args
	t.Cleanup(func() { exit, stderr, os.Args = oldExit, oldErr, oldArgs })

	t.Run("a run that succeeds neither prints nor exits", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < 3; i++ {
			scene(t, dir, fmt.Sprintf("frame-%03d.png", i+1), 320, 120, i*8, i+1)
		}
		var errs bytes.Buffer
		codes := []int{}
		stderr, exit = &errs, func(c int) { codes = append(codes, c) }
		os.Args = []string{"headflowreplay", "-quiet", dir}
		main()
		if len(codes) != 0 {
			t.Errorf("a successful run exited with %v", codes)
		}
		if errs.Len() != 0 {
			t.Errorf("a successful run wrote to stderr: %q", errs.String())
		}
	})

	t.Run("a run that fails says so and exits 1", func(t *testing.T) {
		var errs bytes.Buffer
		codes := []int{}
		stderr, exit = &errs, func(c int) { codes = append(codes, c) }
		os.Args = []string{"headflowreplay", filepath.Join(t.TempDir(), "nope")}
		main()
		if len(codes) != 1 || codes[0] != 1 {
			t.Errorf("a failing run exited with %v, want exactly [1]", codes)
		}
		if !strings.Contains(errs.String(), "headflowreplay:") {
			t.Errorf("the failure does not name the command: %q", errs.String())
		}
	})
}

func TestDegreesAndRadiansAreNotMixedUp(t *testing.T) {
	t.Parallel()
	// A sanity bound rather than a value: a handful of frames of a scene sliding
	// a fifth of the frame cannot be hundreds of degrees, and a radians-labelled
	// -as-degrees bug is exactly that size.
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		scene(t, dir, fmt.Sprintf("frame-%03d.png", i+1), 640, 240, i*24, i+1)
	}
	var out bytes.Buffer
	if err := run([]string{"-quiet", dir}, &out); err != nil {
		t.Fatal(err)
	}
	var travel float64
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "net yaw") {
			if _, err := fmt.Sscanf(line, "net yaw %v°, total travel %v°", new(float64),
				&travel); err != nil {
				t.Fatalf("could not read the summary line %q: %v", line, err)
			}
		}
	}
	if travel <= 0 || travel > 90 || math.IsNaN(travel) {
		t.Errorf("total travel %v° over five frames is not a plausible head turn", travel)
	}
}
