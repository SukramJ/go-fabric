// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// FuzzGroupcastFieldsReader feeds arbitrary command fields to the Groupcast
// decoders, which a controller reaches with any payload it likes: they must
// neither panic nor accept a payload without its mandatory fields. Seeded
// with the payloads matter.js encoded.
func FuzzGroupcastFieldsReader(f *testing.F) {
	var fixtures []groupcastWireFixture
	if err := json.Unmarshal(groupcastWireFixturesJSON, &fixtures); err != nil {
		f.Fatal(err)
	}
	commands := map[string]uint8{"JoinGroup": 0, "LeaveGroup": 1, "UpdateGroupKey": 3, "ConfigureAuxiliaryAcl": 4, "GroupcastTesting": 5}
	for _, fx := range fixtures {
		if cmd, ok := commands[fx.Element]; ok && fx.Kind == "commands" {
			b, err := hex.DecodeString(fx.BytesHex)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(cmd, b)
		}
	}
	f.Fuzz(func(t *testing.T, cmd uint8, payload []byte) {
		dec := tlv.NewDecoder(payload)
		open, err := dec.Next()
		if err != nil || !open.IsContainer || open.Type != tlv.TypeStructure {
			return
		}
		fields, err := commandFieldsReader(im.ConcreteCommandPath{Cluster: mattercore.GroupcastClusterID, Command: uint32(cmd % 6)}, dec, open)
		if err != nil {
			return
		}
		switch req := fields.(type) {
		case mattercore.JoinGroupRequest:
			if req.Endpoints == nil {
				t.Fatalf("JoinGroup accepted without Endpoints: %x", payload)
			}
		case mattercore.LeaveGroupRequest:
			if req.HasEndpoints && req.Endpoints == nil {
				t.Fatalf("LeaveGroup reported Endpoints it did not decode: %x", payload)
			}
		}
	})
}
