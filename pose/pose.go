// Package pose is the orientation arithmetic an XR viewer needs: quaternions,
// the Euler conventions headsets actually report, recentring, and smoothing.
//
// It knows nothing about devices or rendering. That is the point — orientation
// is where sign and axis-order mistakes hide, and they hide best when the only
// way to exercise the code is to put a headset on. Everything here is pure and
// tested against known rotations, so a wrong axis fails a test instead of
// making the horizon tilt.
package pose

import "math"

// Vec3 is a vector in a right-handed space: +X right, +Y up, +Z towards the
// viewer, which is the convention OpenGL, OpenXR and this package share. A
// viewer looking straight ahead looks down -Z.
type Vec3 struct{ X, Y, Z float64 }

// Add, Sub and Scale are the vector arithmetic the projection code needs.
func (v Vec3) Add(w Vec3) Vec3      { return Vec3{v.X + w.X, v.Y + w.Y, v.Z + w.Z} }
func (v Vec3) Sub(w Vec3) Vec3      { return Vec3{v.X - w.X, v.Y - w.Y, v.Z - w.Z} }
func (v Vec3) Scale(s float64) Vec3 { return Vec3{v.X * s, v.Y * s, v.Z * s} }

// Dot is the scalar product.
func (v Vec3) Dot(w Vec3) float64 { return v.X*w.X + v.Y*w.Y + v.Z*w.Z }

// Len is the Euclidean length.
func (v Vec3) Len() float64 { return math.Sqrt(v.Dot(v)) }

// Unit returns v scaled to length 1. The zero vector has no direction, and is
// returned unchanged rather than producing NaNs that would propagate silently
// into a sampling coordinate.
func (v Vec3) Unit() Vec3 {
	l := v.Len()
	if l == 0 {
		return v
	}
	return v.Scale(1 / l)
}

// Quat is a rotation as a unit quaternion. The zero value is not a rotation;
// use [Identity].
type Quat struct{ W, X, Y, Z float64 }

// Identity is the null rotation.
func Identity() Quat { return Quat{W: 1} }

// Mul composes rotations: q.Mul(r) applies r first, then q — the same order as
// matrix multiplication, so a reader familiar with either is not surprised.
func (q Quat) Mul(r Quat) Quat {
	return Quat{
		W: q.W*r.W - q.X*r.X - q.Y*r.Y - q.Z*r.Z,
		X: q.W*r.X + q.X*r.W + q.Y*r.Z - q.Z*r.Y,
		Y: q.W*r.Y - q.X*r.Z + q.Y*r.W + q.Z*r.X,
		Z: q.W*r.Z + q.X*r.Y - q.Y*r.X + q.Z*r.W,
	}
}

// Conj is the conjugate, which for a unit quaternion is the inverse rotation.
func (q Quat) Conj() Quat { return Quat{q.W, -q.X, -q.Y, -q.Z} }

// Len is the quaternion's norm.
func (q Quat) Len() float64 {
	return math.Sqrt(q.W*q.W + q.X*q.X + q.Y*q.Y + q.Z*q.Z)
}

// Unit renormalises. Repeated composition drifts off the unit sphere, and a
// non-unit quaternion scales what it rotates, so anything long-lived should be
// renormalised. A zero quaternion cannot be normalised and yields [Identity]
// rather than NaNs.
func (q Quat) Unit() Quat {
	l := q.Len()
	if l == 0 {
		return Identity()
	}
	return Quat{q.W / l, q.X / l, q.Y / l, q.Z / l}
}

// Rotate applies the rotation to v.
func (q Quat) Rotate(v Vec3) Vec3 {
	// v' = v + 2w(u×v) + 2u×(u×v), with u the vector part. This is the standard
	// expansion of q v q*, and costs no quaternion multiplications.
	u := Vec3{q.X, q.Y, q.Z}
	t := cross(u, v).Scale(2)
	return v.Add(t.Scale(q.W)).Add(cross(u, t))
}

func cross(a, b Vec3) Vec3 {
	return Vec3{
		a.Y*b.Z - a.Z*b.Y,
		a.Z*b.X - a.X*b.Z,
		a.X*b.Y - a.Y*b.X,
	}
}

// Angle returns the rotation's magnitude in radians, in [0, π].
func (q Quat) Angle() float64 {
	u := q.Unit()
	return 2 * math.Acos(clampUnit(math.Abs(u.W)))
}

// Euler is an orientation as three angles in DEGREES, the unit head trackers
// report. Yaw turns the head left/right, pitch tips it up/down, roll tilts it.
type Euler struct{ Roll, Pitch, Yaw float64 }

// FromEulerZXY builds a rotation applying ROLL about Z first, then pitch about
// X, then yaw about Y -- the Z, X, Y of the name is the order the rotations are
// applied in, giving R = Ry(yaw) . Rx(pitch) . Rz(roll).
//
// The order is not a detail to pick by taste, and getting it backwards is not a
// subtle error. Yaw must be applied LAST, about the global up axis, or it stops
// being a horizontal turn: compose it first instead and pitching to 90 degrees
// no longer looks straight up, so the horizon swings as the viewer raises their
// head. This is the convention head trackers report in, and the one that makes
// pitch = 90 degrees the degenerate case rather than an arbitrary direction.
func FromEulerZXY(e Euler) Quat {
	r, p, y := rad(e.Roll), rad(e.Pitch), rad(e.Yaw)
	qy := Quat{W: math.Cos(y / 2), Y: math.Sin(y / 2)}
	qx := Quat{W: math.Cos(p / 2), X: math.Sin(p / 2)}
	qz := Quat{W: math.Cos(r / 2), Z: math.Sin(r / 2)}
	// Right-to-left is application order: roll, then pitch, then yaw.
	return qy.Mul(qx).Mul(qz)
}

// EulerZXY decomposes a rotation back into the same convention
// [FromEulerZXY] builds from. Pitch is clamped to ±90°, where yaw and roll
// become degenerate (gimbal lock): there the decomposition puts the whole
// remaining rotation into yaw and leaves roll at zero, which is a choice, not a
// recovery of information the orientation no longer distinguishes.
func (q Quat) EulerZXY() Euler {
	u := q.Unit()
	// From R = Ry.Rx.Rz: R[1][2] = -sin(pitch), so sin(pitch) = 2(wx - yz).
	s := clampUnit(2 * (u.W*u.X - u.Y*u.Z))
	pitch := math.Asin(s)
	if math.Abs(s) > 1-1e-12 {
		// Gimbal lock: looking straight up or down, only yaw-minus-roll (or
		// yaw-plus-roll) is determined -- the orientation genuinely no longer
		// distinguishes the two. Attributing all of it to yaw is a choice, not a
		// recovery of information that is there.
		return Euler{
			Roll:  0,
			Pitch: deg(pitch),
			Yaw:   deg(math.Atan2(2*(u.W*u.Y-u.X*u.Z), 1-2*(u.Y*u.Y+u.Z*u.Z))),
		}
	}
	// roll = atan2(R[1][0], R[1][1]), yaw = atan2(R[0][2], R[2][2]).
	roll := math.Atan2(2*(u.X*u.Y+u.W*u.Z), 1-2*(u.X*u.X+u.Z*u.Z))
	yaw := math.Atan2(2*(u.X*u.Z+u.W*u.Y), 1-2*(u.X*u.X+u.Y*u.Y))
	return Euler{Roll: deg(roll), Pitch: deg(pitch), Yaw: deg(yaw)}
}

// clampUnit holds x inside [-1, 1], the domain of Asin and Acos.
//
// It exists because the arguments handed to those functions are built from
// quaternion components, and a value that is mathematically exactly 1 can come
// out a few ulps above it. Acos then returns NaN, which does not fail loudly --
// it propagates into a sampling coordinate and shows up as a hole in the
// picture. The guard is unreachable through the public API precisely because
// Unit() normalises first, so it is a function of its own and tested directly
// rather than left as an untested branch.
func clampUnit(x float64) float64 {
	if x > 1 {
		return 1
	}
	if x < -1 {
		return -1
	}
	return x
}

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }

// Slerp interpolates along the shortest arc from q to r, with t clamped to
// [0,1]. Taking the shortest arc matters: a quaternion and its negation are the
// same rotation, so interpolating without the sign check can travel the long
// way round and spin the view through 300° to reach a neighbouring angle.
func Slerp(q, r Quat, t float64) Quat {
	if t <= 0 {
		return q
	}
	if t >= 1 {
		return r
	}
	a, b := q.Unit(), r.Unit()
	dot := a.W*b.W + a.X*b.X + a.Y*b.Y + a.Z*b.Z
	if dot < 0 {
		b, dot = Quat{-b.W, -b.X, -b.Y, -b.Z}, -dot
	}
	if dot > 1-1e-9 {
		// Nearly identical: lerp and renormalise, since sin(θ) → 0 would divide
		// away the precision that is left.
		return Quat{
			a.W + t*(b.W-a.W),
			a.X + t*(b.X-a.X),
			a.Y + t*(b.Y-a.Y),
			a.Z + t*(b.Z-a.Z),
		}.Unit()
	}
	theta := math.Acos(dot)
	sin := math.Sin(theta)
	wa, wb := math.Sin((1-t)*theta)/sin, math.Sin(t*theta)/sin
	return Quat{
		a.W*wa + b.W*wb,
		a.X*wa + b.X*wb,
		a.Y*wa + b.Y*wb,
		a.Z*wa + b.Z*wb,
	}.Unit()
}

// Recentre makes one orientation the new "straight ahead". A viewer sits how
// they like, presses recentre, and the content is in front of them.
type Recentre struct{ ref Quat }

// NewRecentre starts with no offset, so Apply is the identity.
func NewRecentre() *Recentre { return &Recentre{ref: Identity()} }

// Set makes q the reference: Apply(q) then returns [Identity].
func (r *Recentre) Set(q Quat) { r.ref = q.Unit() }

// Reference returns the current reference orientation.
func (r *Recentre) Reference() Quat { return r.ref }

// Apply expresses q relative to the reference.
func (r *Recentre) Apply(q Quat) Quat { return r.ref.Conj().Mul(q.Unit()) }

// Smoother low-pass filters a stream of orientations. A head tracker's output
// is noisy at rest, and that noise is visible as a shimmer in a magnified view.
type Smoother struct {
	// Alpha is how much of each new sample is taken, in (0,1]. 1 is no
	// smoothing; smaller is smoother and lags more. Values outside the range are
	// clamped, so a zero value means "no smoothing" rather than "freeze".
	Alpha float64

	cur   Quat
	valid bool
}

// Update folds in a new sample and returns the smoothed orientation. The first
// sample is adopted as-is: easing in from an arbitrary starting orientation
// would swing the view on the first frame.
func (s *Smoother) Update(q Quat) Quat {
	q = q.Unit()
	if !s.valid {
		s.cur, s.valid = q, true
		return s.cur
	}
	a := s.Alpha
	if a <= 0 || a > 1 {
		a = 1
	}
	s.cur = Slerp(s.cur, q, a)
	return s.cur
}

// Current returns the last smoothed value, and whether any sample has arrived.
func (s *Smoother) Current() (Quat, bool) {
	if !s.valid {
		return Identity(), false
	}
	return s.cur, true
}

// Reset forgets the history, so the next Update is adopted as-is.
func (s *Smoother) Reset() { s.cur, s.valid = Identity(), false }
