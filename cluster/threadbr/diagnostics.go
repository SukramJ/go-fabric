// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package threadbr

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tnd "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/thread"
)

// ThreadNetworkDiagnostics (0x0035) read from a border router. What each
// attribute reads follows connectedhomeip's
// src/app/clusters/thread-network-diagnostics-server/
// DirectThreadNetworkDiagnosticsProvider.cpp at 6170af84 (DTNDP below),
// which reads an in-process OpenThread instance; here the instance is the
// border router behind the [BorderRouter] port, its active dataset and its
// own diagnostics record (ADR 0027 decision 3).

// DiagnosticsFeatures are the features the server serves: the MLE and MAC
// counts (owner decision 2 of ADR 0027). PacketCounts and ErrorCounts stay
// off; the REST surface carries neither the packet counts nor an overrun
// count, and ResetCounts has no REST counterpart.
const DiagnosticsFeatures = tnd.FeatureMleCounts | tnd.FeatureMacCounts

// ErrDiagnosticsFeature is a feature selection the server does not serve.
var ErrDiagnosticsFeature = errors.New("threadbr: ThreadNetworkDiagnostics serves MleCounts and MacCounts only")

// mleCountAttrs are the MleCounts attributes (DTNDP :325-371).
var mleCountAttrs = []uint32{
	tnd.AttrDetachedRoleCount, tnd.AttrChildRoleCount, tnd.AttrRouterRoleCount, tnd.AttrLeaderRoleCount,
	tnd.AttrAttachAttemptCount, tnd.AttrPartitionIdChangeCount, tnd.AttrBetterPartitionAttachAttemptCount,
	tnd.AttrParentChangeCount,
}

// macCountAttrs are the MacCounts attributes a border router's MAC
// Counters record answers one to one: the unicast and broadcast frames
// sent and received (DTNDP :379-390, :481-492 read mTxUnicast,
// mTxBroadcast, mRxUnicast, mRxBroadcast; the record's ifOutUcastPkts,
// ifOutBroadcastPkts, ifInUcastPkts, ifInBroadcastPkts,
// tlv/diag/MacCounters.ts:17-27 at matter.js 862ad75f). The other
// MacCounts attributes are optional under the feature ("[MACCNT]") and
// have no counterpart in the record; they are not served.
var macCountAttrs = []uint32{
	tnd.AttrTxUnicastCount, tnd.AttrTxBroadcastCount, tnd.AttrRxUnicastCount, tnd.AttrRxBroadcastCount,
}

// DiagnosticsConfig carries the ThreadNetworkDiagnostics server's
// construction parameters.
type DiagnosticsConfig struct {
	// BorderRouter is the border router the server reads; required.
	BorderRouter BorderRouter
	// Features is the FeatureMap selection, a subset of
	// [DiagnosticsFeatures] ([ErrDiagnosticsFeature] otherwise).
	Features tnd.Feature
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
	// Logger receives the refreshes that fail; slog.Default when nil.
	Logger *slog.Logger
}

// DiagnosticsServer implements [contract.ClusterServer] for
// ThreadNetworkDiagnostics: the mandatory attributes (the provisional
// ExtAddress and Rloc16 among them) and, with the features, the MLE counts
// and the MAC counts the border router's record carries, read from the
// [BorderRouter] at construction and at every [DiagnosticsServer.Refresh].
// It accepts no command and emits no event.
type DiagnosticsServer struct {
	*spec.Instance
	// SelfHeld is the typed server's write veto and SetAttributes over srv.
	spec.SelfHeld

	srv      *spec.Server
	br       BorderRouter
	features tnd.Feature
	logger   *slog.Logger
}

// Compile-time assertions.
var (
	_ contract.ClusterServer           = (*DiagnosticsServer)(nil)
	_ contract.ClusterDataVersion      = (*DiagnosticsServer)(nil)
	_ contract.ClusterAttributeLister  = (*DiagnosticsServer)(nil)
	_ contract.AttributeChangeNotifier = (*DiagnosticsServer)(nil)
)

// NewDiagnostics builds the ThreadNetworkDiagnostics server over
// cfg.BorderRouter and reads it once; a border router that does not
// answer starts the server with the values of an unconfigured node
// (DTNDP :65-92), and the error is logged.
func NewDiagnostics(ctx context.Context, cfg DiagnosticsConfig) (*DiagnosticsServer, error) {
	if cfg.BorderRouter == nil {
		return nil, ErrNoBorderRouter
	}
	if cfg.Features&^DiagnosticsFeatures != 0 {
		return nil, fmt.Errorf("%w: 0x%X", ErrDiagnosticsFeature, uint32(cfg.Features))
	}
	s := &DiagnosticsServer{br: cfg.BorderRouter, features: cfg.Features, logger: cfg.Logger}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	// The provisional ExtAddress and Rloc16 ("P, M") are optional to the
	// conformance (cluster/spec/conformance.go); chip serves both in its
	// mandatory range (ThreadNetworkDiagnosticsCluster.cpp:80-83).
	optional := []uint32{tnd.AttrExtAddress, tnd.AttrRloc16}
	if cfg.Features&tnd.FeatureMleCounts != 0 {
		optional = append(optional, mleCountAttrs...)
	}
	if cfg.Features&tnd.FeatureMacCounts != 0 {
		optional = append(optional, macCountAttrs...)
	}
	initial, err := s.read(ctx)
	if err != nil {
		s.logger.Warn("threadbr.diagnostics.refresh_failed", slog.Any("err", err))
	}
	srv, err := spec.NewServer(tnd.Definition, spec.Options{Features: uint32(cfg.Features), Attributes: optional}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Initial:     initial,
	})
	if err != nil {
		return nil, fmt.Errorf("threadbr: %w", err)
	}
	s.Instance, s.SelfHeld, s.srv = srv.Instance, spec.NewSelfHeld(srv), srv
	return s, nil
}

// Refresh reads the border router again and reports what moved; a border
// router that does not answer reads as an unconfigured node and the error
// is returned.
func (s *DiagnosticsServer) Refresh(ctx context.Context) error {
	values, err := s.read(ctx)
	if setErr := s.srv.SetAttributes(values); setErr != nil {
		return errors.Join(err, setErr)
	}
	return err
}

// read is every served attribute's value now.
func (s *DiagnosticsServer) read(ctx context.Context) (map[uint32]any, error) {
	diag, diagErr := s.br.Diagnostics(ctx)
	ds, dsErr := s.br.Dataset(ctx, false)
	var view thread.DatasetView
	commissioned := false
	if dsErr == nil && ds != nil {
		if v, err := thread.NewDatasetView(ds); err == nil {
			view, commissioned = v, v.IsCommissioned()
		}
	}
	values := unconfigured()
	if diagErr == nil {
		values[tnd.AttrRoutingRole] = routingRole(diag)
		values[tnd.AttrNeighborTable] = neighborTable(diag)
		values[tnd.AttrRouteTable] = routeTable(diag)
	}
	if commissioned {
		datasetValues(view, values)
	}
	if commissioned && diagErr == nil {
		values[tnd.AttrPartitionId] = diag.LeaderData.PartitionID                     // DTNDP :295-299
		values[tnd.AttrWeighting] = uint16(diag.LeaderData.Weighting)                 // :301-305
		values[tnd.AttrDataVersion] = uint16(diag.LeaderData.DataVersion)             // :307-311
		values[tnd.AttrStableDataVersion] = uint16(diag.LeaderData.StableDataVersion) // :313-317
		values[tnd.AttrLeaderRouterId] = diag.LeaderData.LeaderRouterID               // :319-323
		values[tnd.AttrExtAddress] = extAddress(diag.ExtAddress)                      // :672-686
		values[tnd.AttrRloc16] = diag.Rloc16                                          // :687-689
	}
	if s.features&tnd.FeatureMleCounts != 0 {
		var c thread.MleCounters
		if diag.Self != nil && diag.Self.MleCounters != nil {
			c = *diag.Self.MleCounters
		}
		values[tnd.AttrDetachedRoleCount] = c.DetachedRole // :325-329
		values[tnd.AttrChildRoleCount] = c.ChildRole
		values[tnd.AttrRouterRoleCount] = c.RouterRole
		values[tnd.AttrLeaderRoleCount] = c.LeaderRole
		values[tnd.AttrAttachAttemptCount] = c.AttachAttempts
		values[tnd.AttrPartitionIdChangeCount] = c.PartitionIDChanges
		values[tnd.AttrBetterPartitionAttachAttemptCount] = c.BetterPartitionAttachAttempts
		values[tnd.AttrParentChangeCount] = c.ParentChanges // :367-371
	}
	if s.features&tnd.FeatureMacCounts != 0 {
		var c thread.MacCounters
		if diag.Self != nil && diag.Self.MacCounters != nil {
			c = *diag.Self.MacCounters
		}
		values[tnd.AttrTxUnicastCount] = c.IfOutUcastPkts       // :379-383
		values[tnd.AttrTxBroadcastCount] = c.IfOutBroadcastPkts // :385-389
		values[tnd.AttrRxUnicastCount] = c.IfInUcastPkts        // :481-485
		values[tnd.AttrRxBroadcastCount] = c.IfInBroadcastPkts  // :487-491
	}
	var errs []error
	if diagErr != nil {
		errs = append(errs, fmt.Errorf("threadbr: Diagnostics: %w", diagErr))
	}
	if dsErr != nil {
		errs = append(errs, fmt.Errorf("threadbr: Dataset: %w", dsErr))
	}
	return values, errors.Join(errs...)
}

// unconfigured is a node without a configured dataset (DTNDP :65-92): the
// nullable attributes null, RoutingRole Unspecified, the lists empty
// (:699-702 serve them empty without a Thread stack).
func unconfigured() map[uint32]any {
	values := map[uint32]any{
		tnd.AttrRoutingRole:             tnd.RoutingRoleUnspecified,
		tnd.AttrNeighborTable:           spec.List[tnd.NeighborTableStruct]{},
		tnd.AttrRouteTable:              spec.List[tnd.RouteTableStruct]{},
		tnd.AttrActiveNetworkFaultsList: []any{}, // :666-670
	}
	for _, id := range []uint32{
		tnd.AttrChannel, tnd.AttrNetworkName, tnd.AttrPanId, tnd.AttrExtendedPanId, tnd.AttrMeshLocalPrefix,
		tnd.AttrPartitionId, tnd.AttrWeighting, tnd.AttrDataVersion, tnd.AttrStableDataVersion,
		tnd.AttrLeaderRouterId, tnd.AttrSecurityPolicy, tnd.AttrChannelPage0Mask,
		tnd.AttrOperationalDatasetComponents, tnd.AttrExtAddress, tnd.AttrRloc16,
	} {
		values[id] = nil
	}
	return values
}

// datasetValues sets what DTNDP reads from the active dataset and the
// network it describes: Channel (:98-102), NetworkName (:149-153), PanId
// (:155-159), ExtendedPanId as a big-endian integer (:161-165),
// MeshLocalPrefix with its prefix length first (:167-176), SecurityPolicy
// (:606-622), ChannelPage0Mask (:624-641) and OperationalDatasetComponents
// (:643-664). A sub-TLV the dataset does not carry in chip's size keeps
// its attribute null.
func datasetValues(v thread.DatasetView, values map[uint32]any) {
	if ch, err := v.Channel(); err == nil {
		values[tnd.AttrChannel] = ch
	}
	if name, err := v.NetworkName(); err == nil {
		values[tnd.AttrNetworkName] = name
	}
	if pan, err := v.PANID(); err == nil {
		values[tnd.AttrPanId] = pan
	}
	if xp, err := v.ExtPANID(); err == nil {
		values[tnd.AttrExtendedPanId] = binary.BigEndian.Uint64(xp)
	}
	if mlp, err := v.MeshLocalPrefix(); err == nil {
		// OT_IP6_PREFIX_BITSIZE (64) is the octet string's first byte.
		values[tnd.AttrMeshLocalPrefix] = append([]byte{thread.MeshLocalPrefixLen * 8}, mlp...)
	}
	if sp, err := v.SecurityPolicy(); err == nil {
		values[tnd.AttrSecurityPolicy] = tnd.SecurityPolicy{RotationTime: sp.RotationTime, Flags: sp.Flags}
	}
	values[tnd.AttrChannelPage0Mask] = channelPage0Mask(v)
	values[tnd.AttrOperationalDatasetComponents] = tnd.OperationalDatasetComponents{
		ActiveTimestampPresent:  v.Has(thread.TLVActiveTimestamp),
		PendingTimestampPresent: v.Has(thread.TLVPendingTimestamp),
		MasterKeyPresent:        v.Has(thread.TLVNetworkKey),
		NetworkNamePresent:      v.Has(thread.TLVNetworkName),
		ExtendedPanIdPresent:    v.Has(thread.TLVExtPANID),
		MeshLocalPrefixPresent:  v.Has(thread.TLVMeshLocalPrefix),
		DelayPresent:            v.Has(thread.TLVDelayTimer),
		PanIdPresent:            v.Has(thread.TLVPANID),
		ChannelPresent:          v.Has(thread.TLVChannel),
		PskcPresent:             v.Has(thread.TLVPSKc),
		SecurityPolicyPresent:   v.Has(thread.TLVSecurityPolicy),
		ChannelMaskPresent:      v.Has(thread.TLVChannelMask),
	}
}

// channelPage0Mask is the four-byte mask of channel page 0, the most
// significant bit of its first byte channel 0 (DTNDP :624-641, which
// bit-swaps OpenThread's mChannelMask back into that order). The dataset's
// Channel Mask sub-TLV already carries it so: entries of [page][length]
// [mask], the mask most significant bit first (Thread spec §8.10.1.18). A
// mask of another length is cut or zero-filled to four bytes; a dataset
// without one for page 0 reads all zeros, as chip's mask of no channel.
func channelPage0Mask(v thread.DatasetView) []byte {
	out := make([]byte, 4)
	entries, err := v.ChannelMask()
	if err != nil {
		return out
	}
	for i := 0; i+2 <= len(entries); {
		page, n := entries[i], int(entries[i+1])
		end := min(i+2+n, len(entries))
		if page == 0 {
			copy(out, entries[i+2:end])
			return out
		}
		i = end
	}
	return out
}

// routingRole maps the border router's role as DTNDP :104-145 maps
// otDeviceRole: disabled Unspecified, detached Unassigned, router Router,
// leader Leader; a child is a REED as a router-eligible FTD (:129-136), an
// EndDevice when its record says it is not a full Thread device, a
// SleepyEndDevice when it is not rx-on-when-idle (:126-141).
func routingRole(d thread.NodeDiagnostics) tnd.RoutingRoleEnum {
	switch d.Role {
	case thread.RoleDisabled:
		return tnd.RoutingRoleUnspecified
	case thread.RoleDetached:
		return tnd.RoutingRoleUnassigned
	case thread.RoleRouter:
		return tnd.RoutingRoleRouter
	case thread.RoleLeader:
		return tnd.RoutingRoleLeader
	case thread.RoleChild:
		if d.Self != nil && d.Self.Mode != nil {
			switch {
			case !d.Self.Mode.RxOnWhenIdle:
				return tnd.RoutingRoleSleepyEndDevice
			case !d.Self.Mode.FullThreadDevice:
				return tnd.RoutingRoleEndDevice
			}
		}
		return tnd.RoutingRoleReed
	}
	return tnd.RoutingRoleUnspecified
}

// rloc16Of is a router's RLOC16, its router id in the upper six bits
// (Thread spec §5.2.1).
func rloc16Of(routerID uint8) uint16 { return uint16(routerID) << 10 }

// invalidRouterID is OpenThread's kInvalidRouterId, the next hop of a
// router without a link to it.
const invalidRouterID = 63

// meshExtAddress is the extended address the mesh's records give rloc16,
// 0 when none does.
func meshExtAddress(d thread.NodeDiagnostics, rloc16 uint16) uint64 {
	for _, r := range d.Mesh {
		if r.Rloc16 != nil && *r.Rloc16 == rloc16 && len(r.ExtMACAddress) == 8 {
			return binary.BigEndian.Uint64(r.ExtMACAddress)
		}
	}
	return 0
}

// routeTable is RouteTable (DTNDP :242-293, one entry per allocated router)
// from the border router's own Route64 record: a router's RLOC16 is its id
// shifted, its extended address the one the mesh's record for that RLOC16
// names, the path cost the route cost and the link qualities the
// record's; a link is established where both qualities are non-zero, and
// such a router is its own next hop, any other the invalid router id. The
// record carries no age; it reads 0.
func routeTable(d thread.NodeDiagnostics) spec.List[tnd.RouteTableStruct] {
	out := spec.List[tnd.RouteTableStruct]{}
	if d.Self == nil || d.Self.Route64 == nil {
		return out
	}
	for _, e := range d.Self.Route64.Entries {
		linked := e.LinkQualityIn > 0 && e.LinkQualityOut > 0
		next := uint8(invalidRouterID)
		if linked {
			next = e.RouterID
		}
		out = append(out, tnd.RouteTableStruct{
			ExtAddress: meshExtAddress(d, rloc16Of(e.RouterID)), Rloc16: rloc16Of(e.RouterID),
			RouterId: e.RouterID, NextHop: next, PathCost: e.RouteCost,
			LqiIn: e.LinkQualityIn, LqiOut: e.LinkQualityOut, Allocated: true, LinkEstablished: linked,
		})
	}
	return out
}

// neighborTable is NeighborTable (DTNDP :184-240) from the border router's
// own record: each child of its Child Table (RLOC16 the border router's
// with the child id, the mode bits its record's, IsChild) and each router
// its Route64 record has a link to (a full Thread device, rx-on, full
// network data). The record carries neither frame counters, error rates
// nor RSSI: they read 0 and null.
func neighborTable(d thread.NodeDiagnostics) spec.List[tnd.NeighborTableStruct] {
	out := spec.List[tnd.NeighborTableStruct]{}
	if d.Self == nil {
		return out
	}
	null := spec.Nullable[int8]{Null: true}
	for _, c := range d.Self.ChildTable {
		rloc16 := d.Rloc16&0xfc00 | c.ChildID&0x03ff
		out = append(out, tnd.NeighborTableStruct{
			ExtAddress: meshExtAddress(d, rloc16), Rloc16: rloc16, Lqi: c.IncomingLinkQuality,
			AverageRssi: null, LastRssi: null,
			RxOnWhenIdle: c.Mode.RxOnWhenIdle, FullThreadDevice: c.Mode.FullThreadDevice,
			FullNetworkData: c.Mode.FullNetworkData, IsChild: true,
		})
	}
	if d.Self.Route64 != nil {
		for _, e := range d.Self.Route64.Entries {
			if e.LinkQualityIn == 0 || e.LinkQualityOut == 0 {
				continue
			}
			out = append(out, tnd.NeighborTableStruct{
				ExtAddress: meshExtAddress(d, rloc16Of(e.RouterID)), Rloc16: rloc16Of(e.RouterID), Lqi: e.LinkQualityIn,
				AverageRssi: null, LastRssi: null, RxOnWhenIdle: true, FullThreadDevice: true, FullNetworkData: true,
			})
		}
	}
	return out
}

// extAddress is the border router's extended address as a big-endian
// integer (DTNDP :672-686), null when it is not 8 bytes.
func extAddress(b []byte) any {
	if len(b) != 8 {
		return nil
	}
	return binary.BigEndian.Uint64(b)
}

// SetAttributes implements [spec.AttributeSetter]: every attribute is the
// border router's ([spec.ErrNotHeld] for a value other than the one
// served); [DiagnosticsServer.Refresh] reads them again.
func (s *DiagnosticsServer) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		cur, _ := s.srv.Value(id)
		if !sameValue(cur, values[id]) {
			return fmt.Errorf("%w: threadbr: attribute 0x%04X follows the border router (Refresh)", spec.ErrNotHeld, id)
		}
	}
	return nil
}

// --- plumbing -------------------------------------------------------------

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *DiagnosticsServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (s *DiagnosticsServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.srv.OnAttributeChange(cb)
}

// MatterRead implements [contract.ClusterServer].
func (s *DiagnosticsServer) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite implements [contract.ClusterServer]: every attribute is
// read-only, which the definition answers.
func (s *DiagnosticsServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke implements [contract.ClusterServer]: ResetCounts is
// ErrorCounts' (ThreadNetworkDiagnosticsCluster.cpp:237-250), which the
// server does not serve; the definition answers.
func (s *DiagnosticsServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &s.SelfHeld, cmdID, fields)
}
