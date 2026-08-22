package warp

import (
	"fmt"
	"testing"

	"github.com/go-xrkit/xrkit/pose"
	"github.com/go-xrkit/xrkit/projection"
	"github.com/go-xrkit/xrkit/stereo"
)

// BenchmarkApply measures the per-frame cost that actually matters: reshaping a
// decoded frame into both eyes of the glasses' 3840x1080 side-by-side mode.
//
// The question it answers is whether a CPU gather is enough, or whether the warp
// has to go to the GPU. 60 frames a second means the whole two-eye pass must fit
// in 16.6 ms.
func BenchmarkApply(b *testing.B) {
	for _, tc := range []struct {
		name       string
		srcW, srcH int
		layout     stereo.Layout
		proj       projection.Projection
	}{
		{"4K-equirect360-to-3840x1080", 3840, 1920, stereo.Mono, projection.Sphere360},
		{"4K-SBS-VR180-to-3840x1080", 3840, 1920, stereo.SideBySide, projection.Hemisphere180},
		{"1080p-flat-screen-to-3840x1080", 1920, 1080, stereo.Mono, projection.Screen},
		{"8K-equirect360-to-3840x1080", 7680, 3840, stereo.Mono, projection.Sphere360},
	} {
		b.Run(tc.name, func(b *testing.B) {
			const outW, outH = 1920, 1080 // per eye; the panel is 3840x1080
			f := stereo.Format{Layout: tc.layout}
			vp := projection.Viewport{Width: outW, Height: outH, FOVyDeg: 90}
			src := make([]uint32, tc.srcW*tc.srcH)
			dst := make([]uint32, 3840*1080)

			maps := make([]*Map, 2)
			for i, eye := range []stereo.Eye{stereo.Left, stereo.Right} {
				maps[i] = Build(vp, tc.proj, pose.Identity(), Source{
					Width: tc.srcW, Height: tc.srcH, Stride: tc.srcW,
					Eye: f.EyeRect(eye, tc.srcW, tc.srcH),
				})
			}
			cov := float64(maps[0].Covered()) / float64(outW*outH) * 100

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				maps[0].ApplySwapRB(src, dst, 3840, 0, 0)
				maps[1].ApplySwapRB(src, dst, 3840, outW, 0)
			}
			b.StopTimer()
			perFrame := b.Elapsed().Seconds() / float64(b.N)
			b.ReportMetric(1/perFrame, "frames/s")
			b.ReportMetric(perFrame*1000, "ms/frame")
			b.ReportMetric(cov, "%covered")
			_ = fmt.Sprint()
		})
	}
}

// BenchmarkBuild measures building a table, which happens once per geometry
// change rather than per frame — but a viewer that lets the user adjust the field
// of view rebuilds it interactively, so it must not take a visible pause.
func BenchmarkBuild(b *testing.B) {
	vp := projection.Viewport{Width: 1920, Height: 1080, FOVyDeg: 90}
	src := Source{Width: 3840, Height: 1920, Stride: 3840,
		Eye: stereo.Rect{W: 3840, H: 1920}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Build(vp, projection.Sphere360, pose.Identity(), src)
	}
}
