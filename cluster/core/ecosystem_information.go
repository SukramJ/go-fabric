// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"cmp"
	"context"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The bounds connectedhomeip holds an EcosystemInformation entry to
// before it accepts it
// (src/app/clusters/ecosystem-information-server/
// ecosystem-information-server.cpp:32-35 at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20).
const (
	ecoDeviceNameMaxSize             = 64  // kDeviceNameMaxSize
	ecoUniqueLocationIDMaxSize       = 64  // kUniqueLocationIdMaxSize
	ecoUniqueLocationIDsListMaxSize  = 64  // kUniqueLocationIdsListMaxSize
	ecoLocationDescriptorNameMaxSize = 128 // kLocationDescriptorNameMaxSize
	ecoMinFabricIndex                = 1   // kMinValidFabricIndex
	ecoMaxFabricIndex                = 254 // kMaxValidFabricIndex
)

// EcosystemDirectory is the host port of an EcosystemInformation server:
// the devices and locations the ecosystem behind this bridged endpoint
// knows, each entry carrying the FabricIndex of the fabric it belongs to.
// Both are asked on every read. The server filters them by the reading
// fabric; the host returns every fabric's entries.
type EcosystemDirectory interface {
	// EcosystemDevices is the DeviceDirectory, in the order the host
	// added the devices (connectedhomeip keeps them in a vector,
	// ecosystem-information-server.h:248).
	EcosystemDevices(ctx context.Context) []ecodef.EcosystemDeviceStruct
	// EcosystemLocations is the LocationDirectory, in any order.
	EcosystemLocations(ctx context.Context) []ecodef.EcosystemLocationStruct
}

// EcosystemInformation is an EcosystemInformation (0x0750) server for one
// bridged endpoint.
//
// Mirrors matter.js packages/node/src/behaviors/ecosystem-information/
// EcosystemInformationServer.ts:14, `export class
// EcosystemInformationServer extends EcosystemInformationBehavior {}`: no
// server logic. The rules are connectedhomeip's at the harness pin:
//
//   - An entry its builders refuse never reaches a directory
//     (EcosystemDeviceStruct::Builder::Build :135-157,
//     EcosystemLocationStruct::Builder::Build :215-229, AddLocationInfo
//     :271-288); this server leaves such a host entry out of the read.
//   - A second location with the same UniqueLocationId and FabricIndex is
//     refused (:282-284): the first one stays. Locations read in the
//     order of that key (std::map over EcosystemLocationKey,
//     ecosystem-information-server.h:235-249).
//   - DeviceNameLastEdit is sent only with a DeviceName (Encode :162-167).
//   - Both attributes are fabric-scoped lists: a fabric-filtered read
//     sees the reading fabric's entries only, an unfiltered one every
//     fabric's, another fabric's with its fabric-sensitive fields (all of
//     them) left out — connectedhomeip's AttributeValueEncoder
//     ListEncodeHelper::Encode (src/app/AttributeValueEncoder.h:96-107),
//     which skips a foreign entry on a filtered read and encodes it for
//     the accessing fabric otherwise.
//
// Reading needs Manage ("R F M"), which the embedded definition instance
// answers through [contract.ClusterAttributeReadPrivilege].
type EcosystemInformation struct {
	*spec.Instance
	cluster.AttributeChanges

	dv  cluster.DataVersionTracker
	dir EcosystemDirectory
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*EcosystemInformation)(nil)
	_ contract.ClusterDataVersion            = (*EcosystemInformation)(nil)
	_ contract.ClusterAttributeLister        = (*EcosystemInformation)(nil)
	_ contract.ClusterAttributeReadPrivilege = (*EcosystemInformation)(nil)
	_ contract.FabricScopedReader            = (*EcosystemInformation)(nil)
	_ contract.AttributeChangeNotifier       = (*EcosystemInformation)(nil)
)

// ecoInst is the definition bound: no features, both attributes
// mandatory, no commands or events.
var ecoInst = func() *spec.Instance {
	inst, err := spec.New(ecodef.Definition, spec.Options{})
	if err != nil {
		panic("core: EcosystemInformation: " + err.Error())
	}
	return inst
}()

// NewEcosystemInformation builds the server over dir.
func NewEcosystemInformation(dir EcosystemDirectory) *EcosystemInformation {
	return &EcosystemInformation{Instance: ecoInst, dir: dir}
}

// DirectoryChanged tells the server the host's directory moved: both
// attributes change, as connectedhomeip marks the attribute dirty on
// every AddDeviceInfo / AddLocationInfo (:267, :286).
func (e *EcosystemInformation) DirectoryChanged() {
	e.dv.Bump()
	e.Notify(ecodef.AttrDeviceDirectory, ecodef.AttrLocationDirectory)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (e *EcosystemInformation) MatterDataVersion() uint32 { return e.dv.Current() }

// MatterRead answers a read with no Interaction Model request behind it:
// every fabric's entries in full.
func (e *EcosystemInformation) MatterRead(attrID uint32) (any, bool) {
	return e.read(context.Background(), attrID, false, 0, false)
}

// MatterReadFiltered implements [contract.FabricScopedReader].
func (e *EcosystemInformation) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	filtered, fabric, request := im.LookupFabricFilter(ctx)
	if !request {
		return e.MatterRead(attrID) //nolint:contextcheck // MatterRead is the cluster-interface read without a request; it takes no ctx by contract
	}
	return e.read(ctx, attrID, filtered, fabric, true)
}

// read projects the directory for one reader. scoped is false for a local
// read, which sees everything; otherwise filtered and fabric say whose
// entries are listed and whose are disclosed.
func (e *EcosystemInformation) read(ctx context.Context, attrID uint32, filtered bool, fabric uint8, scoped bool) (any, bool) {
	switch attrID {
	case ecodef.AttrDeviceDirectory:
		out := spec.List[spec.Encodable]{}
		for _, d := range e.dir.EcosystemDevices(ctx) {
			if !validEcosystemDevice(d) {
				continue
			}
			d = normalizeEcosystemDevice(d)
			if v, ok := scopeEntry(d, d.FabricIndex, filtered, fabric, scoped); ok {
				out = append(out, v)
			}
		}
		return out, true
	case ecodef.AttrLocationDirectory:
		out := spec.List[spec.Encodable]{}
		for _, l := range ecosystemLocations(e.dir.EcosystemLocations(ctx)) {
			if v, ok := scopeEntry(l, l.FabricIndex, filtered, fabric, scoped); ok {
				out = append(out, v)
			}
		}
		return out, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return e.ReadGlobal(attrID)
	}
	return nil, false
}

// scopeEntry decides how one entry owned by owner reads: as it is, as its
// FabricIndex alone, or not at all (AttributeValueEncoder.h:96-107).
func scopeEntry(v spec.Encodable, owner uint8, filtered bool, fabric uint8, scoped bool) (spec.Encodable, bool) {
	switch {
	case !scoped, owner == fabric && fabric != 0:
		return v, true
	case filtered:
		return nil, false
	default:
		return redactedEntry{fabricIndex: owner}, true
	}
}

// redactedEntry is another fabric's entry on an unfiltered read: every
// field of both entry structs but FabricIndex is fabric-sensitive
// (ecosystem-information.element.ts, "S"), so only FabricIndex is sent.
type redactedEntry struct{ fabricIndex uint8 }

// EncodeTLV implements [spec.Encodable].
func (r redactedEntry) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	spec.PutUint[uint8](enc, tlv.ContextTag(254), r.fabricIndex)
	_ = enc.EndContainer()
}

// validEcosystemDevice holds a device entry to
// EcosystemDeviceStruct::Builder::Build (:135-150).
func validEcosystemDevice(d ecodef.EcosystemDeviceStruct) bool {
	if d.DeviceName != nil && len(*d.DeviceName) > ecoDeviceNameMaxSize { // :138
		return false
	}
	if d.OriginalEndpoint == nil || *d.OriginalEndpoint == 0xFFFF { // :139, kInvalidEndpointId
		return false
	}
	if len(d.DeviceTypes) == 0 { // :140
		return false
	}
	if len(d.UniqueLocationIds) > ecoUniqueLocationIDsListMaxSize { // :141-142
		return false
	}
	if !validFabricIndex(d.FabricIndex) { // :143-144
		return false
	}
	for _, id := range d.UniqueLocationIds { // :146-149
		if len(id) > ecoUniqueLocationIDMaxSize {
			return false
		}
	}
	return true
}

// normalizeEcosystemDevice applies Encode's name rule (:162-167): a
// DeviceName goes out only when it is not empty, and with it its
// DeviceNameLastEdit, which the builder defaults to 0
// (ecosystem-information-server.h:88); without a name neither is sent.
func normalizeEcosystemDevice(d ecodef.EcosystemDeviceStruct) ecodef.EcosystemDeviceStruct {
	if d.DeviceName == nil || *d.DeviceName == "" {
		d.DeviceName, d.DeviceNameLastEdit = nil, nil
		return d
	}
	if d.DeviceNameLastEdit == nil {
		zero := uint64(0)
		d.DeviceNameLastEdit = &zero
	}
	return d
}

// ecosystemLocations keeps the locations connectedhomeip would hold — a
// valid entry (EcosystemLocationStruct::Builder::Build :215-222,
// AddLocationInfo :276-279), the first of each (UniqueLocationId,
// FabricIndex) key (:282-284) — ordered by that key.
func ecosystemLocations(in []ecodef.EcosystemLocationStruct) []ecodef.EcosystemLocationStruct {
	out := make([]ecodef.EcosystemLocationStruct, 0, len(in))
	for _, l := range in {
		if l.UniqueLocationId == "" || len(l.UniqueLocationId) > ecoUniqueLocationIDMaxSize ||
			!validFabricIndex(l.FabricIndex) ||
			l.LocationDescriptor.LocationName == "" || len(l.LocationDescriptor.LocationName) > ecoLocationDescriptorNameMaxSize {
			continue
		}
		if slices.ContainsFunc(out, func(o ecodef.EcosystemLocationStruct) bool {
			return o.UniqueLocationId == l.UniqueLocationId && o.FabricIndex == l.FabricIndex
		}) {
			continue
		}
		out = append(out, l)
	}
	slices.SortStableFunc(out, func(a, b ecodef.EcosystemLocationStruct) int {
		return cmp.Or(cmp.Compare(a.UniqueLocationId, b.UniqueLocationId), cmp.Compare(a.FabricIndex, b.FabricIndex))
	})
	return out
}

func validFabricIndex(f uint8) bool { return f >= ecoMinFabricIndex && f <= ecoMaxFabricIndex }

// MatterWrite refuses: both attributes are read-only (access "R F M").
func (e *EcosystemInformation) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := e.ValidateWrite(attrID, value, nil)
	return err
}

// MatterInvoke answers UNSUPPORTED_COMMAND: the cluster has no commands.
func (e *EcosystemInformation) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, im.UnsupportedCommandf("EcosystemInformation: command 0x%02X is not supported", cmdID)
}
