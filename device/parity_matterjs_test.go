// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// lightParity is what a matter.js light device file states, transcribed
// line by line.
type lightParity struct {
	ty             *Type
	id, revision   uint16
	mandatory      []string // server.mandatory, in order
	optional       []string // server.optional, in order
	clientOptional []string // client.optional
	defaults       []string // SupportedBehaviors(...), in order
	features       map[string][]string
}

// levelControlAlteration is LevelControlServer's .alter(...) in both
// files: currentLevel {min 1, max 254}, minLevel {min 1, max 1, default 1},
// maxLevel {min 254, max 254, default 254}.
var levelControlAlteration = []ElementRequirement{
	{Element: schema.RequirementAttribute, Name: "CurrentLevel", ID: 0x0000, HasMin: true, Min: 1, HasMax: true, Max: 254},
	{Element: schema.RequirementAttribute, Name: "MinLevel", ID: 0x0002, HasDefault: true, Default: 1, HasMin: true, Min: 1, HasMax: true, Max: 1},
	{Element: schema.RequirementAttribute, Name: "MaxLevel", ID: 0x0003, HasDefault: true, Default: 254, HasMin: true, Min: 254, HasMax: true, Max: 254},
}

// TestDeviceRequirementsParityMatterJS holds the generated DimmableLight and
// OnOffLight to matter.js 0ceb202d:
//
//   - packages/node/src/devices/dimmable-light.ts:38 IdentifyServer
//     .alter({commands:{triggerEffect:{optional:false}}}); :52
//     OnOffServer.with("Lighting"); :59-67 LevelControlServer
//     .with("Lighting","OnOff").alter({attributes:{currentLevel:{min:1,max:254},
//     minLevel:{min:1,max:1,default:1}, maxLevel:{min:254,max:254,default:254}}});
//     :74-75 ScenesManagementServer.alter({commands:{copyScene:{optional:false}}});
//     :87-95 server.mandatory Identify, Groups, OnOff, LevelControl,
//     ScenesManagement; :100 client.optional OccupancySensing; :103-116
//     MutableEndpoint({name:"DimmableLight", deviceType:0x101,
//     deviceRevision:4, behaviors: the five mandatory servers}).
//   - packages/node/src/devices/on-off-light.ts:83-91 LevelControlServer
//     .with("OnOff","Lighting") with the same alteration; :103-112
//     server.mandatory Identify, Groups, OnOff, ScenesManagement and
//     server.optional LevelControl; :117 client.optional OccupancySensing;
//     :120-132 MutableEndpoint({name:"OnOffLight", deviceType:0x100,
//     deviceRevision:4, behaviors: the four mandatory servers}) — the
//     optional LevelControl is not a default behavior.
func TestDeviceRequirementsParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, want := range []lightParity{
		{
			ty: &DimmableLightType, id: 0x101, revision: 4,
			mandatory:      []string{"Identify", "Groups", "OnOff", "LevelControl", "ScenesManagement"},
			clientOptional: []string{"OccupancySensing"},
			defaults:       []string{"Identify", "Groups", "OnOff", "LevelControl", "ScenesManagement"},
			features:       map[string][]string{"OnOff": {"LT"}, "LevelControl": {"LT", "OO"}},
		},
		{
			ty: &OnOffLightType, id: 0x100, revision: 4,
			mandatory:      []string{"Identify", "Groups", "OnOff", "ScenesManagement"},
			optional:       []string{"LevelControl"},
			clientOptional: []string{"OccupancySensing"},
			defaults:       []string{"Identify", "Groups", "OnOff", "ScenesManagement"},
			features:       map[string][]string{"OnOff": {"LT"}, "LevelControl": {"OO", "LT"}},
		},
	} {
		ty := want.ty
		if ty.ID != want.id || ty.Revision != want.revision {
			t.Errorf("%s: 0x%04X rev %d, want 0x%04X rev %d", ty.Name, ty.ID, ty.Revision, want.id, want.revision)
		}
		var mand, opt []string
		for _, r := range ty.Servers {
			if r.Mandatory {
				mand = append(mand, r.Name)
			} else {
				opt = append(opt, r.Name)
			}
			if fs, ok := want.features[r.Name]; ok {
				got := slices.Sorted(slices.Values(r.FeatureNames))
				if !slices.Equal(got, slices.Sorted(slices.Values(fs))) {
					t.Errorf("%s %s features %v, want %v", ty.Name, r.Name, r.FeatureNames, fs)
				}
			} else if len(r.FeatureNames) > 0 {
				t.Errorf("%s %s mandates features %v matter.js does not", ty.Name, r.Name, r.FeatureNames)
			}
			switch r.Name {
			case "LevelControl":
				if !slices.Equal(r.Elements, levelControlAlteration) {
					t.Errorf("%s LevelControl alteration %+v", ty.Name, r.Elements)
				}
			case "Identify":
				if e, ok := r.Element(schema.RequirementCommand, 0x40); !ok || e.Presence != PresenceMandatory {
					t.Errorf("%s Identify triggerEffect not optional:false", ty.Name)
				}
			case "ScenesManagement":
				if e, ok := r.Element(schema.RequirementCommand, 0x40); !ok || e.Presence != PresenceMandatory {
					t.Errorf("%s ScenesManagement copyScene not optional:false", ty.Name)
				}
			default:
				if len(r.Elements) > 0 {
					t.Errorf("%s %s alters %+v", ty.Name, r.Name, r.Elements)
				}
			}
		}
		if !slices.Equal(mand, want.mandatory) || !slices.Equal(opt, want.optional) {
			t.Errorf("%s servers mandatory %v optional %v", ty.Name, mand, opt)
		}
		var clients []string
		for _, r := range ty.Clients {
			if r.Mandatory {
				t.Errorf("%s client %s mandatory", ty.Name, r.Name)
			}
			clients = append(clients, r.Name)
		}
		if !slices.Equal(clients, want.clientOptional) {
			t.Errorf("%s clients %v", ty.Name, clients)
		}
		// The default behaviors: what a device of the type mounts, the
		// assembler's Identify, Groups and ScenesManagement included.
		d, err := New(ty, testOptions(ty.Name))
		if err != nil {
			t.Fatal(err)
		}
		mounted := map[string]bool{}
		for _, srv := range d.Servers() {
			r, _ := ty.Server(srv.MatterClusterID())
			mounted[r.Name] = true
		}
		for _, name := range want.defaults {
			if _, byAssembler := assemblerMountedByName(name); !byAssembler && !mounted[name] {
				t.Errorf("%s: default behavior %s not mounted", ty.Name, name)
			}
		}
		for _, name := range want.optional {
			if mounted[name] {
				t.Errorf("%s: optional %s mounted by default", ty.Name, name)
			}
		}
	}
}

// assemblerMountedByName reports whether the assembler mounts the cluster
// named name.
func assemblerMountedByName(name string) (uint32, bool) {
	for id, n := range assemblerMounted {
		if n == name {
			return id, true
		}
	}
	return 0, false
}
