// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package peer

import "time"

// Timing are the parameters that pace the connection process and the
// address monitor of a peer — matter.js PeerTimingParameters
// (packages/protocol/src/peer/PeerTimingParameters.ts:21-133 at
// 0ceb202d). A zero field is unset: [DefaultTiming] or the base of [Merge]
// supplies it.
type Timing struct {
	// DefaultConnectionTimeout bounds [Peer.Connect] when the caller sets
	// no timeout (:22-28).
	DefaultConnectionTimeout time.Duration
	// MaxDelayBetweenInitialContactRetries caps the backoff between the
	// retransmissions of the first CASE message (:30-35).
	MaxDelayBetweenInitialContactRetries time.Duration
	// DelayBeforeNextAddress staggers the first attempts of two addresses
	// (:37-42).
	DelayBeforeNextAddress time.Duration
	// DelayAfterNetworkError is the pause after a transient error (:44-52).
	DelayAfterNetworkError time.Duration
	// DelayAfterPeerError is the pause after the peer refused the session
	// (:54-59).
	DelayAfterPeerError time.Duration
	// DelayAfterUnhandledError is the pause after any other error (:61-66).
	DelayAfterUnhandledError time.Duration
	// KickThrottleInterval is the shortest interval between two kicks that
	// reach the attempts (:68-74).
	KickThrottleInterval time.Duration
	// KickMinRetransmissions is the retransmissions an exchange must have
	// made before a kick may restart it (:76-82).
	KickMinRetransmissions int
	// KickMinRestartSaving is the least a restart must shorten the wait for
	// the next retransmission to be worth it (:84-92).
	KickMinRestartSaving time.Duration
	// KickRestartCooldown is the shortest interval between two
	// kick-triggered restarts, per trigger (:94-106).
	KickRestartCooldown KickRestartCooldown
	// AddressChangeStabilizationDelay is the quiet time after an address
	// change before the session's address is checked, and before a stale
	// fallback address is retired (:108-114).
	AddressChangeStabilizationDelay time.Duration
	// AddressChangeProbeCooldown bounds the growing interval between
	// address-change probes (:116-132).
	AddressChangeProbeCooldown ProbeCooldown
}

// KickRestartCooldown is the per-trigger restart cooldown of [Timing].
type KickRestartCooldown struct {
	// AddressChange follows a restart a DNS-SD address change triggered.
	AddressChange time.Duration
	// Connect follows a restart [Peer.Kick] triggered.
	Connect time.Duration
}

// ProbeCooldown is the Fibonacci range of the address-change probe
// cooldown.
type ProbeCooldown struct {
	// Min is the first two cooldowns.
	Min time.Duration
	// Max caps the cooldown.
	Max time.Duration
}

// maxInitialContactRetryInterval is PeerTimingParameters.ts:184.
const maxInitialContactRetryInterval = 2 * time.Minute

// DefaultTiming is matter.js PeerTimingParameters.defaults
// (PeerTimingParameters.ts:184-209).
func DefaultTiming() Timing {
	return Timing{
		DefaultConnectionTimeout:             90 * time.Second,
		MaxDelayBetweenInitialContactRetries: maxInitialContactRetryInterval,
		DelayBeforeNextAddress:               45 * time.Second,
		DelayAfterNetworkError:               15 * time.Second,
		DelayAfterPeerError:                  5 * time.Minute,
		DelayAfterUnhandledError:             2 * time.Minute,
		KickThrottleInterval:                 3 * time.Second,
		KickMinRetransmissions:               2,
		KickMinRestartSaving:                 maxInitialContactRetryInterval / 2,
		KickRestartCooldown:                  KickRestartCooldown{AddressChange: 30 * time.Minute, Connect: 10 * time.Minute},
		AddressChangeStabilizationDelay:      10 * time.Second,
		AddressChangeProbeCooldown:           ProbeCooldown{Min: 10 * time.Minute, Max: 24 * time.Hour},
	}
}

// Merge layers the set fields of over onto base, the groups field-wise so
// one member of a group leaves its sibling alone — matter.js
// PeerTimingParameters.merge and applyOverrides (PeerTimingParameters.ts:149-182).
func Merge(base, over Timing) Timing {
	set := func(dst *time.Duration, v time.Duration) {
		if v != 0 {
			*dst = v
		}
	}
	set(&base.DefaultConnectionTimeout, over.DefaultConnectionTimeout)
	set(&base.MaxDelayBetweenInitialContactRetries, over.MaxDelayBetweenInitialContactRetries)
	set(&base.DelayBeforeNextAddress, over.DelayBeforeNextAddress)
	set(&base.DelayAfterNetworkError, over.DelayAfterNetworkError)
	set(&base.DelayAfterPeerError, over.DelayAfterPeerError)
	set(&base.DelayAfterUnhandledError, over.DelayAfterUnhandledError)
	set(&base.KickThrottleInterval, over.KickThrottleInterval)
	if over.KickMinRetransmissions != 0 {
		base.KickMinRetransmissions = over.KickMinRetransmissions
	}
	set(&base.KickMinRestartSaving, over.KickMinRestartSaving)
	set(&base.KickRestartCooldown.AddressChange, over.KickRestartCooldown.AddressChange)
	set(&base.KickRestartCooldown.Connect, over.KickRestartCooldown.Connect)
	set(&base.AddressChangeStabilizationDelay, over.AddressChangeStabilizationDelay)
	set(&base.AddressChangeProbeCooldown.Min, over.AddressChangeProbeCooldown.Min)
	set(&base.AddressChangeProbeCooldown.Max, over.AddressChangeProbeCooldown.Max)
	return base
}
