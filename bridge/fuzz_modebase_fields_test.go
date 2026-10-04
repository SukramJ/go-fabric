// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"encoding/hex"
	"testing"

	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// FuzzChangeToModeFieldsReader feeds arbitrary command fields to the
// ModeBase ChangeToMode decoder, which a controller reaches with any
// payload: it must not panic, and it must not accept a payload without
// the mandatory NewMode. Seeded with the payloads matter.js encoded.
func FuzzChangeToModeFieldsReader(f *testing.F) {
	for _, fx := range loadApplianceFixturesForFuzz(f) {
		if fx.Kind == "commands" && fx.Element == "ChangeToMode" {
			b, err := hex.DecodeString(fx.BytesHex)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(b)
		}
	}
	path := im.ConcreteCommandPath{Cluster: clusterwire.RvcRunModeClusterID, Command: clusterwire.ModeBaseCmdChangeToMode}
	f.Fuzz(func(t *testing.T, payload []byte) {
		dec := tlv.NewDecoder(payload)
		open, err := dec.Next()
		if err != nil || !open.IsContainer || open.Type != tlv.TypeStructure {
			return
		}
		if _, err := commandFieldsReader(path, dec, open); err != nil {
			return
		}
		generic := tlv.NewDecoder(payload)
		_, _ = generic.Next()
		fields, err := decodeGenericTagMap(generic)
		if err != nil {
			return // the generic salvage is stricter about nesting; nothing to compare
		}
		if _, ok := fields[clusterwire.ChangeToModeFieldNewMode]; !ok {
			t.Fatalf("ChangeToMode accepted without NewMode: %x", payload)
		}
	})
}
