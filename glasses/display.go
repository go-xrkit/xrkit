// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package glasses answers the questions an XR application has about the panel
// it is about to draw on: which of the attached displays is the headset, how
// wide the wearer's view of it actually is, and whether the mode it is in is
// carrying one eye or two.
//
// None of it involves a vendor SDK. XR glasses expose their 3D mode AS A DISPLAY
// MODE and their identity as a display NAME, so everything here is arithmetic
// over what the windowing system already reports.
//
// # What has actually been tested against hardware
//
// Almost every figure here was read off a specification sheet, and a
// specification sheet says nothing about the two things this package most needs
// to know: what the device calls itself, and what modes it offers. So each entry
// records a [Confidence], and today the honest list is short:
//
//   - VITURE Beast — Observed. Connected over DisplayPort, seen presenting a
//     3840x1080 side-by-side 3D mode and a 1920x1200 2D mode, and rendered to.
//   - VITURE Luma Ultra — Enumerated. Seen on the USB bus here as
//     35ca:1104 "VITURE Luma Ultra XR GLASSES". Its video was NOT connected, so
//     its display name and its modes are still unconfirmed.
//
// Everything else is Published: sourced, cited, and never plugged in by anyone
// here. Display names and USB ids for those came from decoded EDID binaries,
// kernel logs and other people's compositor configurations, and none of the
// artifacts behind them are from macOS — so a macOS display name should be
// treated as unverified until one is read.
//
// IF YOU WANT A MODEL SUPPORTED AND TESTED, SEND US ONE. We will gladly add it,
// plug it in, and move it up that list. Failing that, the next best thing is to
// declare it yourself and tell us what you saw.
//
// # Adding a model without rebuilding
//
// The built-in catalogue only names hardware somebody here has held or found a
// specification sheet for, and it is always going to be behind. So a person can
// declare their own model in an HCL file and it takes effect with no Go
// toolchain involved. The file lives at
//
//	~/Library/Application Support/go-xrkit/glasses.hcl   (macOS)
//	~/.config/go-xrkit/glasses.hcl                       (Linux, or $XDG_CONFIG_HOME)
//	%AppData%\go-xrkit\glasses.hcl                       (Windows)
//
// and $XRKIT_GLASSES_CATALOGUE overrides that. It looks like this:
//
//	glasses "ACME Visor 3" {
//	  # What I saw: connected over USB-C, display name "ACME Visor 3",
//	  # offered 3840x1080 side by side. The angle is ACME's own figure.
//	  source = "https://example.invalid/visor-3#specifications"
//
//	  match        = ["acme visor 3"]
//	  usb_vendor   = "0x2b41"
//	  usb_products = ["0x0110"]
//
//	  fov        = 46
//	  fov_axis   = "diagonal"
//	  eye_width  = 1920
//	  eye_height = 1080
//	}
//
// HCL because a figure here is only worth something with its provenance
// attached, and HCL has comments: what you measured goes next to the number you
// measured it from. See [LoadCatalogueFile] for the whole schema.
//
// An application reads it by calling [LoadUserCatalogue] once at start-up and
// showing the error if there is one. Having NO file is the normal case and is
// not an error, so that call costs a failed stat and nothing else; a file that
// exists and is wrong fails loudly, naming the file, the line and the block,
// because a catalogue line that quietly does nothing is the same invisible
// failure as a wrong angle.
//
// A model declared this way wins over a built-in entry with the same match, so
// a figure this package got wrong can be corrected on the machine that noticed
// it. [Register] does the same from Go, for an application that keeps its
// settings somewhere else entirely.
package glasses

import (
	"fmt"
	"strings"
)

// Display is the subset of a window back-end's screen description this package
// needs. It is declared here, rather than taken from go-widgets/window, so the
// choosing logic can be tested against displays that do not exist.
type Display struct {
	Name          string
	Width, Height int
	Primary       bool
	// Scale is the display's backing factor: framebuffer pixels per logical
	// point. 1 means the framebuffer matches the mode; 2 means macOS renders at
	// twice the size and scales down.
	Scale float64
}

// String renders the display as a person would identify it.
func (d Display) String() string {
	s := fmt.Sprintf("%q %dx%d", d.Name, d.Width, d.Height)
	if d.Primary {
		s += " (primary)"
	}
	return s
}

// ChooseDisplay picks the display to take over.
//
// want, when not empty, is matched case-insensitively against the display names
// and must match exactly one — an ambiguous request is an error rather than a
// coin toss, because going full screen on the wrong monitor takes over the
// machine the user was working on.
//
// With no preference the order is: a recognised pair of glasses; failing that
// the widest non-primary display, since an external panel is far more likely to
// be the intended target than the laptop the user is driving from; failing that
// the primary, so the thing still runs on one screen.
func ChooseDisplay(displays []Display, want string) (Display, error) {
	if len(displays) == 0 {
		return Display{}, fmt.Errorf("glasses: no displays attached")
	}
	if want != "" {
		var hits []Display
		for _, d := range displays {
			if strings.Contains(strings.ToLower(d.Name), strings.ToLower(want)) {
				hits = append(hits, d)
			}
		}
		switch len(hits) {
		case 1:
			return hits[0], nil
		case 0:
			return Display{}, fmt.Errorf("glasses: no display matches %q; attached: %s",
				want, describe(displays))
		default:
			return Display{}, fmt.Errorf("glasses: %q matches %d displays: %s",
				want, len(hits), describe(hits))
		}
	}
	for _, d := range displays {
		if _, ok := Identify(d.Name); ok {
			return d, nil
		}
	}
	best := Display{}
	for _, d := range displays {
		if !d.Primary && d.Width > best.Width {
			best = d
		}
	}
	if best.Width > 0 {
		return best, nil
	}
	for _, d := range displays {
		if d.Primary {
			return d, nil
		}
	}
	return displays[0], nil
}

// describe lists displays for an error message.
func describe(displays []Display) string {
	parts := make([]string, len(displays))
	for i, d := range displays {
		parts[i] = d.String()
	}
	return strings.Join(parts, ", ")
}

// ScalingAdvice returns a warning when a display is in a SCALED mode, or "" when
// there is nothing to say.
//
// macOS offers displays a choice of modes, and a scaled one renders at a larger
// framebuffer and downsamples onto the panel. On a laptop screen that is a
// deliberate legibility trade. On XR glasses it is loss for nothing: the panel
// has one fixed native resolution, nobody is reading small text on it, and every
// downsample softens the image the user came for. A VITURE Beast was observed
// reporting "5120x1600 rendered, looks like 2560x800" while its panel is
// 3840x1080 — three quarters of the rendered pixels thrown away.
//
// The advice is only offered for displays that look like glasses. An external
// monitor in a scaled mode is the user's business.
func ScalingAdvice(d Display) string {
	if d.Scale <= 1 {
		return ""
	}
	if _, ok := Identify(d.Name); !ok {
		return ""
	}
	return fmt.Sprintf(
		"%s is in a SCALED mode: %dx%d at %gx, so %dx%d is rendered and downsampled onto the panel. "+
			"Choose the panel's native mode in Displays settings for a sharper picture.",
		d.Name, d.Width, d.Height, d.Scale,
		int(float64(d.Width)*d.Scale), int(float64(d.Height)*d.Scale))
}

// StereoMode reports whether a display's dimensions look like a side-by-side 3D
// mode, and what one eye's viewport is.
//
// XR glasses expose their 3D mode AS A DISPLAY MODE: the VITURE Beast reports
// 3840x1080 for side-by-side 3D and 1920x1200 for ordinary 2D. So there is no
// SDK involved in getting stereo out — only the question of which mode the
// glasses are in, which is what this answers. A panel wider than 21:9 is
// carrying two eyes; anything else is one.
func StereoMode(w, h int) (stereoscopic bool, eyeW, eyeH int) {
	if w <= 0 || h <= 0 {
		return false, 0, 0
	}
	// The threshold sits at 3.0, between a 21:9 ultrawide (2.39 for the common
	// 3440x1440) and two 16:9 eyes side by side (3.56).
	//
	// It is a heuristic and it has a known blind spot: a genuine 32:9 monitor —
	// a Samsung Odyssey G9 is 5120x1440, also 3.56 — reads as stereoscopic. No
	// arithmetic on the panel size can separate those two, because they are the
	// same panel size. So this is a DEFAULT, and the caller can say otherwise.
	if float64(w)/float64(h) > 3.0 {
		return true, w / 2, h
	}
	return false, w, h
}
