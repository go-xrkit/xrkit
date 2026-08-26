package ribbon

import (
	"fmt"
	"math"

	"github.com/go-xrkit/xrkit/pose"
)

// DefaultTau is the approach time constant [NewNav] starts with, in seconds.
// Around a fifth of a second reads as deliberate: fast enough that a keypress
// feels answered, slow enough that the eye follows the screens round instead of
// finding them somewhere else.
const DefaultTau = 0.18

// tail is the angle, in radians, over which the approach stops being
// exponential and finishes at a constant speed. About three degrees.
//
// A pure exponential never lands. Worse than that, it CREEPS: a hundred-degree
// step is visually over in half a second and still half a degree from its target
// two seconds later, drifting a few panorama columns at a time while the
// renderer is kept awake for it. Cutting the tail with a snap instead would
// finish the motion with a visible jump.
//
// So the last few degrees are covered at the speed the exponential had reached
// when it got there — tail/Tau, which makes the velocity continuous at the
// changeover rather than a kink — and the whole motion ends, exactly, one Tau
// later.
const tail = 0.05

// Mode is what the viewer is looking at.
type Mode int

// The three modes.
const (
	// ModeRibbon is the ordinary view: the band of screens, turning as the
	// viewer scrolls.
	ModeRibbon Mode = iota
	// ModeFullscreen promotes the focused screen to fill the view. The ribbon is
	// still there, still turning, and the yaw still tracks the focused screen —
	// so leaving fullscreen does not drop the viewer somewhere else.
	ModeFullscreen
	// ModeGallery shows every screen at once, as a grid in front of the viewer,
	// with one of them selected — the answer to "where is the one three round the
	// back", which the ribbon cannot give without turning through the ones in
	// between.
	//
	// It is a mode of [Nav] rather than a thing the application drives beside it
	// because it is exactly what [Mode] is for — what the viewer is looking at —
	// and because opening it, cancelling it and choosing from it are all
	// statements about where the viewer will be next, which is this type's job.
	// It is entered only through [Nav.ToggleGallery], which needs a [Gallery] to
	// enter it with; a mode with no gallery behind it is a state with no picture,
	// and it should not be reachable.
	ModeGallery
)

// String names the mode.
func (m Mode) String() string {
	switch m {
	case ModeRibbon:
		return "ribbon"
	case ModeFullscreen:
		return "fullscreen"
	case ModeGallery:
		return "gallery"
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

// Nav is where the viewer is looking and where they are heading.
//
// Yaw is a plain continuous number, not a quaternion and not a step count. That
// is deliberate on both ends: the keyboard drives it by naming a screen, and a
// head tracker — when one is reachable — can drive it by writing an angle,
// without either of them knowing about the other.
type Nav struct {
	// Tau is the approach time constant in SECONDS: every Tau closes about 63%
	// of the angle still to go. It is a time constant rather than a per-frame
	// fraction because the frame rate is not ours to choose — a fraction per
	// frame makes the ribbon turn at a speed that depends on how busy the
	// machine is. Zero or less means jump straight there.
	Tau float64

	r     *Ribbon
	focus int
	// yaw and target are CONTINUOUS, deliberately not folded into [0, 2π): the
	// difference between them carries which way round to turn, and wrapping it
	// away is exactly how a 20° step becomes a 340° one.
	yaw, target float64
	mode        Mode
	// gal is the open gallery, and nil at every other time. It is the whole of
	// the gallery's state that Nav owns: the selection lives in the [Gallery],
	// where it is always meaningful, rather than here where it would need a
	// sentinel for "no gallery is open".
	gal Selectable
	// saved is the ribbon as the gallery found it.
	//
	// Restoring it is an ASSIGNMENT of the numbers that were taken, not a
	// recomputation from the focus. A yaw caught mid-turn is not any screen's
	// centre, and re-deriving it would put the viewer back NEARLY where they
	// were, which is a jump they can see; and it keeps [Nav.Moving] true across a
	// cancelled gallery, because the turn the viewer interrupted is still owed to
	// them.
	saved struct {
		yaw, target float64
		focus       int
		mode        Mode
	}
}

// NewNav starts the viewer facing the first screen, at rest.
func NewNav(r *Ribbon) *Nav {
	return &Nav{Tau: DefaultTau, r: r, yaw: r.screens[0].Centre, target: r.screens[0].Centre}
}

// Ribbon returns the ribbon being navigated.
func (n *Nav) Ribbon() *Ribbon { return n.r }

// Focus is the index of the screen the viewer is heading for.
func (n *Nav) Focus() int { return n.focus }

// Yaw is the longitude the viewer is facing now, in [0, 2π).
func (n *Nav) Yaw() float64 { return wrap(n.yaw) }

// Target is the longitude the viewer is heading for, in [0, 2π).
func (n *Nav) Target() float64 { return wrap(n.target) }

// Moving reports whether the yaw has anywhere left to go.
func (n *Nav) Moving() bool { return n.yaw != n.target }

// Mode is what the viewer is looking at.
func (n *Nav) Mode() Mode { return n.mode }

// SetMode switches between the ribbon and the promoted screen. An unknown mode
// is refused rather than quietly treated as one of the two, since which one it
// would be is not something a caller could reason about.
//
// [ModeGallery] is refused too, and it is not an unknown mode: it is a known one
// that cannot be entered this way, because entering it means remembering the
// ribbon to come back to and choosing a screen to start on, and neither of those
// is expressible as a mode. [Nav.ToggleGallery] is the way in.
func (n *Nav) SetMode(m Mode) error {
	if m == ModeGallery {
		return fmt.Errorf("%w: open it with ToggleGallery", ErrNoGallery)
	}
	if m != ModeRibbon && m != ModeFullscreen {
		return fmt.Errorf("%w: %s", ErrMode, m)
	}
	n.mode = m
	return nil
}

// ToggleFullscreen promotes the focused screen, or puts it back.
//
// It does nothing while the gallery is open. The gallery is modal — it is drawn
// instead of the ribbon, not over it — and the two ways out of it are the key
// that opened it and the one that chooses. Letting a third key leave by another
// door would abandon the saved ribbon.
func (n *Nav) ToggleFullscreen() {
	switch n.mode {
	case ModeGallery:
		return
	case ModeFullscreen:
		n.mode = ModeRibbon
		return
	}
	n.mode = ModeFullscreen
}

// Selectable is what a navigator needs of a gallery: how many screens it holds,
// which one is highlighted, and the ability to move that highlight.
//
// It is an interface because the gallery in this package is not the only one
// worth having. A consumer whose screens are FLAT — laid side by side rather
// than round a cylinder — folds them into a grid of its own, and the navigator
// has nothing to say about how that grid is drawn. What it needs is the
// selection, and that is all this asks for.
type Selectable interface {
	// Len is how many screens the gallery holds.
	Len() int
	// Selected is the highlighted screen.
	Selected() int
	// Select highlights one, and refuses an index it does not have.
	Select(i int) error
}

// ToggleGallery opens the gallery, or closes it and leaves the ribbon exactly as
// it was.
//
// One method, because it is one key: the application binds ⌥⌘Space, and the same
// press has to put the viewer back. Two methods would let an application open
// the gallery twice and overwrite the ribbon it was going to restore.
//
// Opening selects the screen the viewer is FACING — [Ribbon.Nearest], not
// [Nav.Focus] — so the gallery starts where they are even if a head tracker put
// them between two screens. It also freezes the ribbon: see [Nav.Advance].
//
// Closing restores the yaw, the target, the focus and the MODE, so ⌥⌘Space from
// a promoted screen and back again returns to that promoted screen. g must not
// be nil, and must be a gallery of THIS ribbon: one built for another would
// select screens that are not there. A [Gallery] is checked by identity, which
// is exact; any other [Selectable] is checked by how many screens it holds,
// which is the most that can be known about it.
func (n *Nav) ToggleGallery(g Selectable) error {
	if n.mode == ModeGallery {
		n.restore()
		return nil
	}
	if own, ok := g.(*Gallery); ok {
		if own.r != n.r {
			return fmt.Errorf("%w: %d screens, not %d", ErrNotOurs, own.r.Len(), n.r.Len())
		}
	} else if g.Len() != n.r.Len() {
		return fmt.Errorf("%w: %d screens, not %d", ErrNotOurs, g.Len(), n.r.Len())
	}
	n.saved.yaw, n.saved.target = n.yaw, n.target
	n.saved.focus, n.saved.mode = n.focus, n.mode
	n.gal, n.mode = g, ModeGallery
	// Selecting through the interface rather than by writing the field: a
	// gallery this package did not build has its own idea of what an index is
	// allowed to be, and Nearest is only ever a screen of this ribbon.
	_ = g.Select(n.r.Nearest(n.yaw))
	return nil
}

// Choose leaves the gallery on the selected screen: back to the ribbon, focused
// on it, turning to it the short way round.
//
// It restores the ribbon first and then aims, rather than aiming from wherever
// the gallery left things. That is what makes choosing the screen you were
// already looking at cost no motion at all, and what makes choosing any other
// one a turn measured from where the viewer actually is.
func (n *Nav) Choose() error {
	if n.mode != ModeGallery {
		return fmt.Errorf("%w: nothing to choose", ErrNoGallery)
	}
	sel := n.gal.Selected()
	n.restore()
	// Enter goes back to the BAND, whatever mode the gallery was opened from: the
	// viewer asked for that screen, and the ribbon is where a screen is chosen.
	n.mode = ModeRibbon
	return n.GoTo(sel)
}

// restore puts the ribbon back exactly as the gallery found it and closes it.
func (n *Nav) restore() {
	n.yaw, n.target = n.saved.yaw, n.saved.target
	n.focus, n.mode = n.saved.focus, n.saved.mode
	n.gal = nil
}

// GoTo focuses screen i and heads for it the short way round.
func (n *Nav) GoTo(i int) error {
	if i < 0 || i >= n.r.Len() {
		return fmt.Errorf("%w: %d", ErrIndex, i)
	}
	n.aim(i, 0)
	return nil
}

// Next moves the focus one screen to the right, wrapping past the last.
//
// It turns the short way, which on a ribbon that fills the circle is the hop
// forwards to the neighbour — including the hop from the last screen to the
// first, which is a gap's width forwards and not a lap backwards. On a [Packed]
// ribbon that does not fill the circle, the same rule turns back across the
// block instead, because there the block IS the short way: the viewer sees the
// screens they know go past, not a long swing through empty space.
func (n *Nav) Next() { n.aim((n.focus+1)%n.r.Len(), +1) }

// Prev moves the focus one screen to the left, wrapping past the first.
func (n *Nav) Prev() { n.aim((n.focus+n.r.Len()-1)%n.r.Len(), -1) }

// aim points the viewer at screen i, turning the short way from wherever the yaw
// is NOW — mid-flight included, so a second keypress before the first has landed
// redirects rather than queues.
func (n *Nav) aim(i, dir int) {
	n.focus = i
	n.target = n.yaw + shortest(n.yaw, n.r.screens[i].Centre, dir)
}

// SetYaw puts the viewer at a longitude immediately, with nothing left to
// approach. It is the seam a head tracker would be wired into, and it does not
// change the focus: where the viewer is looking and which screen they last
// chose are different facts, and [Ribbon.Nearest] reconciles them when the
// application wants them reconciled.
func (n *Nav) SetYaw(yaw float64) { n.yaw, n.target = yaw, yaw }

// Advance eases the yaw towards the target over dt seconds.
//
// The step is 1 - exp(-dt/Tau), which is the exponential approach SAMPLED at dt
// rather than approximated by it: two 8 ms frames land in the same place as one
// 16 ms frame, so the ribbon turns at the same speed on a machine dropping
// frames as on one that is not. It cannot overshoot, because that factor is
// always below 1 — the motion settles from one side and stops, rather than
// ringing round the target the way a spring would — and the last [tail] radians
// are covered at constant speed so that it actually arrives.
//
// It does nothing while the gallery is open. The ribbon is not on screen then,
// so turning it is motion nobody can see — and an application that calls this
// every frame, which is every application, would otherwise let a turn the viewer
// interrupted quietly finish behind the gallery and then watch it snap back when
// they cancelled.
func (n *Nav) Advance(dt float64) {
	d := n.target - n.yaw
	if dt <= 0 || d == 0 || n.mode == ModeGallery {
		return
	}
	if n.Tau <= 0 {
		n.yaw = n.target
		return
	}
	step := d * (1 - math.Exp(-dt/n.Tau))
	if floor := tail * dt / n.Tau; math.Abs(step) < floor {
		if math.Abs(d) <= floor {
			// Close enough that this frame covers the rest. Assigning the target
			// rather than adding to the yaw is what makes the arrival EXACT: an
			// addition that ought to land on it can miss by an ulp, and then the
			// ribbon is still moving, forever, by nothing.
			n.yaw = n.target
			return
		}
		step = math.Copysign(floor, d)
	}
	n.yaw += step
}

// Orientation is the yaw as a rotation, for the rest of the pipeline.
//
// The sign flips here, and this is the only place it does. A ribbon longitude
// grows to the RIGHT, because that is the equirectangular convention
// [github.com/go-xrkit/xrkit/projection.Projection.Sample] reads a panorama
// with; a [github.com/go-xrkit/xrkit/pose.Euler] yaw grows to the LEFT, because
// that is a rotation about +Y in a right-handed space. Facing longitude L is
// therefore a yaw of -L, and getting that backwards mirrors the whole ribbon —
// which looks entirely plausible until the viewer scrolls and the screens go the
// wrong way.
func (n *Nav) Orientation() pose.Quat {
	return pose.FromEulerZXY(pose.Euler{Yaw: -deg(n.Yaw())})
}
