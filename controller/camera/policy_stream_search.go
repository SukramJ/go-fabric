// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

// videoRetryDimension is one ceiling a search lowers.
type videoRetryDimension uint8

// The three dimensions, in the order an unservable window gives them up.
const (
	dimBitRate videoRetryDimension = iota
	dimFrameRate
	dimResolution
)

// videoRetrySteps says how many times each ceiling of a videoPlan is
// halved. frameRateTarget, when hasTarget, is the rate frame rate counts
// down from (the plan's first rate otherwise): a size step for an
// unservable window starts it again at the highest rate the smaller size
// allows. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:11-22 at 0bd06f2.
type videoRetrySteps struct {
	bitRate, frameRate, resolution int
	frameRateTarget                int
	hasTarget                      bool
}

// of is the steps taken in d.
func (s videoRetrySteps) of(d videoRetryDimension) int {
	switch d {
	case dimBitRate:
		return s.bitRate
	case dimFrameRate:
		return s.frameRate
	default:
		return s.resolution
	}
}

// with is s with count steps in d.
func (s videoRetrySteps) with(d videoRetryDimension, count int) videoRetrySteps {
	switch d {
	case dimBitRate:
		s.bitRate = count
	case dimFrameRate:
		s.frameRate = count
	default:
		s.resolution = count
	}
	return s
}

// maxVideoAsks is the number of windows asked per request, eviction retries
// included; it bounds how long the endpoint lock is held. A camera refusing
// everything from a start of at most 8 Mbit/s takes at most 31: 1 + 22 bit
// rate halvings from 8 Mbit/s to a floor of 1 + 2 frame rate steps + 2 size
// steps with 2 frame rate steps each. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:24-31 at 0bd06f2.
const maxVideoAsks = 32

// boundedLimits caps the frame rate and frame size steps, so a camera
// refusing everything fails after a known number of attempts; bit rate is
// not bounded (bitRateSteps). Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:33-40 at 0bd06f2.
type boundedLimits struct {
	frameRate, resolution int
}

var defaultBoundedLimits = boundedLimits{frameRate: 2, resolution: 2}

// of is the limit of a bounded dimension.
func (l boundedLimits) of(d videoRetryDimension) int {
	if d == dimFrameRate {
		return l.frameRate
	}
	return l.resolution
}

// bitRateSteps is the bit rate halvings that reach the floor of the frame
// size resolutionSteps size steps make: the trade-off point for that size.
// Profiles hide their bit rate ceilings (a tested camera caps at 2 Mbit/s),
// so no fixed count reaches them. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:42-54 at 0bd06f2.
func bitRateSteps(plan *videoPlan, resolutionSteps int) int {
	floor := planBitRateFloor(plan, retryResolution(plan, resolutionSteps))
	steps := 0
	for rate := plan.envelope.maxBitRate; rate > floor; rate = max(floor, rate/2) {
		steps++
	}
	return steps
}

// retryDimension is a dimension and its traits: unpublishedFloor — no
// attribute states how low the camera goes, so a lower value can make a
// servable window unservable (resolution has MinViewport, bit rate the
// trade-off point); chargesEncoder — lowering it charges the encoder budget
// less (MaxEncodedPixelRate, §11.2.7.2). Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:56-68 at 0bd06f2.
type retryDimension struct {
	name             videoRetryDimension
	unpublishedFloor bool
	chargesEncoder   bool
}

// retryDimensions is the order an unservable window is narrowed in: the
// quality given up first comes first. Bit rate first, since no attribute
// publishes a profile's bit rate ceiling; then frame rate, then frame size:
// a stream keeps its frame size longest. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:70-80 at 0bd06f2.
var retryDimensions = []retryDimension{
	{name: dimBitRate, unpublishedFloor: false, chargesEncoder: false},
	{name: dimFrameRate, unpublishedFloor: true, chargesEncoder: true},
	{name: dimResolution, unpublishedFloor: false, chargesEncoder: true},
}

// videoRetryWindow is the plan's window with each ceiling lowered as often
// as steps says, never below its floor. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:82-96 at 0bd06f2.
func videoRetryWindow(plan *videoPlan, steps videoRetrySteps) videoEnvelope {
	env := plan.envelope
	maxResolution := retryResolution(plan, steps.resolution)
	frameRate := env.maxFrameRate
	if steps.hasTarget {
		frameRate = steps.frameRateTarget
	}
	for range steps.frameRate {
		frameRate = max(plan.frameRateFloor, frameRate/2)
	}
	minBitRate := planBitRateFloor(plan, maxResolution)
	maxBitRate := env.maxBitRate
	for range steps.bitRate {
		maxBitRate = max(minBitRate, maxBitRate/2)
	}
	w := withFrameRate(env, frameRate)
	w.maxResolution, w.minBitRate, w.maxBitRate = maxResolution, minBitRate, maxBitRate
	return w
}

// retryResolution is the plan's frame size after steps halvings. Mirrors
// matterjs-server packages/ws-controller/src/camera/videoWindowSearch.ts:98-102
// at 0bd06f2.
func retryResolution(plan *videoPlan, steps int) resolution {
	r := plan.envelope.maxResolution
	for range steps {
		r = halveResolution(r, plan.envelope.minResolution)
	}
	return r
}

// videoRefusal is the camera's answer to a window: unservable
// (DynamicConstraintError: outside every profile) or capacity
// (ResourceExhausted). The reference camera app checks profiles first, so a
// window refused for capacity is servable. Only the camera refuses: the
// encoder budget predicts, it never refuses. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:104-109 at 0bd06f2.
type videoRefusal uint8

// The two refusals.
const (
	refusedUnservable videoRefusal = iota + 1
	refusedCapacity
)

// searchStep is how the search got to a window: from which steps, along
// which dimension.
type searchStep struct {
	from      videoRetrySteps
	dimension retryDimension
}

// moveKind is what a search asks of its caller.
type moveKind uint8

// The three moves: ask the camera for a window; free a stream, then call
// roomMade, or noRoom when nothing could be freed; give up.
const (
	moveAsk moveKind = iota
	moveMakeRoom
	moveGiveUp
)

// videoSearchMove is the search's next move; window is set for moveAsk.
// Mirrors matterjs-server packages/ws-controller/src/camera/videoWindowSearch.ts:116-120
// at 0bd06f2.
type videoSearchMove struct {
	kind   moveKind
	window videoEnvelope
}

// fitted is where the encoder budget moved a wanted window: the steps to
// ask, whether they fit, whether the budget (not the camera) lowered the
// frame rate on the way, and the steps after the budget's size steps and
// before any frame rate step (steps itself when it lowered no rate).
// Mirrors matterjs-server packages/ws-controller/src/camera/videoWindowSearch.ts:122-129
// at 0bd06f2.
type fitted struct {
	steps             videoRetrySteps
	fits              bool
	budgetLoweredRate bool
	beforeRate        videoRetrySteps
}

// fitToBudget is the window to ask instead of wanted when wanted costs more
// than the free encoder budget: the capacity order, frame size first at the
// same rate, then frame rate. When nothing fits, the smallest size at the
// wanted rate, which the camera then decides on. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:131-157 at 0bd06f2.
func fitToBudget(plan *videoPlan, wanted videoRetrySteps, limits boundedLimits, fits func(videoEnvelope) bool) fitted {
	steps := wanted
	if fits(videoRetryWindow(plan, steps)) {
		return fitted{steps: steps, fits: true, beforeRate: steps}
	}
	for count := steps.resolution + 1; count <= limits.resolution; count++ {
		steps.resolution = count
		if fits(videoRetryWindow(plan, steps)) {
			return fitted{steps: steps, fits: true, beforeRate: steps}
		}
	}
	beforeRate := steps
	for count := steps.frameRate + 1; count <= limits.frameRate; count++ {
		slower := beforeRate
		slower.frameRate = count
		if fits(videoRetryWindow(plan, slower)) {
			return fitted{steps: slower, fits: true, budgetLoweredRate: true, beforeRate: beforeRate}
		}
	}
	return fitted{steps: beforeRate, beforeRate: beforeRate}
}

// refusedWindow is a window the camera refused, and how.
type refusedWindow struct {
	window  videoEnvelope
	refusal videoRefusal
}

// videoWindowSearch is the only thing that changes a request window. It
// walks the windows of a videoPlan, best first; every window lies inside
// the plan, so inside the caller's bounds and the offer's decode ceiling.
//
//   - fits is the encoder budget, and it only predicts: a window it rejects
//     is replaced by the first one in the capacity order that fits
//     (fitToBudget), which is then asked. Only the camera's answers count as
//     refusals, so nothing is freed before the camera answers
//     ResourceExhausted.
//   - A step never proposes a window the camera refused, compared as the
//     budget would fit it. An unservable refusal holds for the whole
//     search; a capacity refusal only until room is made.
//   - Each window's bit rate floor is the trade-off point for its own frame
//     size; a size step keeps the bit rate halvings taken.
//   - Unservable: lower the first dimension in retryDimensions order that
//     can still step; a frame size step starts frame rate again at the
//     highest rate the smaller size allows.
//   - Capacity: lower a dimension that charges the encoder and has a
//     published floor, keeping the frame rate; without one, ask for room.
//     Only when no room can be made, lower one with an unpublished floor.
//   - Unservable right after a frame rate lowered for capacity: the rate
//     before is restored, and the camera's floor found becomes the frame
//     rate limit.
//   - Last rung: while the camera has refused every window as unservable,
//     the plan's certification window is asked once, when the steps run out
//     or on the last ask askLimit leaves.
//   - At most askLimit windows are asked.
//
// Mirrors matterjs-server packages/ws-controller/src/camera/videoWindowSearch.ts:159-418
// at 0bd06f2. A search is used by one goroutine at a time.
type videoWindowSearch struct {
	plan *videoPlan
	fits func(videoEnvelope) bool
	// asked are the steps asked last, wanted the steps the search wanted
	// before the budget moved them.
	asked, wanted videoRetrySteps
	budgetBefore  *videoRetrySteps
	limits        boundedLimits
	lastStep      *searchStep
	refused       []refusedWindow
	bestServable  *videoRetrySteps
	// capacityRefused are the first steps the camera refused for capacity
	// since room was last made: servable.
	capacityRefused *videoRetrySteps
	capacitySeen    bool
	budgetMoved     bool
	askLimit        int
	asks            int
	askedWindow     videoEnvelope
	lastRungAsked   bool
	move            videoSearchMove
}

// newVideoWindowSearch starts a search over plan; askLimit 0 is
// maxVideoAsks. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:210-216 at 0bd06f2.
func newVideoWindowSearch(plan *videoPlan, fits func(videoEnvelope) bool, askLimit int) *videoWindowSearch {
	if askLimit == 0 {
		askLimit = maxVideoAsks
	}
	s := &videoWindowSearch{plan: plan, fits: fits, askLimit: askLimit, limits: defaultBoundedLimits, askedWindow: plan.envelope}
	s.move = s.settle(s.propose(videoRetrySteps{}, nil))
	return s
}

// asksLeft is the asks the camera can still be given, which is what makes
// freeing room worth it. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:222-225 at 0bd06f2.
func (s *videoWindowSearch) asksLeft() int { return s.askLimit - s.asks }

// outcome is why the search ended without a stream: capacity once the
// camera refused any window for capacity, since it can serve that one and
// only room is missing. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:227-233 at 0bd06f2.
func (s *videoWindowSearch) outcome() videoRefusal {
	if s.capacitySeen {
		return refusedCapacity
	}
	return refusedUnservable
}

// budgetNarrowing is the first window's ceilings the budget made the
// request give up, ok false when it cost nothing. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:235-248 at 0bd06f2.
func (s *videoWindowSearch) budgetNarrowing(accepted videoEnvelope) (videoBudgetNarrowing, bool) {
	if !s.budgetMoved {
		return videoBudgetNarrowing{}, false
	}
	first := s.plan.envelope
	rateLowered := accepted.maxFrameRate < first.maxFrameRate
	sizeLowered := accepted.maxResolution.width < first.maxResolution.width ||
		accepted.maxResolution.height < first.maxResolution.height
	if !rateLowered && !sizeLowered {
		return videoBudgetNarrowing{}, false
	}
	var n videoBudgetNarrowing
	if rateLowered {
		n.maxFrameRate = first.maxFrameRate
	}
	if sizeLowered {
		n.maxResolution = first.maxResolution
	}
	return n, true
}

// refuse records the camera's answer to the window of the last ask.
// Mirrors matterjs-server packages/ws-controller/src/camera/videoWindowSearch.ts:250-253
// at 0bd06f2.
func (s *videoWindowSearch) refuse(refusal videoRefusal) {
	s.move = s.settle(s.answered(refusal))
}

// roomMade continues after a stream was freed. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:255-258 at 0bd06f2.
func (s *videoWindowSearch) roomMade() {
	kept := s.refused[:0:0]
	for _, r := range s.refused {
		if r.refusal == refusedUnservable {
			kept = append(kept, r)
		}
	}
	s.refused = kept
	s.move = s.settle(s.afterRoom())
}

// noRoom continues after nothing could be freed. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:260-269 at 0bd06f2.
func (s *videoWindowSearch) noRoom() {
	if s.lastRungAsked {
		s.move = s.settle(videoSearchMove{kind: moveGiveUp})
		return
	}
	var dims []retryDimension
	for _, d := range retryDimensions {
		if d.unpublishedFloor && d.chargesEncoder {
			dims = append(dims, d)
		}
	}
	s.move = s.settle(s.step(dims, refusedCapacity))
}

// settle passes every move: the ask limit, and the certification window as
// the last rung while every refusal was unservable. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:271-297 at 0bd06f2.
func (s *videoWindowSearch) settle(move videoSearchMove) videoSearchMove {
	cert := s.plan.certificationWindow
	lastRungOpen := cert != nil && !s.lastRungAsked && !s.capacitySeen && !s.wasRefused(*cert)
	if lastRungOpen && (move.kind == moveGiveUp || (move.kind == moveAsk && s.asksLeft() == 1)) {
		s.lastRungAsked = true
		return s.count(videoSearchMove{kind: moveAsk, window: *cert})
	}
	if move.kind == moveAsk {
		if s.asksLeft() == 0 {
			return videoSearchMove{kind: moveGiveUp}
		}
		return s.count(move)
	}
	return move
}

// count records an ask.
func (s *videoWindowSearch) count(move videoSearchMove) videoSearchMove {
	s.asks++
	s.askedWindow = move.window
	return move
}

// wasRefused reports whether the camera refused w in any way.
func (s *videoWindowSearch) wasRefused(w videoEnvelope) bool {
	for _, r := range s.refused {
		if sameVideoEnvelope(r.window, w) {
			return true
		}
	}
	return false
}

// afterRoom returns to the best window refused for capacity, through the
// budget, now with the freed room. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:299-313 at 0bd06f2.
func (s *videoWindowSearch) afterRoom() videoSearchMove {
	if s.lastRungAsked {
		return videoSearchMove{kind: moveAsk, window: s.askedWindow}
	}
	best := s.wanted
	if s.bestServable != nil {
		best = *s.bestServable
	}
	capacityRefused := s.capacityRefused
	s.bestServable, s.capacityRefused = nil, nil
	move := s.propose(best, nil)
	if move.kind == moveAsk && s.wasRefused(move.window) {
		stepped := s.after(refusedUnservable)
		// A stream was freed for this search, so it asks at least once more.
		if stepped.kind == moveGiveUp && capacityRefused != nil {
			return s.askAsIs(*capacityRefused)
		}
		return stepped
	}
	return move
}

// answered handles a refusal. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:315-338 at 0bd06f2.
func (s *videoWindowSearch) answered(refusal videoRefusal) videoSearchMove {
	if refusal == refusedCapacity {
		s.capacitySeen = true
		if s.capacityRefused == nil {
			asked := s.asked
			s.capacityRefused = &asked
		}
	}
	s.refused = append(s.refused, refusedWindow{window: s.askedWindow, refusal: refusal})
	if s.lastRungAsked {
		if refusal == refusedCapacity {
			return videoSearchMove{kind: moveMakeRoom}
		}
		return videoSearchMove{kind: moveGiveUp}
	}
	if refusal == refusedUnservable && s.budgetBefore != nil {
		return s.askAsIs(*s.budgetBefore)
	}
	if last := s.lastStep; refusal == refusedUnservable && last != nil && last.dimension.unpublishedFloor &&
		s.refusalOf(last.from) == refusedCapacity {
		name := last.dimension.name
		if name == dimFrameRate {
			s.limits.frameRate = last.from.frameRate
		} else {
			s.limits.resolution = last.from.resolution
		}
		s.asked, s.wanted = last.from, last.from
		return s.after(refusedCapacity)
	}
	return s.after(refusal)
}

// after steps on from a refusal. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:340-346 at 0bd06f2.
func (s *videoWindowSearch) after(refusal videoRefusal) videoSearchMove {
	if refusal == refusedUnservable {
		return s.step(retryDimensions, refusal)
	}
	if s.bestServable == nil {
		wanted := s.wanted
		s.bestServable = &wanted
	}
	var safe []retryDimension
	for _, d := range retryDimensions {
		if d.chargesEncoder && !d.unpublishedFloor {
			safe = append(safe, d)
		}
	}
	move := s.step(safe, refusedCapacity)
	if move.kind == moveGiveUp {
		return videoSearchMove{kind: moveMakeRoom}
	}
	return move
}

// step steps over windows the camera already refused, so a later step
// starts from the furthest one. An unservable window steps from what the
// search wanted, which the budget then fits again; a window refused for
// capacity steps from what was asked, since it must charge the encoder
// less, so a size step then keeps the frame rate. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:348-380 at 0bd06f2.
func (s *videoWindowSearch) step(dimensions []retryDimension, refusal videoRefusal) videoSearchMove {
	position := s.asked
	if refusal == refusedUnservable {
		position = s.wanted
	}
	for _, d := range dimensions {
		var limit int
		if d.name == dimBitRate {
			limit = bitRateSteps(s.plan, s.fitted(position).resolution)
		} else {
			limit = s.limits.of(d.name)
		}
		for count := position.of(d.name) + 1; count <= limit; count++ {
			next := position.with(d.name, count)
			if d.name == dimResolution && refusal == refusedUnservable {
				next = position
				next.resolution, next.frameRate = count, 0
				next.frameRateTarget, next.hasTarget = planFrameRate(s.plan, retryResolution(s.plan, count)), true
			}
			if s.isRefused(next) {
				position = next
				continue
			}
			return s.propose(next, &searchStep{from: position, dimension: d})
		}
	}
	s.asked = position
	return videoSearchMove{kind: moveGiveUp}
}

// propose asks for wanted, or for the window the budget points to instead.
// Mirrors matterjs-server packages/ws-controller/src/camera/videoWindowSearch.ts:382-391
// at 0bd06f2.
func (s *videoWindowSearch) propose(wanted videoRetrySteps, last *searchStep) videoSearchMove {
	f := fitToBudget(s.plan, wanted, s.limits, s.fits)
	if f.steps != wanted {
		s.budgetMoved = true
	}
	s.wanted, s.asked, s.lastStep = wanted, f.steps, last
	s.budgetBefore = nil
	if f.budgetLoweredRate {
		before := f.beforeRate
		s.budgetBefore = &before
	}
	return videoSearchMove{kind: moveAsk, window: videoRetryWindow(s.plan, f.steps)}
}

// askAsIs asks for steps whatever the budget predicts: the camera decides.
// Mirrors matterjs-server packages/ws-controller/src/camera/videoWindowSearch.ts:393-400
// at 0bd06f2.
func (s *videoWindowSearch) askAsIs(steps videoRetrySteps) videoSearchMove {
	s.wanted, s.asked, s.lastStep, s.budgetBefore = steps, steps, nil, nil
	return videoSearchMove{kind: moveAsk, window: videoRetryWindow(s.plan, steps)}
}

// fitted is the steps steps would be asked as. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:402-405 at 0bd06f2.
func (s *videoWindowSearch) fitted(steps videoRetrySteps) videoRetrySteps {
	return fitToBudget(s.plan, steps, s.limits, s.fits).steps
}

// isRefused reports whether the camera refused the window steps would be
// asked as. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:407-411 at 0bd06f2.
func (s *videoWindowSearch) isRefused(steps videoRetrySteps) bool {
	return s.wasRefused(videoRetryWindow(s.plan, s.fitted(steps)))
}

// refusalOf is the camera's latest answer for the window steps make, 0 when
// it gave none. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:413-417 at 0bd06f2.
func (s *videoWindowSearch) refusalOf(steps videoRetrySteps) videoRefusal {
	w := videoRetryWindow(s.plan, steps)
	var latest videoRefusal
	for _, r := range s.refused {
		if sameVideoEnvelope(r.window, w) {
			latest = r.refusal
		}
	}
	return latest
}

// firstVideoWindow is the first window an allocate would ask that the
// encoder budget fits carries, ok false when no window fits, so an allocate
// could only succeed by freeing room or if the camera accepts a window the
// budget said would not fit. Mirrors matterjs-server
// packages/ws-controller/src/camera/videoWindowSearch.ts:429-437 at 0bd06f2.
func firstVideoWindow(plan *videoPlan, fits func(videoEnvelope) bool) (videoEnvelope, bool) {
	f := fitToBudget(plan, videoRetrySteps{}, defaultBoundedLimits, fits)
	if !f.fits {
		return videoEnvelope{}, false
	}
	return videoRetryWindow(plan, f.steps), true
}
