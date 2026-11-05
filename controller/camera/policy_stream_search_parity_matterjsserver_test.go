// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"fmt"
	"slices"
	"testing"
)

// The cases of matterjs-server's
// packages/ws-controller/test/VideoWindowSearchTest.ts:79-645 at 0bd06f2,
// ported by meaning with its fixtures and its walk driver (:13-77).

var searchWindow = videoEnvelope{
	codec:         spH265,
	minResolution: resolution{640, 360}, maxResolution: resolution{2560, 1440},
	minFrameRate: 30, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 8000000, keyFrameInterval: 4000,
}

// searchPlan is the fixture's PLAN; every call returns a fresh copy.
func searchPlan() videoPlan {
	return videoPlan{envelope: searchWindow, frameRateFloor: 1, frameRateCeiling: 30, allowedFrameRate: 30}
}

func anyBudget(videoEnvelope) bool { return true }

func summary(w videoEnvelope) string {
	return fmt.Sprintf("%dx%d@%d-%d %d", w.maxResolution.width, w.maxResolution.height, w.minFrameRate, w.maxFrameRate, w.maxBitRate)
}

// cameraAnswer is the fake camera's answer to a window: a refusal, or 0 for
// accepted.
type cameraAnswer = videoRefusal

const accepted cameraAnswer = 0

// walkOptions are the walk's budget and its room maker.
type walkOptions struct {
	room func() bool
	fits func(videoEnvelope) bool
}

// walk drives a search the way the allocate ladder does: camera answers
// each window sent; room says whether a stream can be freed when the search
// asks for one.
func walk(t *testing.T, plan videoPlan, camera func(videoEnvelope) cameraAnswer, opts walkOptions) (asked []string, acceptedWindow bool, search *videoWindowSearch) {
	t.Helper()
	fits := opts.fits
	if fits == nil {
		fits = anyBudget
	}
	search = newVideoWindowSearch(&plan, fits, 0)
	for range 100 {
		move := search.move
		switch move.kind {
		case moveGiveUp:
			return asked, false, search
		case moveMakeRoom:
			asked = append(asked, "makeRoom")
			if opts.room != nil && opts.room() {
				search.roomMade()
			} else {
				search.noRoom()
			}
			continue
		default:
		}
		asked = append(asked, summary(move.window))
		answer := camera(move.window)
		if answer == accepted {
			return asked, true, search
		}
		search.refuse(answer)
	}
	t.Fatal("the search did not end")
	return nil, false, nil
}

func unservable(videoEnvelope) cameraAnswer { return refusedUnservable }
func capacity(videoEnvelope) cameraAnswer   { return refusedCapacity }

func expectAsked(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("asked\n%q\nwant\n%q", got, want)
	}
}

// TestVideoWindowSearchUnservableParityMatterjsServer is describe("an
// unservable window") (VideoWindowSearchTest.ts:80-142, matterjs-server at
// 0bd06f2).
func TestVideoWindowSearchUnservableParityMatterjsServer(t *testing.T) {
	t.Run("gives up bit rate first, then frame rate, and frame size last", func(t *testing.T) {
		asked, _, _ := walk(t, searchPlan(), unservable, walkOptions{})
		expectAsked(t, asked, []string{
			"2560x1440@30-30 8000000", "2560x1440@30-30 4000000", "2560x1440@30-30 2000000", "2560x1440@30-30 1000000", "2560x1440@30-30 800000",
			"2560x1440@15-15 800000", "2560x1440@7-7 800000",
			"1280x720@30-30 800000", "1280x720@15-15 800000", "1280x720@7-7 800000",
			"640x360@30-30 800000", "640x360@15-15 800000", "640x360@7-7 800000",
		})
	})
	t.Run("keeps the frame size and gives up frame rate, for a camera serving its full size only at 30 fps", func(t *testing.T) {
		fast := searchPlan()
		fast.envelope.minFrameRate, fast.envelope.maxFrameRate, fast.frameRateCeiling = 60, 60, 60
		asked, ok, _ := walk(t, fast, func(w videoEnvelope) cameraAnswer {
			if w.maxFrameRate > 30 || w.maxBitRate > 800000 {
				return refusedUnservable
			}
			return accepted
		}, walkOptions{})
		if !ok || asked[len(asked)-1] != "2560x1440@30-30 800000" {
			t.Errorf("asked %q, accepted %v", asked, ok)
		}
	})
	t.Run("reaches a camera that serves only 7 fps", func(t *testing.T) {
		asked, ok, _ := walk(t, searchPlan(), func(w videoEnvelope) cameraAnswer {
			if w.maxFrameRate > 7 {
				return refusedUnservable
			}
			return accepted
		}, walkOptions{})
		if !ok || asked[len(asked)-1] != "2560x1440@7-7 800000" {
			t.Errorf("asked %q, accepted %v", asked, ok)
		}
	})
	// The offer decodes 2560x1440 at 15 fps only, so 1280x720 at up to 60,
	// capped at 30.
	bounded := func() videoPlan {
		p := searchPlan()
		p.envelope.minFrameRate, p.envelope.maxFrameRate = 15, 15
		p.limits = videoCodecLimits{maxPixelsPerSecond: stated(2560 * 1440 * 15)}
		return p
	}
	t.Run("asks a smaller frame size at the highest frame rate the offer allows there", func(t *testing.T) {
		asked, _, _ := walk(t, bounded(), unservable, walkOptions{})
		expectAsked(t, asked[5:9], []string{"2560x1440@7-7 800000", "2560x1440@3-3 800000", "1280x720@30-30 800000", "1280x720@15-15 800000"})
	})
	t.Run("keeps the frame rate when it shrinks the frame size for capacity", func(t *testing.T) {
		asked, _, _ := walk(t, bounded(), capacity, walkOptions{})
		expectAsked(t, asked[0:2], []string{"2560x1440@15-15 8000000", "1280x720@15-15 8000000"})
	})
}

// TestVideoWindowSearchCapacityParityMatterjsServer is describe("a window
// refused for capacity") (VideoWindowSearchTest.ts:144-330, matterjs-server
// at 0bd06f2).
//
//nolint:funlen // one subtest per ported case
func TestVideoWindowSearchCapacityParityMatterjsServer(t *testing.T) {
	t.Run("never lowers the bit rate, and lowers the frame rate only when no room can be made", func(t *testing.T) {
		asked, _, _ := walk(t, searchPlan(), capacity, walkOptions{})
		expectAsked(t, asked, []string{"2560x1440@30-30 8000000", "1280x720@30-30 8000000", "640x360@30-30 8000000", "makeRoom", "640x360@15-15 8000000", "makeRoom", "640x360@7-7 8000000", "makeRoom"})
	})
	t.Run("returns to the best servable window once room was made", func(t *testing.T) {
		freed := false
		asked, ok, _ := walk(t, searchPlan(), func(w videoEnvelope) cameraAnswer {
			if freed && w.maxResolution.width == 2560 {
				return accepted
			}
			return refusedCapacity
		}, walkOptions{room: func() bool {
			if freed {
				return false
			}
			freed = true
			return true
		}})
		if !ok {
			t.Error("not accepted")
		}
		expectAsked(t, asked, []string{"2560x1440@30-30 8000000", "1280x720@30-30 8000000", "640x360@30-30 8000000", "makeRoom", "2560x1440@30-30 8000000"})
	})
	t.Run("returns to the window refused for capacity, not the one an unservable step reached", func(t *testing.T) {
		freed := false
		asked, _, _ := walk(t, searchPlan(), func(w videoEnvelope) cameraAnswer {
			switch {
			case w.maxBitRate > 2000000:
				return refusedUnservable
			case freed:
				return accepted
			default:
				return refusedCapacity
			}
		}, walkOptions{room: func() bool { freed = true; return true }})
		expectAsked(t, asked, []string{"2560x1440@30-30 8000000", "2560x1440@30-30 4000000", "2560x1440@30-30 2000000", "1280x720@30-30 2000000", "640x360@30-30 2000000", "makeRoom", "2560x1440@30-30 2000000"})
	})
	t.Run("takes a refused frame rate step from a servable window as the floor, and ends there", func(t *testing.T) {
		asked, ok, _ := walk(t, searchPlan(), func(w videoEnvelope) cameraAnswer {
			if w.maxFrameRate < 30 {
				return refusedUnservable
			}
			return refusedCapacity
		}, walkOptions{})
		if ok {
			t.Error("accepted")
		}
		expectAsked(t, asked, []string{"2560x1440@30-30 8000000", "1280x720@30-30 8000000", "640x360@30-30 8000000", "makeRoom", "640x360@15-15 8000000", "makeRoom"})
	})
	t.Run("lowers the frame rate from the smallest size it already tried, after room ran out", func(t *testing.T) {
		rooms := 1
		asked, _, _ := walk(t, searchPlan(), capacity, walkOptions{room: func() bool { rooms--; return rooms >= 0 }})
		expectAsked(t, asked[:9], []string{"2560x1440@30-30 8000000", "1280x720@30-30 8000000", "640x360@30-30 8000000", "makeRoom", "2560x1440@30-30 8000000", "1280x720@30-30 8000000", "640x360@30-30 8000000", "makeRoom", "640x360@15-15 8000000"})
	})
	t.Run("asks a window again only after room was made since the camera refused it", func(t *testing.T) {
		rooms := 3
		sinceRoom := [][]string{nil}
		asked, _, _ := walk(t, searchPlan(), func(w videoEnvelope) cameraAnswer {
			sinceRoom[len(sinceRoom)-1] = append(sinceRoom[len(sinceRoom)-1], summary(w))
			return refusedCapacity
		}, walkOptions{room: func() bool {
			rooms--
			if rooms < 0 {
				return false
			}
			sinceRoom = append(sinceRoom, nil)
			return true
		}})
		if n := len(slices.DeleteFunc(slices.Clone(asked), func(s string) bool { return s != "makeRoom" })); n <= 3 {
			t.Errorf("made room %d times, want more than 3", n)
		}
		for _, windows := range sinceRoom {
			unique := slices.Compact(slices.Sorted(slices.Values(windows)))
			if len(unique) != len(windows) {
				t.Errorf("a window was asked twice between rooms: %q", windows)
			}
		}
	})
	t.Run("tries the smaller sizes again after room was made, before it takes another stream", func(t *testing.T) {
		rooms := 0
		asked, ok, _ := walk(t, searchPlan(), func(w videoEnvelope) cameraAnswer {
			if rooms > 0 && w.maxResolution.width <= 1280 {
				return accepted
			}
			return refusedCapacity
		}, walkOptions{room: func() bool { rooms++; return rooms <= 2 }})
		if !ok {
			t.Error("not accepted")
		}
		expectAsked(t, asked, []string{"2560x1440@30-30 8000000", "1280x720@30-30 8000000", "640x360@30-30 8000000", "makeRoom", "2560x1440@30-30 8000000", "1280x720@30-30 8000000"})
	})
	// Another controller may allocate meanwhile, so the budget after room is
	// made can be tighter.
	t.Run("does not return to a window the camera refused as unservable once room was made", func(t *testing.T) {
		tight := false
		asked, ok, _ := walk(t, searchPlan(), func(w videoEnvelope) cameraAnswer {
			switch {
			case w.maxResolution.width == 1280 && w.maxBitRate == 8000000:
				return refusedUnservable
			case tight:
				return accepted
			default:
				return refusedCapacity
			}
		}, walkOptions{
			fits: func(w videoEnvelope) bool { return !tight || w.maxResolution.width <= 1280 },
			room: func() bool { tight = true; return true },
		})
		if !ok {
			t.Error("not accepted")
		}
		expectAsked(t, asked, []string{"2560x1440@30-30 8000000", "1280x720@30-30 8000000", "1280x720@30-30 4000000", "640x360@30-30 4000000", "makeRoom", "1280x720@30-30 4000000"})
	})
	t.Run("asks the window refused for capacity as it is after room was made, when no step is left", func(t *testing.T) {
		fixed := searchPlan()
		fixed.envelope.minResolution, fixed.envelope.maxResolution = resolution{1280, 720}, resolution{1280, 720}
		fixed.envelope.minBitRate, fixed.envelope.maxBitRate = 2000000, 2000000
		roomMade := false
		asked, ok, _ := walk(t, fixed, func(w videoEnvelope) cameraAnswer {
			switch {
			case w.maxFrameRate < 30:
				return refusedUnservable
			case roomMade:
				return accepted
			default:
				return refusedCapacity
			}
		}, walkOptions{
			fits: func(w videoEnvelope) bool { return w.maxFrameRate < 15 },
			room: func() bool { roomMade = true; return true },
		})
		if !ok {
			t.Error("not accepted")
		}
		expectAsked(t, asked, []string{"1280x720@7-7 2000000", "1280x720@30-30 2000000", "makeRoom", "1280x720@30-30 2000000"})
	})
}

// TestVideoWindowSearchBitRateFloorParityMatterjsServer is describe("the bit
// rate floor") (VideoWindowSearchTest.ts:332-418, matterjs-server at 0bd06f2).
func TestVideoWindowSearchBitRateFloorParityMatterjsServer(t *testing.T) {
	offChain := spLargePoint
	offChain.minBitRate = 3000000
	pointsPlan := func(points ...rateDistortionPoint) videoPlan {
		return *planOfSelection(t, selectVideo(spPointCapabilities(points...), codecOnly(spH265), nil))
	}
	for _, order := range []struct {
		name     string
		reversed bool
	}{{"smallest first", false}, {"largest first", true}} {
		listed := func(points ...rateDistortionPoint) []rateDistortionPoint {
			if order.reversed {
				slices.Reverse(points)
			}
			return points
		}
		t.Run("is the trade-off point for each window's own size, with points listed "+order.name, func(t *testing.T) {
			var acceptedWindow videoEnvelope
			asked, ok, _ := walk(t, pointsPlan(listed(spSmallPoint, spLargePoint)...), func(w videoEnvelope) cameraAnswer {
				if w.maxResolution.width > 960 || w.maxBitRate >= 1500000 {
					return refusedUnservable
				}
				acceptedWindow = w
				return accepted
			}, walkOptions{})
			if !ok {
				t.Error("not accepted")
			}
			expectAsked(t, asked, []string{"1920x1080@30-30 8000000", "1920x1080@30-30 4000000", "1920x1080@30-30 2000000", "1920x1080@15-15 2000000", "1920x1080@7-7 2000000", "960x540@30-30 2000000", "960x540@30-30 1000000"})
			if acceptedWindow.minBitRate != 500000 {
				t.Errorf("accepted minBitRate = %d, want 500000", acceptedWindow.minBitRate)
			}
		})
		t.Run("keeps the halvings taken across a size step, with points listed "+order.name, func(t *testing.T) {
			asked, _, _ := walk(t, pointsPlan(listed(spSmallPoint, offChain)...), unservable, walkOptions{})
			want := []string{
				"1920x1080@30-30 8000000", "1920x1080@30-30 4000000", "1920x1080@30-30 3000000", "1920x1080@15-15 3000000", "1920x1080@7-7 3000000",
				"960x540@30-30 2000000", "960x540@30-30 1000000", "960x540@30-30 500000", "960x540@15-15 500000", "960x540@7-7 500000",
				"640x360@30-30 500000", "640x360@15-15 500000", "640x360@7-7 500000",
			}
			// The certification window, unless it is the full-size window
			// already refused.
			if !order.reversed {
				want = append(want, "1920x1080@30-30 500000")
			}
			expectAsked(t, asked, want)
		})
	}
	t.Run("halves to the floor of the size the budget fits, not of the size the search wanted", func(t *testing.T) {
		var acceptedWindow videoEnvelope
		asked, ok, _ := walk(t, pointsPlan(spSmallPoint, spLargePoint), func(w videoEnvelope) cameraAnswer {
			if w.maxBitRate > 1000000 {
				return refusedUnservable
			}
			acceptedWindow = w
			return accepted
		}, walkOptions{fits: func(w videoEnvelope) bool { return encodedPixelRate(w) <= 960*540*30 }})
		if !ok {
			t.Error("not accepted")
		}
		expectAsked(t, asked, []string{"960x540@30-30 8000000", "960x540@30-30 4000000", "960x540@30-30 2000000", "960x540@30-30 1000000"})
		if acceptedWindow.minBitRate != 500000 {
			t.Errorf("accepted minBitRate = %d, want 500000", acceptedWindow.minBitRate)
		}
	})
}

// TestVideoWindowSearchBudgetParityMatterjsServer is describe("the encoder
// budget") (VideoWindowSearchTest.ts:420-497, matterjs-server at 0bd06f2).
func TestVideoWindowSearchBudgetParityMatterjsServer(t *testing.T) {
	upTo720p30 := func(w videoEnvelope) bool { return encodedPixelRate(w) <= 1280*720*30 }
	acceptAll := func(videoEnvelope) cameraAnswer { return accepted }
	t.Run("asks the largest frame size the budget carries, at the same frame rate", func(t *testing.T) {
		plan := searchPlan()
		asked, _, search := walk(t, plan, acceptAll, walkOptions{fits: upTo720p30})
		expectAsked(t, asked, []string{"1280x720@30-30 8000000"})
		n, ok := search.budgetNarrowing(videoRetryWindow(&plan, videoRetrySteps{resolution: 1}))
		if !ok || n != (videoBudgetNarrowing{maxResolution: resolution{2560, 1440}}) {
			t.Errorf("narrowing = %+v, %v", n, ok)
		}
	})
	t.Run("lowers the frame rate for the budget only once no frame size fits", func(t *testing.T) {
		asked, _, _ := walk(t, searchPlan(), acceptAll, walkOptions{fits: func(w videoEnvelope) bool { return encodedPixelRate(w) <= 640*360*15 }})
		expectAsked(t, asked, []string{"640x360@15-15 8000000"})
	})
	t.Run("asks the camera before it makes room, and returns to the best window after", func(t *testing.T) {
		freed := false
		asked, _, _ := walk(t, searchPlan(), func(videoEnvelope) cameraAnswer {
			if freed {
				return accepted
			}
			return refusedCapacity
		}, walkOptions{fits: func(videoEnvelope) bool { return freed }, room: func() bool { freed = true; return true }})
		expectAsked(t, asked, []string{"640x360@30-30 8000000", "makeRoom", "2560x1440@30-30 8000000"})
	})
	t.Run("makes no room while the camera accepts what the budget predicted it would not", func(t *testing.T) {
		asked, ok, _ := walk(t, searchPlan(), acceptAll, walkOptions{fits: func(videoEnvelope) bool { return false }, room: func() bool { return true }})
		if !ok {
			t.Error("not accepted")
		}
		expectAsked(t, asked, []string{"640x360@30-30 8000000"})
	})
	// Not even 640x360 at 7 fps fits at first, so the budget cannot pick a
	// window.
	t.Run("after making room, asks a smaller size the budget now carries instead of making room again", func(t *testing.T) {
		free, rooms := 1000000, 0
		fits := func(w videoEnvelope) bool { return encodedPixelRate(w) <= free }
		asked, _, _ := walk(t, searchPlan(), func(w videoEnvelope) cameraAnswer {
			if fits(w) {
				return accepted
			}
			return refusedCapacity
		}, walkOptions{fits: fits, room: func() bool { rooms++; free += 30000000; return true }})
		if rooms != 1 {
			t.Errorf("made room %d times, want 1", rooms)
		}
		expectAsked(t, asked, []string{"640x360@30-30 8000000", "makeRoom", "1280x720@30-30 8000000"})
	})
	// The size is pinned, the camera serves nothing under 30 fps, and
	// nothing can be freed.
	t.Run("restores a frame rate the budget lowered once the camera refuses it, and keeps the floor", func(t *testing.T) {
		pinned := searchPlan()
		pinned.envelope.minResolution = searchWindow.maxResolution
		asked, ok, search := walk(t, pinned, func(w videoEnvelope) cameraAnswer {
			if w.maxFrameRate < 30 {
				return refusedUnservable
			}
			return refusedCapacity
		}, walkOptions{fits: func(w videoEnvelope) bool { return encodedPixelRate(w) <= 2560*1440*7 }})
		if ok {
			t.Error("accepted")
		}
		expectAsked(t, asked, []string{"2560x1440@7-7 8000000", "2560x1440@30-30 8000000", "makeRoom"})
		if search.outcome() != refusedCapacity {
			t.Errorf("outcome = %v, want capacity", search.outcome())
		}
	})
	// Bit rate and frame rate cannot step, so a refused full size is given
	// up for 1280x720.
	t.Run("reports nothing narrowed when the camera, not the budget, made the request give up size", func(t *testing.T) {
		sizeOnly := searchPlan()
		sizeOnly.envelope.minBitRate = searchWindow.maxBitRate
		sizeOnly.frameRateFloor = 30
		for _, refusal := range []videoRefusal{refusedUnservable, refusedCapacity} {
			asked, _, search := walk(t, sizeOnly, func(w videoEnvelope) cameraAnswer {
				if w.maxResolution.width == 2560 {
					return refusal
				}
				return accepted
			}, walkOptions{})
			if asked[len(asked)-1] != "1280x720@30-30 8000000" {
				t.Errorf("refusal %v: asked %q", refusal, asked)
			}
			if _, ok := search.budgetNarrowing(videoRetryWindow(&sizeOnly, videoRetrySteps{resolution: 1})); ok {
				t.Errorf("refusal %v: the budget narrowed", refusal)
			}
		}
	})
}

// TestVideoWindowSearchCertificationParityMatterjsServer is describe("the
// certification window") (VideoWindowSearchTest.ts:499-574, matterjs-server
// at 0bd06f2).
func TestVideoWindowSearchCertificationParityMatterjsServer(t *testing.T) {
	certified := func() videoPlan {
		p := searchPlan()
		p.envelope.minFrameRate, p.envelope.maxFrameRate, p.frameRateCeiling = 60, 60, 60
		cert := searchWindow
		cert.minFrameRate, cert.maxFrameRate, cert.minBitRate, cert.maxBitRate = 30, 60, 800000, 800000
		p.certificationWindow = &cert
		return p
	}
	const certSummary = "2560x1440@30-60 800000"
	t.Run("is asked once the steps run out and every refusal was unservable", func(t *testing.T) {
		asked, ok, _ := walk(t, certified(), func(w videoEnvelope) cameraAnswer {
			if w.minFrameRate == 30 && w.maxFrameRate == 60 {
				return accepted
			}
			return refusedUnservable
		}, walkOptions{})
		if !ok || asked[len(asked)-1] != certSummary {
			t.Errorf("asked %q, accepted %v", asked, ok)
		}
		if n := len(slices.DeleteFunc(slices.Clone(asked), func(s string) bool { return s != certSummary })); n != 1 {
			t.Errorf("asked the certification window %d times", n)
		}
	})
	// The step at 800000 bit/s has the same ceilings, but its bit rate range
	// starts at 100000.
	t.Run("is told apart from a refused step window with the same ceilings", func(t *testing.T) {
		same := searchPlan()
		same.envelope.minBitRate, same.envelope.maxBitRate = 100000, 6400000
		cert := same.envelope
		cert.minBitRate, cert.maxBitRate = 800000, 800000
		same.certificationWindow = &cert
		asked, ok, _ := walk(t, same, func(w videoEnvelope) cameraAnswer {
			if w.minBitRate == 800000 {
				return accepted
			}
			return refusedUnservable
		}, walkOptions{})
		if !ok || !slices.Contains(asked, "2560x1440@30-30 800000") {
			t.Errorf("asked %q, accepted %v", asked, ok)
		}
	})
	t.Run("takes the last ask the limit leaves", func(t *testing.T) {
		plan := certified()
		search := newVideoWindowSearch(&plan, anyBudget, 3)
		var asked []string
		for move := search.move; move.kind == moveAsk; move = search.move {
			asked = append(asked, summary(move.window))
			search.refuse(refusedUnservable)
		}
		expectAsked(t, asked, []string{"2560x1440@60-60 8000000", "2560x1440@60-60 4000000", certSummary})
	})
	t.Run("is not asked once the camera refused a window for capacity", func(t *testing.T) {
		asked, _, _ := walk(t, certified(), func(w videoEnvelope) cameraAnswer {
			if w.maxBitRate == 8000000 {
				return refusedCapacity
			}
			return refusedUnservable
		}, walkOptions{})
		if slices.Contains(asked, certSummary) {
			t.Errorf("asked %q", asked)
		}
	})
	t.Run("is asked again after room was made when the camera refused it for capacity", func(t *testing.T) {
		freed := false
		asked, ok, _ := walk(t, certified(), func(w videoEnvelope) cameraAnswer {
			switch {
			case w.minFrameRate != 30 || w.maxFrameRate != 60:
				return refusedUnservable
			case freed:
				return accepted
			default:
				return refusedCapacity
			}
		}, walkOptions{room: func() bool { freed = true; return true }})
		if !ok {
			t.Error("not accepted")
		}
		expectAsked(t, asked[len(asked)-3:], []string{certSummary, "makeRoom", certSummary})
	})
}

// TestVideoWindowSearchParityMatterjsServer is the top-level cases of
// describe("VideoWindowSearch") (VideoWindowSearchTest.ts:576-627,
// matterjs-server at 0bd06f2).
func TestVideoWindowSearchParityMatterjsServer(t *testing.T) {
	t.Run("reports capacity as the reason once the camera refused any window for capacity", func(t *testing.T) {
		plan := searchPlan()
		search := newVideoWindowSearch(&plan, anyBudget, 0)
		if search.outcome() != refusedUnservable {
			t.Errorf("outcome = %v, want unservable", search.outcome())
		}
		search.refuse(refusedCapacity)
		search.noRoom()
		search.refuse(refusedUnservable)
		if search.outcome() != refusedCapacity {
			t.Errorf("outcome = %v, want capacity", search.outcome())
		}
	})
	t.Run("skips a step that leaves the window unchanged", func(t *testing.T) {
		p := searchPlan()
		p.envelope.minResolution, p.envelope.minBitRate, p.frameRateFloor = searchWindow.maxResolution, searchWindow.maxBitRate, 30
		asked, _, _ := walk(t, p, unservable, walkOptions{})
		expectAsked(t, asked, []string{"2560x1440@30-30 8000000"})
	})
	t.Run("keeps every window inside the caller's bounds and the offer's ceilings", func(t *testing.T) {
		caps := videoCapabilities{
			sensor: resolution{2560, 1440}, maxFrameRate: 60, minViewport: resolution{640, 360},
			rateDistortionPoints: []rateDistortionPoint{{codec: spH265, resolution: resolution{1280, 720}, minBitRate: 400000}},
			maxNetworkBandwidth:  stated(8000000),
		}
		limits := selectedVideoCodecLimits{codec: spH265, videoCodecLimits: videoCodecLimits{maxPixels: stated(1920 * 1080), maxPixelsPerSecond: stated(1920 * 1080 * 40)}}
		bounded := planOfSelection(t, selectVideo(caps, limits, rangeHints(videoRangeBounds{minFrameRate: 20, maxFrameRate: 50, minBitRate: 3000000, maxBitRate: 6000000, minResolution: resolution{1280, 720}})))
		search := newVideoWindowSearch(bounded, anyBudget, 0)
		var windows []videoEnvelope
		for move := search.move; move.kind == moveAsk; move = search.move {
			windows = append(windows, move.window)
			search.refuse(refusedUnservable)
		}
		if len(windows) <= 3 {
			t.Fatalf("%d windows, want more than 3", len(windows))
		}
		for _, w := range windows {
			switch {
			case w.minFrameRate != w.maxFrameRate,
				w.maxFrameRate < 20 || w.maxFrameRate > 50,
				w.maxFrameRate*w.maxResolution.pixels() > 1920*1080*40,
				w.minBitRate != 3000000,
				w.maxBitRate < 3000000 || w.maxBitRate > 6000000,
				w.maxResolution.width < 1280 || w.maxResolution.width > 1920,
				w.maxResolution.height < 720 || w.maxResolution.height > 1080:
				t.Errorf("window %+v leaves the bounds", w)
			}
		}
	})
}

// TestFirstVideoWindowParityMatterjsServer is describe("firstVideoWindow")
// (VideoWindowSearchTest.ts:629-644, matterjs-server at 0bd06f2).
func TestFirstVideoWindowParityMatterjsServer(t *testing.T) {
	plan := searchPlan()
	t.Run("is the plan's window when the budget carries it", func(t *testing.T) {
		if w, ok := firstVideoWindow(&plan, anyBudget); !ok || w != searchWindow {
			t.Errorf("first = %+v, %v", w, ok)
		}
	})
	t.Run("is the first window the budget lets through", func(t *testing.T) {
		w, ok := firstVideoWindow(&plan, func(w videoEnvelope) bool { return encodedPixelRate(w) <= 1280*720*30 })
		if !ok || summary(w) != "1280x720@30-30 8000000" {
			t.Errorf("first = %s, %v", summary(w), ok)
		}
	})
	t.Run("is nothing when no window fits the budget", func(t *testing.T) {
		if _, ok := firstVideoWindow(&plan, func(videoEnvelope) bool { return false }); ok {
			t.Error("a window fits")
		}
	})
}
