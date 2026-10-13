// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"log/slog"
	"strconv"
	"time"
)

// statusState is the loop-owned state of a service's [Status].
type statusState struct {
	connecting            map[*int]struct{}
	connectionInitiatedAt time.Time
	lastReceiptAt         time.Time
	resolveCancel         context.CancelFunc
	resolving             chan struct{}
}

// Status tracks the reachability of a [Service] and drives its discovery:
// while the service is not reachable and a connection attempt is
// outstanding, it resolves the service actively ([Service.Resolve]) until
// the service is reachable again with no attempt outstanding. It is
// matter.js IpServiceStatus
// (packages/general/src/net/dns-sd/IpServiceStatus.ts:23-205 at 0ceb202d);
// [Service.Reachable] is its isReachable flag.
type Status struct {
	s *Service
}

// Status returns the service's status.
func (s *Service) Status() *Status { return &Status{s: s} }

// Reachable is [Service.Reachable].
func (st *Status) Reachable() bool { return st.s.Reachable() }

// SetReachable sets the reachability: clearing it while a connection
// attempt is outstanding starts resolving, setting it with none
// outstanding stops a resolution. Mirrors IpServiceStatus.ts:78-90.
func (st *Status) SetReachable(reachable bool) {
	_ = st.s.names.loop.call(func() { st.s.setReachable(reachable) })
}

// IsConnecting reports whether a connection attempt registered with
// [Status.Connecting] is outstanding (IpServiceStatus.ts:48-50).
func (st *Status) IsConnecting() bool {
	var out bool
	_ = st.s.names.loop.call(func() { out = len(st.s.status.connecting) > 0 })
	return out
}

// IsResolving reports whether the status is resolving the service
// (IpServiceStatus.ts:74-76).
func (st *Status) IsResolving() bool {
	var out bool
	_ = st.s.names.loop.call(func() { out = st.s.status.resolving != nil })
	return out
}

// ConnectionInitiatedAt is when the latest connection attempt was
// registered, zero once none is outstanding (IpServiceStatus.ts:100-102,
// :119-123, :146).
func (st *Status) ConnectionInitiatedAt() time.Time {
	var out time.Time
	_ = st.s.names.loop.call(func() { out = st.s.status.connectionInitiatedAt })
	return out
}

// LastReceiptAt is the last receipt from the service's node a connection
// recorded, zero when none (IpServiceStatus.ts:92-98).
func (st *Status) LastReceiptAt() time.Time {
	var out time.Time
	_ = st.s.names.loop.call(func() { out = st.s.status.lastReceiptAt })
	return out
}

// SetLastReceiptAt records a receipt from the service's node.
func (st *Status) SetLastReceiptAt(at time.Time) {
	_ = st.s.names.loop.call(func() { st.s.status.lastReceiptAt = at })
}

// Connecting registers a connection attempt whose outcome result
// delivers: true marks the service reachable, false — or result closing
// without a value, matter.js's rejected promise — unreachable. While any
// attempt is outstanding [Status.IsConnecting] is true; the status starts
// resolving if the service is not reachable. Mirrors
// IpServiceStatus.ts:104-150.
func (st *Status) Connecting(result <-chan bool) {
	s := st.s
	token := new(int)
	if err := s.names.loop.call(func() {
		if s.status.connecting == nil {
			s.status.connecting = make(map[*int]struct{})
		}
		s.names.logger.Debug("matter.discovery.status.connecting", slog.String("service", s.name.qname))
		s.status.connectionInitiatedAt = s.names.clock.Now()
		s.status.connecting[token] = struct{}{}
		s.maybeStartResolving()
	}); err != nil {
		return
	}
	go func() {
		var connected, settled bool
		select {
		case connected, settled = <-result:
		case <-s.names.loop.done:
			return
		}
		s.names.loop.post(func() { s.settle(token, connected, settled) })
	}()
}

// settle ends one connection attempt (IpServiceStatus.ts:117-144).
func (s *Service) settle(token *int, connected, settled bool) {
	delete(s.status.connecting, token)
	if len(s.status.connecting) == 0 {
		s.status.connectionInitiatedAt = time.Time{}
	}
	switch {
	case !settled:
		s.names.logger.Warn("matter.discovery.status.connection_error", slog.String("service", s.name.qname))
		s.setReachable(false)
	case connected:
		s.setReachable(true)
		s.names.logger.Info("matter.discovery.status.connected", slog.String("service", s.name.qname))
	default:
		s.setReachable(false)
		s.names.logger.Debug("matter.discovery.status.connect_ended_without_session", slog.String("service", s.name.qname))
	}
}

// setReachable is the isReachable setter (IpServiceStatus.ts:78-90).
func (s *Service) setReachable(reachable bool) {
	if s.reachable == reachable {
		return
	}
	s.reachable = reachable
	if reachable {
		s.maybeStopResolving()
	} else {
		s.maybeStartResolving()
	}
}

// maybeStartResolving runs IpServiceResolution while the service is not
// reachable and an attempt is outstanding (IpServiceStatus.ts:152-186).
func (s *Service) maybeStartResolving() {
	if s.reachable || len(s.status.connecting) == 0 || s.status.resolveCancel != nil || s.closed {
		return
	}
	var why string
	switch n := s.addresses.len(); n {
	case 0:
		why = "no address known"
	case 1:
		why = "address is unreachable"
	default:
		why = "some of the " + strconv.Itoa(n) + " known addresses are unreachable"
	}
	s.names.logger.Info("matter.discovery.status.resolving", slog.String("service", s.name.qname), slog.String("why", why))
	ctx, cancel := context.WithCancel(s.names.ctx)
	done := make(chan struct{})
	s.status.resolveCancel = cancel
	s.status.resolving = done
	go func() {
		defer close(done)
		_, err := s.Resolve(ctx)
		s.names.loop.post(func() {
			if err == nil {
				s.names.logger.Debug("matter.discovery.status.resolved", slog.String("service", s.name.qname), slog.Int("addresses", s.addresses.len()))
			}
			cancel()
			s.status.resolveCancel = nil
			s.status.resolving = nil
		})
	}()
}

// maybeStopResolving stops the resolution once the service is reachable
// and no attempt is outstanding (IpServiceStatus.ts:188-194).
func (s *Service) maybeStopResolving() {
	if !s.reachable || len(s.status.connecting) > 0 || s.status.resolveCancel == nil {
		return
	}
	s.stopResolving()
}

// stopResolving cancels the resolution (IpServiceStatus.ts:196-204).
func (s *Service) stopResolving() {
	if s.status.resolveCancel == nil {
		return
	}
	s.status.resolveCancel()
	s.names.logger.Debug("matter.discovery.status.stopped_resolving", slog.String("service", s.name.qname))
}

// Changes signals every change of the service — its addresses or its TXT
// parameters — coalesced: one pending signal stands for any number of
// changes. The channel closes when ctx ends or the cache closes. It is
// matter.js IpService.changed (IpService.ts:122-124, emitted at :342-355),
// which a peer observes for its discovery data (Peer.ts:126-139).
func (s *Service) Changes(ctx context.Context) <-chan struct{} {
	out := make(chan struct{}, 1)
	var obs *changeObserver
	if err := s.names.loop.call(func() {
		obs = s.onChanged(func() {
			select {
			case out <- struct{}{}:
			default:
			}
		})
	}); err != nil {
		close(out)
		return out
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-s.names.loop.done:
		}
		// call, not post: a task the loop accepted but never ran (it shut
		// down first) still has its channel closed here.
		if err := s.names.loop.call(func() {
			s.offChanged(obs)
			close(out)
		}); err != nil {
			close(out)
		}
	}()
	return out
}
