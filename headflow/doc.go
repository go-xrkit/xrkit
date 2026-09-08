// Copyright (c) the go-xrkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package headflow recovers how far a head has turned from the pictures its
// headset's camera takes, in pure Go with CGO_ENABLED=0.
//
// It exists because some headsets will not say. A VITURE Beast holds its own
// 3DOF tracking and uses it to anchor the picture it is given, but it publishes
// no orientation at all: measured three ways on 2026-09-07 -- listening for
// unsolicited frames, asking for the documented orientation stream, and sweeping
// every readable message with the head still and then moving -- and none of them
// produced a number. Its camera, however, is an ordinary UVC device, and a turn
// of the head is plainly visible in it.
//
// # What it does, and why it is this crude
//
// A yaw moves the whole picture sideways together. So each column of a frame is
// summed into one number, and that one-dimensional profile is slid against the
// previous frame's until it fits. There is no feature detection, no calibration
// against a model, and no state beyond the last profile. On a 1920x1080 frame it
// costs under a millisecond.
//
// # What it cannot do
//
//   - It measures YAW ONLY. Pitch and roll move the picture too, and this reads
//     the horizontal component of whatever happened.
//   - It needs something to look at. A blank wall or a dark room offers nothing
//     to match, and [Shift] says so through its confidence rather than
//     inventing a number -- see [Tracker.Confidence].
//   - It is not a pose. There is no position, no gravity, and no absolute
//     heading: only how far the view has turned since it was last recentred.
//
// # What it is good enough for, measured
//
// Held on a head and swept back and forth, the residual after returning to the
// starting point was 1.14% of the distance travelled -- about 2.6 degrees per
// there-and-back. Stationary, over five minutes and 7474 frames, it accumulated
// exactly nothing: it does not invent movement. The camera pipeline delivers a
// frame between 46 and 107 milliseconds after it was taken.
//
// Those three together decide what this is for. Choosing WHICH SCREEN somebody
// is facing -- a discrete question, where the screens are tens of degrees
// apart -- is comfortably within them. Holding a picture locked to the head at
// 1:1 is not: that wants latency under 20 milliseconds and drift near zero.
//
// ⛔ AND HELD AT ARM'S LENGTH IT IS NINE TIMES WORSE, which is worth knowing
// before testing it that way. Sweeping the camera on the end of an arm moves it
// half a metre through the room as well as turning it, and near things then
// slide further than far ones. The single best-fit shift is a compromise between
// them, and it is biased: the same experiment gave 10.53% instead of 1.14%. On a
// head the camera turns nearly about its own centre and there is little parallax
// to compromise with.
package headflow
