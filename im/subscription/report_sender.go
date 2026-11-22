// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"context"

	"thingfabric.dev/go-fabric/im"
)

// Report is one report of a subscription: what the engine hands the
// [ReportSender] to send as one ReportData. One pass, one report: the
// attribute changes and the events that are due together are in it
// together.
type Report struct {
	// Paths are the changed attribute paths the report carries, in the
	// priming report's order.
	Paths []im.ConcreteAttributePath
	// Events are the events the report carries, read from the
	// [EventSource] at drain time.
	Events []im.EventReport
	// KeepAlive says the subscription's send interval elapsed with this
	// pass: the report goes out even when nothing is left in it for the
	// subscriber — an empty ReportData is the keep-alive.
	KeepAlive bool
	// Flush marks the report of a subscription whose fabric or session
	// leaves ([Manager.FlushFabric], [Manager.FlushSession]): everything it
	// owes, whatever its intervals; never a keep-alive.
	Flush bool
}

// ReportOutcome is what a [ReportSender] did with a [Report].
type ReportOutcome uint8

const (
	// Sent says the report is the sender's: it went out, or its sending
	// failed and the sender handed the data back
	// ([Subscription.RequeueAttributes], [Subscription.RequeueEvents]), or
	// the subscription it was for is gone. The pass counts as the
	// subscription's report. The zero value.
	Sent ReportOutcome = iota
	// NothingForSubscriber says nothing of the report was left for this
	// subscriber — every event and attribute change it carried was
	// filtered, by the access check or as fabric-sensitive data of another
	// fabric — and the sender sent nothing. The pass does not count as a
	// report: the time of the last report stays. When the pass was the send
	// interval's and carried data, the engine hands the sender the
	// keep-alive alone (a [Report] with only KeepAlive set) — the keep-alive
	// is owed whether or not the data it would have carried was for this
	// subscriber's eyes. Read for an ongoing report only; a flush's outcome
	// is not read.
	NothingForSubscriber
)

// ReportSender sends one [Report] of sub — the subscription's ongoing
// reports, its keep-alive and the flush before its fabric or session
// leaves — and says what it did. The single report hook (review II,
// R2-P6-api-0508, owner decision 138): one hook for one action, so a manager
// is either wired for every report or for none, never half.
//
// The engine calls it outside the manager's lock, one report per
// subscription at a time, on a goroutine of that report's own; it may block
// on the wire. ctx ends when the engine stops ([Manager.Stop]) — or, for a
// flush, with the flush's ctx — and a sender that waits on its peer ends the
// wait with it.
//
// The bridge's sender is installed by bridge.Bridge.AttachSubscriptionManager;
// a host that runs a [Manager] without the bridge sets its own with
// [Manager.SetReportSender].
type ReportSender func(ctx context.Context, sub *Subscription, report Report) ReportOutcome

// SetReportSender sets the hook every report of the manager goes through:
// the ongoing reports, the keep-alives and the flushes. Pass nil to detach.
// Safe to call at any time; a report already handed to the previous sender
// stays with it.
//
// A manager without a sender fails visibly rather than losing reports:
// [Manager.Subscribe] and [Manager.Restore] refuse with [ErrNoReportSender],
// and a subscription admitted before the sender was detached keeps what it
// owes — the engine drains nothing until a sender is set again and logs the
// gap once.
//
// While a deprecated hook is set — [NewManager]'s reporter,
// [Manager.SetEventReporter], [Manager.SetFlushReporter] — the manager
// reports through those as before and the sender is not used: a manager the
// host wired the old way keeps its exact behaviour for the deprecation
// window, whatever installed a sender beside it.
func (m *Manager) SetReportSender(s ReportSender) {
	m.mu.Lock()
	m.sender = s
	if s != nil {
		m.noSenderLogged = false
	}
	m.mu.Unlock()
}

// legacyHooks are the deprecated report hooks of a manager.
type legacyHooks struct {
	reporter Reporter
	events   EventReporter
	flush    FlushReporter
}

func (h legacyHooks) set() bool {
	return h.reporter != nil || h.events != nil || h.flush != nil
}

// senderFor returns the sender a report goes through: the deprecated hooks
// adapted while one is set, else the [ReportSender]; nil when the manager
// has none. For a flush (flush true) of a manager wired the deprecated way
// without a [FlushReporter] it is nil too: such a manager drained and sent
// nothing on a flush, and keeps doing so.
func (m *Manager) senderFor(flush bool) ReportSender {
	m.mu.RLock()
	hooks := legacyHooks{reporter: m.reporter, events: m.eventReporter, flush: m.flushReporter}
	sender := m.sender
	m.mu.RUnlock()
	if !hooks.set() {
		return sender
	}
	if flush && hooks.flush == nil {
		return nil
	}
	return hooks.sender()
}

// sender adapts the deprecated hooks to a [ReportSender], with the
// behaviour they had before the single hook:
//
//   - A flush goes to the [FlushReporter].
//   - A report with events goes to the [EventReporter], with the paths due
//     beside them offered through [Subscription.TakeDuePaths]; the paths it
//     leaves untaken are due again (they follow in an attribute report of
//     their own once MinIntervalFloor has elapsed), and its
//     [Subscription.NothingSent] is [NothingForSubscriber].
//   - Anything else goes to the [Reporter], without the events: a manager
//     without an [EventReporter] drains them and sends none — the silent
//     loss the single hook removes (R2-P6-api-0508), kept unchanged for
//     the window as the README's deprecation policy asks. Without a
//     [Reporter], or with nothing to send, the outcome is
//     [NothingForSubscriber].
func (h legacyHooks) sender() ReportSender {
	return func(ctx context.Context, sub *Subscription, r Report) ReportOutcome {
		if r.Flush {
			h.flush(ctx, sub, r.Events, r.Paths)
			return Sent
		}
		if len(r.Events) > 0 && h.events != nil {
			sub.offerDuePaths(r.Paths)
			h.events(ctx, sub, r.Events)
			if left := sub.TakeDuePaths(); len(left) > 0 {
				// Not a failed update: the paths the event reporter left
				// are still due.
				for _, p := range left {
					sub.markDirty(p)
				}
			}
			if sub.takeSentNothing() {
				return NothingForSubscriber
			}
			return Sent
		}
		if h.reporter == nil || (len(r.Paths) == 0 && !r.KeepAlive) {
			return NothingForSubscriber
		}
		h.reporter(ctx, sub, r.Paths)
		return Sent
	}
}

// logNoSenderOnce logs, once until a sender is set, that a report was due
// and the manager had no [ReportSender] to send it.
func (m *Manager) logNoSenderOnce(what string) {
	m.mu.Lock()
	logged := m.noSenderLogged
	m.noSenderLogged = true
	m.mu.Unlock()
	if logged {
		return
	}
	m.logger.Error("subscription: no ReportSender is set: reports are held, nothing is drained until one is set",
		"report", what)
}
