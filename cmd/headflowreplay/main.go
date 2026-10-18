// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Command headflowreplay feeds a directory of captured frames through
// [headflow.Tracker] and says how far the view turned.
//
// # Why a directory of files
//
// The frames come from a camera this module cannot reach. go-xrkit/android
// drives a UVC camera over usbfs on an unrooted phone and writes what it
// decoded as PNGs; that module's only dependencies are golang.org/x/sys and
// go-appdirs/outdir, and it has no business acquiring this one's — HCL and eight
// indirect modules, for a tracker. So a FILE is the interface between them:
// image/png is in the standard library on both sides, the two halves share no
// code at all, and a person can look at the pictures.
//
//	go run ./cmd/headflowreplay ~/Library/Application\ Support/go-xrkit-android/frames
//
// # What it is for
//
// Separating a tracker that cannot read a scene from a scene that cannot be
// tracked. Held on a head the method's residual is 1.14% of the distance
// travelled; on frames of a blank wall it refuses, and SAYING SO is the
// behaviour [headflow.MinConfidence] exists for. Replaying lets the two be told
// apart without a headset on.
//
// ⛔ THE FRAMES ARE PICTURES OF A ROOM. Whoever captured them chose somewhere
// outside every work tree to put them, and this only reads. It prints the
// directory it read so that deleting them afterwards is one copy away.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-xrkit/xrkit/headflow"
)

// The three things main does that a test cannot otherwise reach.
//
// ⛔ THEY ARE SEAMS BECAUSE os.Exit ENDS THE PROCESS, and a main left
// uncoverable is a main nobody has run — this module gates every package at
// 100% precisely so that an entry point cannot quietly stop working while the
// package under it is green. Replacing these lets a test assert the thing that
// matters about a command's failure: that it SAYS what went wrong and exits
// non-zero. The same seam shape go-xrkit/android uses for ioctl and getwd.
var (
	exit             = os.Exit
	stderr io.Writer = os.Stderr
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(stderr, "headflowreplay:", err)
		exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs0 := flag.NewFlagSet("headflowreplay", flag.ContinueOnError)
	fs0.SetOutput(out)
	quiet := fs0.Bool("quiet", false, "print only the summary, not every frame")
	if err := fs0.Parse(args); err != nil {
		return err
	}
	if fs0.NArg() != 1 {
		return fmt.Errorf("give one directory of frame-*.png files; got %d argument(s)",
			fs0.NArg())
	}
	dir := fs0.Arg(0)

	names, err := framesIn(dir)
	if err != nil {
		return err
	}
	// ⛔ TWO FRAMES ARE THE MINIMUM AND ONE IS NOT A MEASUREMENT. The first
	// frame establishes the reference and reports no motion of its own, so a
	// single file would print a confident zero — which is the shape of an answer
	// and not one.
	if len(names) < 2 {
		return fmt.Errorf("%s holds %d frame(s); a turn needs at least two, because the "+
			"first one establishes the reference and moves nothing", dir, len(names))
	}
	fmt.Fprintf(out, "replaying %d frame(s) from %s\n", len(names), dir)

	var tr headflow.Tracker
	var travel, prev float64
	used, refused, references := 0, 0, 0
	for _, name := range names {
		im, w, err := readFrame(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		yaw, ok := tr.Feed(im)
		deg := yaw * 180 / math.Pi
		// ⛔⛔ A REFERENCE FRAME IS NOT A REFUSAL, AND CALLING IT ONE READS AS A
		// FAULT. headflow returns ok=false for two unrelated things: a frame it
		// could not match, and a frame that became the new reference — which
		// moves nothing BY DEFINITION, there being nothing yet to compare it
		// against. The library distinguishes them and this used to flatten
		// both into "refused  confidence 0.000", which is the first line
		// anybody reads and looked like a broken tracker.
		//
		// Unusable() is the signal: establishing a reference RESETS it, while a
		// frame refused for want of confidence increments it.
		state := "tracking"
		switch {
		case ok:
			used++
			travel += math.Abs(deg - prev)
			prev = deg
		case tr.Unusable() == 0:
			references++
			state = "reference"
		default:
			refused++
			state = "refused"
		}
		if !*quiet {
			fmt.Fprintf(out, "  %-16s %5d px wide  yaw %+8.3f°  confidence %.3f  %s\n",
				name, w, deg, tr.Confidence(), state)
		}
	}

	// ⛔ THE COUNTS ADD UP, which is this fleet's oldest rule about a census: a
	// frame is tracked, or it established a reference, or it was refused.
	fmt.Fprintf(out, "\n%d frame(s) of %d tracked; %d established a reference, "+
		"%d refused for want of confidence\n",
		used, len(names), references, refused)
	fmt.Fprintf(out, "net yaw %+.3f°, total travel %.3f°\n", prev, travel)
	switch {
	case used == 0:
		// ⛔ A REFUSAL IS AN ANSWER AND NOT A FAILURE OF THE TOOL. Below
		// MinConfidence there is nothing wrong with the arithmetic — the picture
		// has nothing in it to match, which is what a dark room or a blank wall
		// looks like. Saying so is what lets somebody be told "it is too dark"
		// instead of being shown a drifting number.
		fmt.Fprintf(out, "⛔ NO FRAME COULD BE MATCHED. That is the scene rather than the "+
			"tracker: below a confidence of %.1f there is nothing in the picture to "+
			"match, which is what a blank wall or a dark room looks like.\n",
			headflow.MinConfidence)
	case travel == 0:
		fmt.Fprintf(out, "⚠ THE VIEW DID NOT MOVE. Every frame matched and none of them "+
			"differed, so the camera was still — which is worth knowing, because a "+
			"tracker that invented movement would show it here.\n")
	default:
		fmt.Fprintf(out, "⭐ the view turned. %.1f° of travel over %d frames, and the "+
			"calibration behind it is a field of view rather than a column count, so "+
			"this reads the same at any frame width.\n", travel, used)
	}
	return nil
}

// framesIn lists the frame files in a directory, in the order they were
// captured.
//
// ⛔ SORTED, AND THAT IS NOT COSMETIC. A tracker matches each frame against the
// one before it, so the ORDER is the measurement: read them as the filesystem
// happens to hand them over and the yaw is a walk through a shuffled deck. The
// writer numbers them with a fixed width for exactly this reason, so a plain
// sort is the capture order.
func framesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, "frame-") || !strings.HasSuffix(n, ".png") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// readFrame reads one PNG and returns it as the RGBA [headflow.Profile] reads,
// with its width.
//
// ⭐ THE GREY GOES INTO ALL THREE CHANNELS, which is what the writer meant: it
// stored grey because Profile sums R+G+B down each column and the luminance is
// all of that sum which carries the motion. Expanding it here costs one pass and
// keeps Profile unchanged.
func readFrame(path string) (*image.RGBA, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	im, err := png.Decode(f)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	b := im.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if g, ok := im.(*image.Gray); ok {
		for y := 0; y < b.Dy(); y++ {
			src := g.Pix[y*g.Stride:]
			row := out.Pix[y*out.Stride:]
			for x := 0; x < b.Dx(); x++ {
				l := src[x]
				i := x * 4
				row[i], row[i+1], row[i+2], row[i+3] = l, l, l, 0xFF
			}
		}
		return out, b.Dx(), nil
	}
	// Any other PNG a person points this at: a picture is a picture, and
	// refusing one over its colour model would be fussiness rather than care.
	for y := 0; y < b.Dy(); y++ {
		row := out.Pix[y*out.Stride:]
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, _ := im.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := x * 4
			row[i], row[i+1], row[i+2], row[i+3] =
				byte(r>>8), byte(g>>8), byte(bl>>8), 0xFF
		}
	}
	return out, b.Dx(), nil
}
