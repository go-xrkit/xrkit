<p align="center"><img src="https://raw.githubusercontent.com/go-xrkit/brand/main/social/go-xrkit.png" alt="go-xrkit" width="720"></p>

# go-xrkit/xrkit

[![Go Reference](https://pkg.go.dev/badge/github.com/go-xrkit/xrkit.svg)](https://pkg.go.dev/github.com/go-xrkit/xrkit)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD%203--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![CI](https://github.com/go-xrkit/xrkit/actions/workflows/ci.yml/badge.svg)](https://github.com/go-xrkit/xrkit/actions/workflows/ci.yml)

The geometry an immersive video player needs, as pure Go with no dependencies:
orientation, stereo packing, and the projections that turn a flat frame into a
world you can look around in.

`CGO_ENABLED=0`, no third-party modules, 100% statement coverage, and every
package testable without a headset attached — which is the point. Sign and
axis-order mistakes are invisible in a still frame and awful to wear, so they
are pinned by tests against known directions rather than discovered by putting
the glasses on.

## `pose` — orientation

Quaternions, the Euler convention head trackers actually report, recentring and
smoothing.

```go
q := pose.FromEulerZXY(pose.Euler{Yaw: -30, Pitch: 10})
dir := q.Rotate(pose.Vec3{Z: -1})     // where the viewer is looking

r := pose.NewRecentre()
r.Set(q)                              // "this is straight ahead now"
rel := r.Apply(next)

s := pose.Smoother{Alpha: 0.35}       // a tracker is noisy at rest
smooth := s.Update(rel)
```

**Yaw is applied last.** `FromEulerZXY` composes roll, then pitch, then yaw, so
yaw stays a turn about the global up axis. Compose it first instead and pitching
to 90° no longer looks straight up: there is no gimbal lock, and the horizon
swings as the viewer raises their head. This package had that bug; every
single-axis test passed while it did, because with one non-zero angle the order
cannot matter.

## `stereo` — how a frame packs two eyes

```go
f := stereo.Format{Layout: stereo.SideBySide}
r := f.EyeRect(stereo.Left, 3840, 1080)   // {0, 0, 1920, 1080}
```

`Swapped` is an explicit flag, never a guess: eye-reversed material is not
detectable from the pixels, and getting it wrong inverts the depth of the whole
scene — which viewers report as eye strain rather than as a wrong picture.

An odd frame dimension floors the split and leaves the middle line unread.
Losing one column is invisible; a column of the wrong eye is not.

## `projection` — direction ↔ picture

```go
vp := projection.Viewport{Width: 1920, Height: 1080, FOVyDeg: 90}
dir := vp.LookRay(headOrientation, x, y)
u, v, ok := projection.Sphere360.Sample(dir)
if !ok {
        // outside the content — show background, do not clamp: clamping smears
        // the edge pixels across the whole of the missing region
}
```

`Flat` (a virtual screen), `Equirect` (360×180 or VR180's 180×180) and `Fisheye`
(equidistant, 180° to 200°). Fisheye is *equidistant*, not tangent: radius is
proportional to the angle from the axis. A tangent law agrees at the centre and
is wrong everywhere else, which is the kind of error that looks plausible in a
still.

Rays are taken through pixel **centres**. Sampling the corner instead biases the
whole image by half a pixel, invisible alone and a visible seam where two views
meet.

## Status

These three packages are complete and gated at 100% coverage. Still to come:
hardware video decode, a GPU warp, and the glasses' own head tracking — the last
of which is not reachable over HID on the current VITURE generation (see
`go-macos/iokit`).

Licence: BSD-3-Clause.
