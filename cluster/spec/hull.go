// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"

	"github.com/SukramJ/go-fabric/contract"
)

// Hull is the forwarding half of a generated typed server
// (<cluster>def.Server embeds it): it holds the cluster's server — any
// [contract.ClusterServer], the generated [Server] or a self-held one —
// and implements contract.ClusterServer and the optional interfaces the
// dispatcher and the bridge assert on a mounted server by forwarding to
// it.
//
// A forwarded optional interface the held server does not implement
// answers what the dispatcher does without it where that is a value (no
// data version, the reportable attributes as the attribute list, no
// commands or events, view to read, no extra privilege, not timed, the
// unfiltered read, no change reports) — but the dispatcher also skips
// checks for an absent lister, which a value cannot express. So a host
// mounts the held server ([Hull.Underlying]), not the typed one; the
// device layer mounts its servers as they are and wraps on access.
type Hull struct{ srv contract.ClusterServer }

// NewHull returns a hull over srv.
func NewHull(srv contract.ClusterServer) Hull { return Hull{srv: srv} }

// Underlying returns the server the hull holds.
func (h Hull) Underlying() contract.ClusterServer { return h.srv }

// Compile-time assertions: the forwarding list.
var (
	_ contract.ClusterServer                  = Hull{}
	_ contract.ClusterDataVersion             = Hull{}
	_ contract.ClusterAttributeLister         = Hull{}
	_ contract.ClusterCommandLister           = Hull{}
	_ contract.ClusterEventLister             = Hull{}
	_ contract.ClusterAttributeReadPrivilege  = Hull{}
	_ contract.ClusterAttributeWritePrivilege = Hull{}
	_ contract.ClusterCommandInvokePrivilege  = Hull{}
	_ contract.TimedCommander                 = Hull{}
	_ contract.FabricScopedReader             = Hull{}
	_ contract.AttributeChangeNotifier        = Hull{}
	_ contract.ChangeNotifier                 = Hull{}
	_ contract.SelfReportedAttributeLister    = Hull{}
	_ contract.ClusterQuiescer                = Hull{}
	_ contract.EventReceiver                  = Hull{}
)

// MatterClusterID implements [contract.ClusterServer].
func (h Hull) MatterClusterID() uint32 { return h.srv.MatterClusterID() }

// MatterRead implements [contract.ClusterServer].
func (h Hull) MatterRead(attrID uint32) (any, bool) { return h.srv.MatterRead(attrID) }

// MatterWrite implements [contract.ClusterServer].
func (h Hull) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return h.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke implements [contract.ClusterServer].
func (h Hull) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return h.srv.MatterInvoke(ctx, cmdID, fields)
}

// MatterReportable implements [contract.ClusterServer].
func (h Hull) MatterReportable() []uint32 { return h.srv.MatterReportable() }

// MatterDataVersion implements [contract.ClusterDataVersion]; 0 without one.
func (h Hull) MatterDataVersion() uint32 {
	if dv, ok := h.srv.(contract.ClusterDataVersion); ok {
		return dv.MatterDataVersion()
	}
	return 0
}

// MatterAttributes implements [contract.ClusterAttributeLister]; the
// reportable attributes without one.
func (h Hull) MatterAttributes() []uint32 {
	if l, ok := h.srv.(contract.ClusterAttributeLister); ok {
		return l.MatterAttributes()
	}
	return h.srv.MatterReportable()
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]; none
// without one.
func (h Hull) MatterAcceptedCommands() []uint32 {
	if l, ok := h.srv.(contract.ClusterCommandLister); ok {
		return l.MatterAcceptedCommands()
	}
	return []uint32{}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]; none
// without one.
func (h Hull) MatterGeneratedCommands() []uint32 {
	if l, ok := h.srv.(contract.ClusterCommandLister); ok {
		return l.MatterGeneratedCommands()
	}
	return []uint32{}
}

// MatterEvents implements [contract.ClusterEventLister]; none without one.
func (h Hull) MatterEvents() []uint32 {
	if l, ok := h.srv.(contract.ClusterEventLister); ok {
		return l.MatterEvents()
	}
	return []uint32{}
}

// MinReadPrivilege implements [contract.ClusterAttributeReadPrivilege];
// view (1) without one.
func (h Hull) MinReadPrivilege(attrID uint32) uint8 {
	if p, ok := h.srv.(contract.ClusterAttributeReadPrivilege); ok {
		return p.MinReadPrivilege(attrID)
	}
	return 1
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege];
// no requirement beyond the dispatcher's (0) without one.
func (h Hull) MinWritePrivilege(attrID uint32) uint8 {
	if p, ok := h.srv.(contract.ClusterAttributeWritePrivilege); ok {
		return p.MinWritePrivilege(attrID)
	}
	return 0
}

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege];
// no requirement beyond the dispatcher's (0) without one.
func (h Hull) MinInvokePrivilege(cmdID uint32) uint8 {
	if p, ok := h.srv.(contract.ClusterCommandInvokePrivilege); ok {
		return p.MinInvokePrivilege(cmdID)
	}
	return 0
}

// IsTimedCommand implements [contract.TimedCommander]; false without one.
func (h Hull) IsTimedCommand(cmdID uint32) bool {
	if tc, ok := h.srv.(contract.TimedCommander); ok {
		return tc.IsTimedCommand(cmdID)
	}
	return false
}

// MatterReadFiltered implements [contract.FabricScopedReader]; the
// unfiltered read without one.
func (h Hull) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	if fr, ok := h.srv.(contract.FabricScopedReader); ok {
		return fr.MatterReadFiltered(ctx, attrID)
	}
	return h.srv.MatterRead(attrID)
}

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier]:
// the held server's, or its [contract.ChangeNotifier] with every
// reportable attribute; no reports without either.
func (h Hull) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	switch n := h.srv.(type) {
	case contract.AttributeChangeNotifier:
		return n.OnMatterAttributesChanged(cb)
	case contract.ChangeNotifier:
		return n.OnMatterValueChanged(func() { cb(h.srv.MatterReportable()) })
	default:
		return func() {}
	}
}

// OnMatterValueChanged implements [contract.ChangeNotifier]: the held
// server's, or its [contract.AttributeChangeNotifier]; no reports without
// either.
func (h Hull) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	switch n := h.srv.(type) {
	case contract.ChangeNotifier:
		return n.OnMatterValueChanged(cb)
	case contract.AttributeChangeNotifier:
		return n.OnMatterAttributesChanged(func([]uint32) { cb() })
	default:
		return func() {}
	}
}

// MatterSelfReportedAttributes implements
// [contract.SelfReportedAttributeLister]; none without one.
func (h Hull) MatterSelfReportedAttributes() []uint32 {
	if l, ok := h.srv.(contract.SelfReportedAttributeLister); ok {
		return l.MatterSelfReportedAttributes()
	}
	return nil
}

// MatterQuiesce implements [contract.ClusterQuiescer]; nothing without one.
func (h Hull) MatterQuiesce() {
	if q, ok := h.srv.(contract.ClusterQuiescer); ok {
		q.MatterQuiesce()
	}
}

// SetMatterEventEmitter implements [contract.EventReceiver]; nothing
// without one.
func (h Hull) SetMatterEventEmitter(emitter contract.EventEmitter) {
	if r, ok := h.srv.(contract.EventReceiver); ok {
		r.SetMatterEventEmitter(emitter)
	}
}
