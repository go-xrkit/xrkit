// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package glasses

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// The built-in catalogue can only ever name hardware somebody here has heard
// of, and new glasses appear faster than releases do. So a person can declare
// their own model — in a file, or from Go — and it takes effect without a Go
// toolchain and without a rebuild.
//
// userCatalogue holds those declarations. It is the package's only mutable
// state, so every read and every write goes through userMu: identification runs
// on whichever goroutine owns the display, while a load happens on whichever one
// handles configuration, and those are not the same one.
var (
	userMu        sync.RWMutex
	userCatalogue []Profile
)

// EnvCatalogue names the environment variable that overrides which file
// [LoadUserCatalogue] reads. It exists so a test, or a person trying a figure
// out, can point at a scratch file without touching the real one.
const EnvCatalogue = "XRKIT_GLASSES_CATALOGUE"

// CataloguePath reports the file [LoadUserCatalogue] reads.
//
// That is $XRKIT_GLASSES_CATALOGUE when it is set, and otherwise glasses.hcl
// under the platform's own configuration directory:
//
//	~/Library/Application Support/go-xrkit/glasses.hcl   (macOS)
//	~/.config/go-xrkit/glasses.hcl                       (Linux, or $XDG_CONFIG_HOME)
//	%AppData%\go-xrkit\glasses.hcl                       (Windows)
//
// It fails only when the platform cannot say where a user's configuration
// lives, which on a Unix means HOME is unset.
func CataloguePath() (string, error) {
	if path := os.Getenv(EnvCatalogue); path != "" {
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("glasses: cannot locate the user catalogue: %w", err)
	}
	return filepath.Join(dir, "go-xrkit", "glasses.hcl"), nil
}

// LoadUserCatalogue reads the file named by [CataloguePath] and adds the models
// it declares.
//
// A MISSING FILE IS NOT AN ERROR. Having no file at all is the normal case —
// the built-in catalogue is what nearly everybody runs on — so an application
// can call this unconditionally at start-up and cost itself one failed stat.
//
// A file that exists and is wrong IS an error, and the application should print
// it: the diagnostics name the file, the line and the block. Somebody who wrote
// a catalogue file meant it to take effect, and a mistyped attribute that
// quietly does nothing is the same silent failure this package refuses
// everywhere else — everything still renders, in the wrong place, with nothing
// to see.
func LoadUserCatalogue() error {
	path, err := CataloguePath()
	if err != nil {
		return err
	}
	if err := LoadCatalogueFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// LoadCatalogueFile reads a catalogue from an explicit path, for an application
// that keeps its configuration somewhere of its own.
//
// The file is HCL, one block per model. A worked example, which is also the
// shape of a useful contribution back:
//
//	glasses "ACME Visor 3" {
//	  # Measured here: connected over USB-C, reported the display name below,
//	  # and offered 3840x1080 side by side. The angle is ACME's own:
//	  source = "https://example.invalid/visor-3#specifications"
//
//	  # How to recognise it. match are substrings of the DISPLAY name; exact
//	  # are whole display names, for names too short to be safe as substrings.
//	  match = ["acme visor 3"]
//	  exact = ["visor 3"]
//
//	  # How to recognise it over USB, which is the only way to tell some
//	  # models apart. Ids are hexadecimal, as lsusb and Windows print them.
//	  usb_vendor   = "0x2b41"
//	  usb_products = ["0x0110"]
//	  usb_match    = ["acme visor 3"]
//
//	  # The geometry. fov_axis is REQUIRED whenever fov is given.
//	  fov        = 46
//	  fov_axis   = "diagonal"
//	  eye_width  = 1920
//	  eye_height = 1080
//	}
//
//	glasses "ACME glasses" {
//	  # No published figure was found, so none is claimed. This still says
//	  # "these are glasses", which is what display selection needs.
//	  match = ["acme"]
//	}
//
// The block label is the name shown to a person. Every model needs at least one
// way to be recognised. The second block is a FAMILY entry: it leaves the
// figures out, which is how the file says "these are glasses and I do not know
// their optics" — the honest answer, and the one that makes [Profile.FOV]
// refuse rather than invent.
//
// HCL rather than a format the standard library parses, because a figure in
// this package is only worth something with its provenance attached, and HCL has
// COMMENTS: a person can write down what they measured and where a number came
// from, right next to it, exactly as the built-in catalogue does. source says
// the same thing in a field, so it is structured rather than folklore.
//
// fov_axis is required alongside fov, and that is deliberate: manufacturers
// print a bare angle and almost never say which one it is, and a horizontal
// figure read as a diagonal makes every angle too small — a picture in the
// wrong place, with nothing to see. Writing "unstated" is allowed and honest;
// it keeps the number visible while [Profile.Known] reports false.
//
// The schema is declared, so an unknown attribute is an ERROR rather than
// something quietly dropped — "fovaxis" silently becoming a family entry is
// precisely the failure that has no symptom. Every problem in the file is
// reported at once, each with its line, and NOTHING is registered if any of
// them is an error: loading half a catalogue would leave a machine running on a
// mixture of what the person wrote and what they thought they wrote.
//
// Entries add to whatever is already registered; they do not replace it.
func LoadCatalogueFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		// Wrapped, so LoadUserCatalogue can still recognise fs.ErrNotExist.
		return fmt.Errorf("glasses: %w", err)
	}
	profiles, err := parseCatalogue(path, data)
	if err != nil {
		return err
	}
	addProfiles(profiles...)
	return nil
}

// Declaration is one model as a caller declares it, for an application that
// gets its configuration from somewhere this package cannot read — a server, a
// database, its own settings file.
//
// A Declaration with no FOV is a FAMILY entry: it identifies the hardware
// without claiming to know its optics, and [Profile.Known] will report false so
// the caller asks rather than guesses. That is a correct answer, not a
// placeholder to be filled in with a plausible angle later.
type Declaration struct {
	// Model is the name to show a person. Required.
	Model string

	// Match are substrings of the display name; Exact are whole display names,
	// for names too short to be safe as substrings. USBVendor with USBProducts
	// identifies the device on the USB bus, and USBMatch are substrings of its
	// product string. At least one of these must be given, or nothing would
	// ever select the model.
	Match, Exact []string
	USBVendor    uint16
	USBProducts  []uint16
	USBMatch     []string

	// FOV is the manufacturer's published angle in degrees, and Axis is which
	// angle it spans. Leave FOV zero when it is not known. Set Axis to
	// [AxisUnstated] when a figure was published without saying: the number is
	// then kept and reported, but not used.
	FOV  float64
	Axis Axis

	// EyeWidth and EyeHeight are one eye's native panel, both or neither.
	EyeWidth, EyeHeight int

	// Source is where the figures came from, so the next person can check them.
	Source string
}

// Register adds a model declared from Go. It is safe to call from several
// goroutines, and from any of them at any time.
func Register(d Declaration) error {
	p, err := d.profile()
	if err != nil {
		return fmt.Errorf("glasses: %q: %w", d.Model, err)
	}
	addProfiles(p)
	return nil
}

// profile validates a Declaration. The file path does its own checking rather
// than calling this, because a diagnostic has to carry the SOURCE RANGE of the
// thing that is wrong, and by the time a value has been decoded into a Go field
// that range is gone.
func (d Declaration) profile() (Profile, error) {
	p := Profile{
		Model:       strings.TrimSpace(d.Model),
		usbVendor:   d.USBVendor,
		usbProducts: d.USBProducts,
		Axis:        d.Axis,
		EyeWidth:    d.EyeWidth,
		EyeHeight:   d.EyeHeight,
		Source:      d.Source,
	}
	if p.Model == "" {
		return Profile{}, errors.New("the model has no name")
	}
	var err error
	if p.match, err = cleanMatches("Match", d.Match); err != nil {
		return Profile{}, err
	}
	if p.exact, err = cleanMatches("Exact", d.Exact); err != nil {
		return Profile{}, err
	}
	if p.usbMatch, err = cleanMatches("USBMatch", d.USBMatch); err != nil {
		return Profile{}, err
	}
	if len(p.match)+len(p.exact)+len(p.usbMatch)+len(p.usbProducts) == 0 && p.usbVendor == 0 {
		return Profile{}, errors.New("no way to recognise it, so no device would ever select this model")
	}
	if len(p.usbProducts) > 0 && p.usbVendor == 0 {
		return Profile{}, errors.New("USBProducts without a USBVendor, and a product id means nothing on its own")
	}
	// Written as a positive test so that a NaN, which compares false against
	// everything, is rejected rather than stored.
	if d.FOV != 0 && !(d.FOV > 0 && d.FOV < 180) {
		return Profile{}, fmt.Errorf("a field of view of %v is not an angle between 0 and 180", d.FOV)
	}
	p.PublishedFOV = d.FOV
	if d.EyeWidth < 0 || d.EyeHeight < 0 || (d.EyeWidth == 0) != (d.EyeHeight == 0) {
		return Profile{}, fmt.Errorf("%dx%d is not a panel size; give both sides, or neither",
			d.EyeWidth, d.EyeHeight)
	}
	return p, nil
}

// cleanMatches lower-cases and trims a list of match strings, refusing any that
// is empty. strings.Contains reports true for the empty string against every
// name, so one blank entry would claim the laptop's own screen is a headset.
func cleanMatches(field string, in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, len(in))
	for i, m := range in {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			return nil, fmt.Errorf("%s[%d] is empty, and an empty string matches everything", field, i)
		}
		out[i] = m
	}
	return out, nil
}

// addProfiles appends to the user catalogue under the write lock.
func addProfiles(profiles ...Profile) {
	userMu.Lock()
	defer userMu.Unlock()
	userCatalogue = append(userCatalogue, profiles...)
}

// The catalogue file's schema, declared rather than inferred so that anything
// the file says which this package does not understand is reported instead of
// ignored.
var (
	fileSchema = &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{{Type: "glasses", LabelNames: []string{"model"}}},
	}
	entrySchema = &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "match"}, {Name: "exact"},
			{Name: "usb_vendor"}, {Name: "usb_products"}, {Name: "usb_match"},
			{Name: "fov"}, {Name: "fov_axis"},
			{Name: "eye_width"}, {Name: "eye_height"},
			{Name: "source"},
		},
	}
	// axes is the spelling of each Axis in a file. "unstated" is offered on
	// purpose: it is how a person records an angle a manufacturer published
	// without saying which one it is, instead of quietly picking.
	axes = map[string]Axis{
		"unstated":   AxisUnstated,
		"diagonal":   AxisDiagonal,
		"horizontal": AxisHorizontal,
	}
)

// parseCatalogue turns a file's bytes into profiles, or into an error carrying
// every diagnostic the file produced.
//
// Diagnostics are ACCUMULATED rather than returned at the first one: a person
// editing a configuration file would rather be told about all four mistakes
// than run the program four times.
func parseCatalogue(path string, data []byte) ([]Profile, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(data, path)
	if diags.HasErrors() {
		// A syntax error leaves nothing worth walking.
		return nil, diagError(parser.Files(), diags)
	}
	content, diags := file.Body.Content(fileSchema)
	profiles := make([]Profile, 0, len(content.Blocks))
	for _, block := range content.Blocks {
		p, blockDiags := blockProfile(block)
		diags = append(diags, blockDiags...)
		profiles = append(profiles, p)
	}
	if diags.HasErrors() {
		return nil, diagError(parser.Files(), diags)
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("glasses: %s: declares no glasses", path)
	}
	return profiles, nil
}

// blockProfile validates one glasses block. It returns whatever it could make
// sense of alongside the diagnostics; the caller discards the lot if any of
// them is an error.
func blockProfile(block *hcl.Block) (Profile, hcl.Diagnostics) {
	content, diags := block.Body.Content(entrySchema)
	p := Profile{Model: strings.TrimSpace(block.Labels[0])}
	if p.Model == "" {
		diags = append(diags, badBlock(block, "Model with no name",
			"A glasses block is labelled with the name to show a person, and that name cannot be blank."))
	}
	p.match, diags = matchAttr(content, "match", diags)
	p.exact, diags = matchAttr(content, "exact", diags)
	p.usbMatch, diags = matchAttr(content, "usb_match", diags)
	var vendors []uint16
	vendors, diags = usbIDs(content, "usb_vendor", diags)
	if len(vendors) == 1 {
		p.usbVendor = vendors[0]
	}
	p.usbProducts, diags = usbIDs(content, "usb_products", diags)
	if len(p.usbProducts) > 0 && p.usbVendor == 0 {
		diags = append(diags, badBlock(block, "Product ids with no vendor",
			"usb_products needs a usb_vendor: a product id means nothing on its own."))
	}
	diags = append(diags, stringAttr(content, "source", &p.Source)...)
	diags = fovAttrs(content, block, &p, diags)
	diags = panelAttrs(content, block, &p, diags)
	if len(p.match)+len(p.exact)+len(p.usbMatch)+len(p.usbProducts) == 0 && p.usbVendor == 0 {
		diags = append(diags, badBlock(block, "Model that nothing can select",
			"Give at least one of match, exact, usb_vendor, usb_products or usb_match, "+
				"or no display and no device would ever be identified as this model."))
	}
	return p, diags
}

// fovAttrs decodes fov and fov_axis, which travel together: an angle nobody
// says the axis of is not usable, so writing one without the other is refused
// rather than quietly assumed to be a diagonal.
func fovAttrs(content *hcl.BodyContent, block *hcl.Block, p *Profile, diags hcl.Diagnostics) hcl.Diagnostics {
	attr, haveFOV := content.Attributes["fov"]
	if haveFOV {
		var fov float64
		attrDiags := gohcl.DecodeExpression(attr.Expr, nil, &fov)
		diags = append(diags, attrDiags...)
		switch {
		case attrDiags.HasErrors():
		case !(fov > 0 && fov < 180):
			diags = append(diags, badAttr(attr, "Not a field of view", fmt.Sprintf(
				"fov is %v. It is the manufacturer's published angle in degrees, so it lies "+
					"between 0 and 180. Leave it out when it is not known: a model with no "+
					"figure is honest, and a wrong one fails silently.", fov)))
		default:
			p.PublishedFOV = fov
		}
	}
	axisAttr, haveAxis := content.Attributes["fov_axis"]
	if haveAxis {
		var name string
		attrDiags := gohcl.DecodeExpression(axisAttr.Expr, nil, &name)
		diags = append(diags, attrDiags...)
		axis, known := axes[strings.ToLower(strings.TrimSpace(name))]
		switch {
		case attrDiags.HasErrors():
		case !known:
			diags = append(diags, badAttr(axisAttr, "Not an axis", fmt.Sprintf(
				"fov_axis is %q. It says which angle fov spans, and must be \"diagonal\", "+
					"\"horizontal\", or \"unstated\" when the manufacturer did not say.", name)))
		default:
			p.Axis = axis
		}
	}
	if haveFOV != haveAxis {
		diags = append(diags, badBlock(block, "An angle with no axis", "fov and fov_axis go together. "+
			"Manufacturers print a bare angle and almost never say which one it is, and a horizontal "+
			"figure read as a diagonal puts everything in the wrong place without looking broken. "+
			"Write fov_axis = \"unstated\" when nobody said."))
	}
	return diags
}

// panelAttrs decodes eye_width and eye_height, which also travel together: half
// a panel size is not a smaller fact, it is a wrong one, because the missing
// half reads as zero and silently unsets EyeAspect.
func panelAttrs(content *hcl.BodyContent, block *hcl.Block, p *Profile, diags hcl.Diagnostics) hcl.Diagnostics {
	width, haveWidth, widthDiags := panelSide(content, "eye_width")
	height, haveHeight, heightDiags := panelSide(content, "eye_height")
	diags = append(append(diags, widthDiags...), heightDiags...)
	switch {
	case haveWidth && haveHeight:
		p.EyeWidth, p.EyeHeight = width, height
	case haveWidth != haveHeight:
		diags = append(diags, badBlock(block, "Half a panel size",
			"eye_width and eye_height describe one eye's panel together. Give both, or neither."))
	}
	return diags
}

// panelSide decodes one side of the per-eye panel, which is optional but must
// be a real number of pixels when it is given.
func panelSide(content *hcl.BodyContent, name string) (int, bool, hcl.Diagnostics) {
	attr, ok := content.Attributes[name]
	if !ok {
		return 0, false, nil
	}
	var px int
	diags := gohcl.DecodeExpression(attr.Expr, nil, &px)
	if diags.HasErrors() {
		return 0, true, diags
	}
	if px <= 0 {
		return 0, true, append(diags, badAttr(attr, "Not a panel size", fmt.Sprintf(
			"%s is %d. One eye's panel is a positive number of pixels; leave eye_width and "+
				"eye_height out when the panel is not known.", name, px)))
	}
	return px, true, diags
}

// matchAttr decodes one list of match strings, lower-casing them and refusing
// any that is empty: an empty string is contained in every name, so one blank
// entry would claim the laptop's own screen is a headset.
func matchAttr(content *hcl.BodyContent, name string, diags hcl.Diagnostics) ([]string, hcl.Diagnostics) {
	attr, ok := content.Attributes[name]
	if !ok {
		return nil, diags
	}
	var raw []string
	attrDiags := gohcl.DecodeExpression(attr.Expr, nil, &raw)
	diags = append(diags, attrDiags...)
	if attrDiags.HasErrors() {
		return nil, diags
	}
	out := make([]string, 0, len(raw))
	for i, m := range raw {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			diags = append(diags, badAttr(attr, "A match that matches everything", fmt.Sprintf(
				"%s[%d] is empty. An empty string is contained in every name, so this model "+
					"would claim the laptop's own screen.", name, i)))
			continue
		}
		out = append(out, m)
	}
	return out, diags
}

// usbIDs decodes a USB id, or a list of them, written the way lsusb and Windows
// print them: hexadecimal, in a string, because HCL numbers have no hex form.
func usbIDs(content *hcl.BodyContent, name string, diags hcl.Diagnostics) ([]uint16, hcl.Diagnostics) {
	attr, ok := content.Attributes[name]
	if !ok {
		return nil, diags
	}
	var raw []string
	if name == "usb_vendor" {
		var one string
		attrDiags := gohcl.DecodeExpression(attr.Expr, nil, &one)
		diags = append(diags, attrDiags...)
		if attrDiags.HasErrors() {
			return nil, diags
		}
		raw = []string{one}
	} else {
		attrDiags := gohcl.DecodeExpression(attr.Expr, nil, &raw)
		diags = append(diags, attrDiags...)
		if attrDiags.HasErrors() {
			return nil, diags
		}
	}
	out := make([]uint16, 0, len(raw))
	for _, s := range raw {
		// Base 0, so both "0x35ca" and a bare "35ca" would need the prefix;
		// requiring it keeps the number unambiguous rather than guessing that
		// "1104" means hexadecimal.
		v, err := strconv.ParseUint(strings.TrimSpace(s), 0, 16)
		if err != nil {
			diags = append(diags, badAttr(attr, "Not a USB id", fmt.Sprintf(
				"%s contains %q, which is not a 16-bit id. Write it as lsusb prints it, "+
					"with the prefix: \"0x35ca\".", name, s)))
			continue
		}
		out = append(out, uint16(v))
	}
	return out, diags
}

// stringAttr decodes a plain optional string.
func stringAttr(content *hcl.BodyContent, name string, into *string) hcl.Diagnostics {
	attr, ok := content.Attributes[name]
	if !ok {
		return nil
	}
	return gohcl.DecodeExpression(attr.Expr, nil, into)
}

// badBlock and badAttr build the two diagnostics this package raises itself:
// one about a block as a whole, one about the value of a single attribute. Both
// carry a source range, which is what puts the file and the line in the message.
func badBlock(block *hcl.Block, summary, detail string) *hcl.Diagnostic {
	return &hcl.Diagnostic{Severity: hcl.DiagError, Summary: summary, Detail: detail,
		Subject: block.DefRange.Ptr()}
}

func badAttr(attr *hcl.Attribute, summary, detail string) *hcl.Diagnostic {
	return &hcl.Diagnostic{Severity: hcl.DiagError, Summary: summary, Detail: detail,
		Subject: attr.Expr.Range().Ptr()}
}

// diagError renders diagnostics the way HCL itself does — the file, the line,
// the offending source and a sentence about what is wrong — and wraps them in
// an ordinary error, so a caller that only prints errors gets the good message
// without knowing anything about HCL.
func diagError(files map[string]*hcl.File, diags hcl.Diagnostics) error {
	var buf bytes.Buffer
	// Colour off: this goes into an error, which may end up anywhere. The write
	// cannot fail, because a bytes.Buffer cannot fail.
	_ = hcl.NewDiagnosticTextWriter(&buf, files, 0, false).WriteDiagnostics(diags)
	return fmt.Errorf("glasses: %s", strings.TrimSpace(buf.String()))
}
