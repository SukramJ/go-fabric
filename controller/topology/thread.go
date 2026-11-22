// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"slices"
	"strconv"

	"thingfabric.dev/go-fabric/thread/brdiscovery"

	threaddiagdef "thingfabric.dev/go-fabric/cluster/spec/threadnetworkdiagnostics"
)

// threadMaps resolve a neighbour-table entry to a commissioned node: by
// extended address, and by RLOC16 as the fallback — matterjs-server's
// buildExtAddrMap and buildRloc16Map at 0bd06f22
// (topology-utils.ts:385-398, :405-418). A node without a known extended
// address or RLOC16 is absent from that map.
//
// An extended address is global; an RLOC16 is unique only within one
// Thread network, so its claim is keyed by the claimant's extended PAN id
// and looked up with the observer's — matterjs-server's own pattern for
// its diagnostics path (buildMatterRloc16ByXp, topology-utils.ts:442-451 at 0bd06f22);
// a node without an extended PAN id keeps a claim across networks. Two
// nodes that claim one extended address, or one RLOC16 within one network,
// resolve to neither: both are self-reported diagnostics and nothing tells
// which claim is true, so a neighbour entry naming it shows as
// unknown_<ext> rather than as an edge to the node that claimed it last
// (review II, R2-P6-bound-2362, owner decisions 222 and 237).
// matterjs-server keeps the later node's claim (Map.set) and its neighbour
// path uses a global RLOC16 map.
type threadMaps struct {
	byExt  map[uint64]string
	byRloc map[rlocKey]string
	// rlocAny is an RLOC16 exactly one node claims across every network,
	// for an observer that reports no extended PAN id.
	rlocAny map[uint16]string
	// collisions are the claims two nodes made, in the order found.
	collisions []collision
}

// rlocKey is an RLOC16 within the network of an extended PAN id; network
// false is a claimant without one.
type rlocKey struct {
	xpan    uint64
	network bool
	rloc    uint16
}

// collision is a claim two or more nodes made: an extended address, or an
// RLOC16 within one network.
type collision struct {
	kind, value string
	nodes       []string
}

func newThreadMaps(nodes []*Image) threadMaps {
	m := threadMaps{byExt: map[uint64]string{}, byRloc: map[rlocKey]string{}, rlocAny: map[uint16]string{}}
	anyClaims := map[uint16][]string{}
	extClaims, rlocClaims := map[uint64][]string{}, map[rlocKey][]string{}
	var extOrder []uint64
	var rlocOrder []rlocKey
	for _, img := range nodes {
		id := strconv.FormatUint(img.NodeID, 10)
		if ext := threadExtAddress(img.GeneralDiagnostics.NetworkInterfaces); ext != 0 {
			if extClaims[ext] == nil {
				extOrder = append(extOrder, ext)
			}
			extClaims[ext] = appendClaim(extClaims[ext], id)
		}
		if r := img.ThreadNetworkDiagnostics.Rloc16; r != nil {
			k := rlocKey{rloc: *r}
			if xp := img.ThreadNetworkDiagnostics.ExtendedPanId; xp != nil {
				k.xpan, k.network = *xp, true
			}
			if rlocClaims[k] == nil {
				rlocOrder = append(rlocOrder, k)
			}
			rlocClaims[k] = appendClaim(rlocClaims[k], id)
			anyClaims[*r] = appendClaim(anyClaims[*r], id)
		}
	}
	for _, ext := range extOrder {
		if ids := extClaims[ext]; len(ids) == 1 {
			m.byExt[ext] = ids[0]
		} else {
			m.collisions = append(m.collisions, collision{kind: "ext_address", value: hex16(ext), nodes: ids})
		}
	}
	for r, ids := range anyClaims {
		if len(ids) == 1 {
			m.rlocAny[r] = ids[0]
		}
	}
	for _, k := range rlocOrder {
		if ids := rlocClaims[k]; len(ids) == 1 {
			m.byRloc[k] = ids[0]
		} else {
			value := strconv.FormatUint(uint64(k.rloc), 10)
			if k.network {
				value = hex16(k.xpan) + ":" + value
			}
			m.collisions = append(m.collisions, collision{kind: "rloc16", value: value, nodes: ids})
		}
	}
	return m
}

// appendClaim adds id to ids unless it is there.
func appendClaim(ids []string, id string) []string {
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}

// node is the commissioned node a neighbour entry of an observer on the
// network of xpan names: by extended address, else by a non-zero RLOC16
// claimed within that network, else by one claimed by a node without an
// extended PAN id; an observer without one (xpan nil) matches an RLOC16
// only one node claims anywhere.
func (m threadMaps) node(nb *threaddiagdef.NeighborTableStruct, xpan *uint64) (string, bool) {
	if id, ok := m.byExt[nb.ExtAddress]; ok {
		return id, true
	}
	if nb.Rloc16 == 0 {
		return "", false
	}
	if xpan != nil {
		if id, ok := m.byRloc[rlocKey{xpan: *xpan, network: true, rloc: nb.Rloc16}]; ok {
			return id, true
		}
	}
	if id, ok := m.byRloc[rlocKey{rloc: nb.Rloc16}]; ok {
		return id, true
	}
	if xpan == nil {
		id, ok := m.rlocAny[nb.Rloc16]
		return id, ok
	}
	return "", false
}

// external is a Thread device commissioned nodes list as a neighbour and
// no commissioned node matches.
type external struct {
	id          string
	ext         uint64
	extPanID    uint64 // the first observer's that knows one
	router      bool   // some observer saw it rx-on-when-idle
	networkName string // the registry's for extPanID
	br          *brdiscovery.Entry
}

// unknowns are the neighbours no commissioned node matches, one per
// extended address in the order first seen — matterjs-server's
// findUnknownDevices at 0bd06f22 (topology-utils.ts:539-633): a neighbour
// the registry knows is its border router, id br_<EXTADDR> (:603-615);
// any other is unknown_<EXTADDR>, named by the registry's network for its
// extended PAN id (:592-600, :616-629); a neighbour seen rx-on-when-idle
// by any observer is a router, the extended PAN id is the first
// observer's. The best RSSI and the observers it also keeps do not reach
// the wire.
func (m threadMaps) unknowns(nodes []*Image, brs borderRouterIndex) []*external {
	var out []*external
	byExt := map[uint64]*external{}
	for _, img := range nodes {
		var xp uint64
		if p := img.ThreadNetworkDiagnostics.ExtendedPanId; p != nil {
			xp = *p
		}
		for i := range img.ThreadNetworkDiagnostics.NeighborTable {
			nb := &img.ThreadNetworkDiagnostics.NeighborTable[i]
			if _, ok := m.node(nb, img.ThreadNetworkDiagnostics.ExtendedPanId); ok {
				continue
			}
			e := byExt[nb.ExtAddress]
			if e == nil {
				e = &external{id: "unknown_" + hex16(nb.ExtAddress), ext: nb.ExtAddress, extPanID: xp}
				byExt[nb.ExtAddress] = e
				out = append(out, e)
			} else if e.extPanID == 0 {
				e.extPanID = xp
			}
			e.router = e.router || nb.RxOnWhenIdle
		}
	}
	for _, e := range out {
		if br, ok := brs.byExt[e.ext]; ok {
			e.br, e.id = br, "br_"+hex16(e.ext)
		} else if e.extPanID != 0 {
			e.networkName = brs.networkByXpan[e.extPanID]
		}
	}
	return out
}

// pair is two nodes and the link each observed to the other; a is the
// smaller id by string order.
type pair struct {
	a, b   string
	ab, ba *Direction
}

// pairs folds every neighbour entry into pairs, in the order first seen:
// the target by extended address, by a non-zero RLOC16, else the
// external; no self-links; per direction the first entry wins —
// matterjs-server's buildThreadEdgePairs at 0bd06f22
// (topology-utils.ts:843-908, the pair key makePairKey at :816-818) without
// the route-table supplement. A direction's RSSI is the entry's average,
// else its last.
func (m threadMaps) pairs(nodes []*Image, externals []*external) []*pair {
	unknown := make(map[uint64]string, len(externals))
	for _, e := range externals {
		unknown[e.ext] = e.id
	}
	var out []*pair
	byKey := map[string]*pair{}
	for _, img := range nodes {
		from := strconv.FormatUint(img.NodeID, 10)
		for i := range img.ThreadNetworkDiagnostics.NeighborTable {
			nb := &img.ThreadNetworkDiagnostics.NeighborTable[i]
			to, ok := m.node(nb, img.ThreadNetworkDiagnostics.ExtendedPanId)
			if !ok {
				to, ok = unknown[nb.ExtAddress]
			}
			if !ok || to == from {
				continue
			}
			a, b := from, to
			if b < a {
				a, b = b, a
			}
			key := a + "|" + b
			p := byKey[key]
			if p == nil {
				p = &pair{a: a, b: b}
				byKey[key] = p
				out = append(out, p)
			}
			d := &Direction{Strength: lqiStrength(nb.Lqi), LQI: nb.Lqi, RSSI: neighbourRSSI(nb)}
			switch {
			case from == p.a && p.ab == nil:
				p.ab = d
			case from == p.b && p.ba == nil:
				p.ba = d
			}
		}
	}
	return out
}

// neighbourRSSI is an entry's average RSSI, else its last, nil for none.
func neighbourRSSI(nb *threaddiagdef.NeighborTableStruct) *int8 {
	switch {
	case !nb.AverageRssi.Null:
		v := nb.AverageRssi.Value
		return &v
	case !nb.LastRssi.Null:
		v := nb.LastRssi.Value
		return &v
	}
	return nil
}

// connectionOf folds a pair into one connection: dropped when no
// direction was observed alive, its strength the strongest direction —
// matterjs-server's mapThreadPair and strongestLevel at 0bd06f22
// (NetworkTopologyService.ts:486-501, :509-511).
func connectionOf(p *pair) (Connection, bool) {
	best := StrengthNone
	alive := false
	for _, d := range []*Direction{p.ab, p.ba} {
		if d == nil {
			continue
		}
		if d.Strength != StrengthNone {
			alive = true
		}
		if levelRank[d.Strength] > levelRank[best] {
			best = d.Strength
		}
	}
	if !alive {
		return Connection{}, false
	}
	return Connection{Source: p.a, Target: p.b, Network: NetworkThread, Strength: best, SourceToTarget: p.ab, TargetToSource: p.ba}, true
}
