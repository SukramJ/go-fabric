// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// snapshotRequestAccess is one request command of parity/schema.json with
// the effective access the invoke gates read: the fabric quality and the
// invoke (write) privilege.
type snapshotRequestAccess struct {
	cluster, command uint32
	clusterName      string
	name             string
	fabric           string
	writePriv        string
}

// snapshotRequestCommands reads every command of the snapshot whose
// effective direction is "request" — the command's own direction when its
// effective layer states none — with its effective access.
func snapshotRequestCommands(t *testing.T) []snapshotRequestAccess {
	t.Helper()
	var snap struct {
		Clusters []struct {
			ID       uint32 `json:"id"`
			Name     string `json:"name"`
			Commands []struct {
				ID        uint32 `json:"id"`
				Name      string `json:"name"`
				Direction string `json:"direction"`
				Effective *struct {
					Direction string `json:"direction"`
					Access    struct {
						Fabric    string `json:"fabric"`
						WritePriv string `json:"writePriv"`
					} `json:"access"`
				} `json:"effective"`
			} `json:"commands"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	var out []snapshotRequestAccess
	for _, c := range snap.Clusters {
		for _, m := range c.Commands {
			dir := m.Direction
			if m.Effective != nil && m.Effective.Direction != "" {
				dir = m.Effective.Direction
			}
			if dir != "request" {
				continue
			}
			if m.Effective == nil {
				// The generator refuses it too: a request without an
				// effective access has none to hold the tables to.
				t.Fatalf("%s.%s: request command without an effective block", c.Name, m.Name)
			}
			out = append(out, snapshotRequestAccess{
				cluster: c.ID, command: m.ID, clusterName: c.Name, name: m.Name,
				fabric: m.Effective.Access.Fabric, writePriv: m.Effective.Access.WritePriv,
			})
		}
	}
	if len(out) == 0 {
		t.Fatal("snapshot carries no request command")
	}
	return out
}

// TestFabricScopedInvokeMatchesSnapshot holds IsFabricScopedInvoke to the
// model it is generated from: true for exactly the request commands whose
// effective access in parity/schema.json is fabric-scoped ("F"), across
// every cluster of the snapshot, and false for every other command id of a
// cluster. Before, the table was hand-written for ten clusters and missed
// 28 "F" commands (review II, R2-P6-acl-1301): TlsCertificateManagement's
// ProvisionRootCertificate ran on a PASE session and stored its root under
// fabric 0.
func TestFabricScopedInvokeMatchesSnapshot(t *testing.T) {
	t.Parallel()
	want := map[uint32]map[uint32]string{}
	clusters := map[uint32]string{}
	for _, r := range snapshotRequestCommands(t) {
		clusters[r.cluster] = r.clusterName
		if r.fabric == "" {
			continue
		}
		if r.fabric != "F" {
			t.Fatalf("%s.%s: command fabric quality %q, want \"F\" or none", r.clusterName, r.name, r.fabric)
		}
		if want[r.cluster] == nil {
			want[r.cluster] = map[uint32]string{}
		}
		want[r.cluster][r.command] = r.name
	}
	total := 0
	for cluster, name := range clusters {
		total += len(want[cluster])
		for id := range uint32(0x100) {
			cmd, scoped := want[cluster][id]
			if got := schema.IsFabricScopedInvoke(cluster, id); got != scoped {
				t.Errorf("%s (0x%04X) command 0x%02X %s: IsFabricScopedInvoke = %v, snapshot fabric-scoped = %v", name, cluster, id, cmd, got, scoped)
			}
		}
	}
	if total == 0 {
		t.Fatal("snapshot carries no fabric-scoped request command: the extractor lost access.fabric")
	}
}

// TestInvokePrivilegeMatchesSnapshot holds InvokePrivilege to the model:
// each request command's effective writePriv in parity/schema.json — "A"
// Administer, "M" Manage, "O" the Operate default — across every cluster of
// the snapshot. Before, the table was hand-written for 21 clusters and held
// only against a sibling matter.js checkout, so 83 Manage or Administer
// commands answered Operate (review II, R2-P6-acl-1302).
func TestInvokePrivilegeMatchesSnapshot(t *testing.T) {
	t.Parallel()
	levels := map[string]uint8{"O": schema.PrivilegeOperate, "M": schema.PrivilegeManage, "A": schema.PrivilegeAdminister}
	elevated := 0
	for _, r := range snapshotRequestCommands(t) {
		want, ok := levels[r.writePriv]
		if !ok {
			t.Errorf("%s.%s: writePriv %q is not one this test knows", r.clusterName, r.name, r.writePriv)
			continue
		}
		if want > schema.PrivilegeOperate {
			elevated++
		}
		if got := schema.InvokePrivilege(r.cluster, r.command); got != want {
			t.Errorf("%s (0x%04X) command 0x%02X %s: InvokePrivilege = %d, snapshot writePriv %q = %d", r.clusterName, r.cluster, r.command, r.name, got, r.writePriv, want)
		}
	}
	if elevated == 0 {
		t.Fatal("snapshot carries no Manage or Administer request command: the extractor lost access.writePriv")
	}
}
