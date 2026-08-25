<p align="center"><img src="https://raw.githubusercontent.com/go-xrkit/brand/main/social/go-xrkit.png" alt="go-xrkit" width="720"></p>

# go-xrkit/xrkit

[![Go Reference](https://pkg.go.dev/badge/github.com/go-xrkit/xrkit.svg)](https://pkg.go.dev/github.com/go-xrkit/xrkit)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD%203--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![CI](https://github.com/go-xrkit/xrkit/actions/workflows/ci.yml/badge.svg)](https://github.com/go-xrkit/xrkit/actions/workflows/ci.yml)

The geometry an immersive video player and an XR virtual desktop need, as pure
Go with no dependencies: orientation, stereo packing, the projections that turn
a flat frame into a world you can look around in, and the band of screens you
scroll through inside it.

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

## `warp` — the projection as a lookup table

Per-pixel trigonometry is fine to reason about and far too slow for four million
pixels sixty times a second. When the viewer's orientation is fixed, the answer
for every output pixel is the same every frame, so it is computed once into a
table of source offsets and each frame becomes a gather.

```go
m := warp.Build(vp, projection.Sphere360, pose.Identity(), src)
m.ApplySwapRB(frame, panel, 3840, 0, 0)   // 2.8 ms for both eyes at 3840x1080
```

`Build` costs **56.5 ms**; `Apply` costs **2.8 ms**. That ratio is what `ribbon`
is designed around.

## `ribbon` — screens on a 360° band

Several captured displays, floating around the viewer at eye level, scrolled
from the keyboard.

```go
r, err := ribbon.Place(displays, ribbon.Layout{
        DensityDeg: 22, GapDeg: 3, FullWidthDeg: 110, Arrangement: ribbon.Spread,
})
c, err := ribbon.NewCompositor(r, ribbon.Pano{W: 2048, H: 1024,
        Window: projection.Projection{Kind: projection.Equirect, HSpanDeg: 140, VSpanDeg: 70}})

n := ribbon.NewNav(r)
n.Next()                                  // the short way round the seam

// every frame:
n.Advance(dt)
blits = c.Frame(blits[:0], n.Yaw())       // 488 ns, zero allocations
```

**The warp map is never rebuilt.** On an equirectangular source a yaw is exactly
a horizontal shift, so the map is built once for `pose.Identity()`, the panorama
is a fixed window centred on straight ahead, and the yaw is applied where it is
free: each screen is composited in at longitude − yaw.

Screens sit on a **cylinder**, so the horizontal mapping is linear in longitude
and a run of destination columns reads a run of source columns at a constant
step. The vertical mapping goes through a tangent — height on a cylinder is not
proportional to latitude — so it is a per-row table, and it does not depend on
the yaw, so it is built once.

Longitude grows to the **right**, matching `projection`; a `pose` yaw grows to
the left. `Nav.Orientation` is the only place that sign is converted, and it has
a test that closes the loop through `projection.Sample` rather than trusting it.

## Status

These packages are complete and gated at 100% coverage. Still to come:
screen capture, hardware video decode, a GPU warp, and the glasses' own head
tracking — the last of which is not reachable over HID on the current VITURE
generation (see `go-macos/iokit`), which is why the ribbon is driven by the
keyboard.

Licence: BSD-3-Clause.
