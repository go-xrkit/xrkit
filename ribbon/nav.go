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

// The two modes.
const (
	// ModeRibbon is the ordinary view: the band of screens, turning as the
	// viewer scrolls.
	ModeRibbon Mode = iota
	// ModeFullscreen promotes the focused screen to fill the view. The ribbon is
	// still there, still turning, and the yaw still tracks the focused screen —
	// so leaving fullscreen does not drop the viewer somewhere else.
	ModeFullscreen
)

// String names the mode.
func (m Mode) String() string {
	switch m {
	case ModeRibbon:
		return "ribbon"
	case ModeFullscreen:
		return "fullscreen"
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
func (n *Nav) SetMode(m Mode) error {
	if m != ModeRibbon && m != ModeFullscreen {
		return fmt.Errorf("%w: %s", ErrMode, m)
	}
	n.mode = m
	return nil
}

// ToggleFullscreen promotes the focused screen, or puts it back.
func (n *Nav) ToggleFullscreen() {
	if n.mode == ModeFullscreen {
		n.mode = ModeRibbon
		return
	}
	n.mode = ModeFullscreen
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
func (n *Nav) Advance(dt float64) {
	d := n.target - n.yaw
	if dt <= 0 || d == 0 {
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
