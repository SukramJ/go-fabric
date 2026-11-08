// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"strconv"

	"github.com/SukramJ/go-fabric/thread/brdiscovery"

	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
)

// threadMaps resolve a neighbour-table entry to a commissioned node: by
// extended address, and by RLOC16 as the fallback — matterjs-server's
// buildExtAddrMap and buildRloc16Map at 0bd06f22
// (topology-utils.ts:385-398, :405-418). A node without a known extended
// address or RLOC16 is absent from that map; on a collision the later
// node wins, as Map.set does there.
type threadMaps struct {
	byExt  map[uint64]string
	byRloc map[uint16]string
}

func newThreadMaps(nodes []*Image) threadMaps {
	m := threadMaps{byExt: map[uint64]string{}, byRloc: map[uint16]string{}}
	for _, img := range nodes {
		id := strconv.FormatUint(img.NodeID, 10)
		if ext := threadExtAddress(img.GeneralDiagnostics.NetworkInterfaces); ext != 0 {
			m.byExt[ext] = id
		}
		if r := img.ThreadNetworkDiagnostics.Rloc16; r != nil {
			m.byRloc[*r] = id
		}
	}
	return m
}

// node is the commissioned node a neighbour entry names, by extended
// address, else by a non-zero RLOC16.
func (m threadMaps) node(nb *threaddiagdef.NeighborTableStruct) (string, bool) {
	if id, ok := m.byExt[nb.ExtAddress]; ok {
		return id, true
	}
	if nb.Rloc16 != 0 {
		if id, ok := m.byRloc[nb.Rloc16]; ok {
			return id, true
		}
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
			if _, ok := m.node(nb); ok {
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
			to, ok := m.node(nb)
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
