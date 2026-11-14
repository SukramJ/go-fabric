// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	stdmaps "maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// openClientStore is the SQLite client store on a temporary file, opened
// as sqlstore's own tests open it.
func openClientStore(t *testing.T) *sqlstore.Store {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "client.db"), "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlstore.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return sqlstore.New(db)
}

// storeRows is every row st holds for node "n": "ep/cluster/attr" to the
// value's bytes, "ep/cluster/version" to the version.
func storeRows(t *testing.T, st client.Store) map[string]string {
	t.Helper()
	eps, err := st.Load("n")
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, e := range eps {
		for _, c := range e.Clusters {
			for a, v := range c.Values {
				rows[fmt.Sprintf("%d/%#x/%#x", e.Number, c.ID, a)] = hex.EncodeToString(v)
			}
			if c.Version != nil {
				rows[fmt.Sprintf("%d/%#x/version", e.Number, c.ID)] = strconv.FormatUint(uint64(*c.Version), 10)
			}
		}
	}
	return rows
}

// loadQuota loads node "n" from st with the image quota quota.
func loadQuota(t *testing.T, st client.Store, quota int) (*client.Node, *warnLog) {
	t.Helper()
	logs := &warnLog{}
	n, err := client.NewWithImageQuota(client.Config{ID: "n", Store: st, Interaction: interaction.Bind(nil, interaction.Config{}), Logger: slog.New(logs)}, quota)
	if err != nil {
		t.Fatal(err)
	}
	return n, logs
}

// blob is an octet string of size bytes.
func blob(size int) spec.Node {
	return spec.Node{El: tlv.Element{Type: tlv.TypeOctetStr2, Octets: make([]byte, size)}}
}

// TestImageCutAtLoadErasesOnlyWhatItDrops: an image stored above the
// node's quota loads cut back to what its structure names, and the store
// loses exactly the dropped endpoints' rows — the record, the settings,
// the ICD entry and every kept endpoint's values stay stored, and a
// restart serves them again.
func TestImageCutAtLoadErasesOnlyWhatItDrops(t *testing.T) {
	t.Parallel()
	const unnamed, vendor = 1000, 0xFFF1_FC00
	for _, tc := range []struct {
		name      string
		structure bool // the image holds a bridge's structure beside the unnamed endpoint
	}{
		{"a bridge and an endpoint no PartsList names", true},
		{"an image with no structure yet", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := openClientStore(t)
			n, _ := loadQuota(t, st, 0)
			client.UpdateRecord(n, func(r *client.Record) {
				r.PeerAddress = &client.PeerAddress{FabricIndex: 1, NodeID: 0x1234}
			})
			if tc.structure {
				if err := client.SaveSettingsAutoSubscribe(n); err != nil {
					t.Fatal(err)
				}
				if err := st.Save("n", 0, client.ICDClusterID, map[uint32][]byte{1: {0x01}}, nil); err != nil {
					t.Fatal(err)
				}
				client.ApplyBridge(n, 1)
			}
			client.ApplyAsked(n, unnamed, vendor, blob(4096))
			before := storeRows(t, st)
			want := stdmaps.Clone(before)
			stdmaps.DeleteFunc(want, func(k, _ string) bool { return strings.HasPrefix(k, "1000/") })
			if len(want) == len(before) {
				t.Fatalf("the store holds no row of endpoint %d before the reload", unnamed)
			}

			// A quota of one byte: every image is above it.
			re, logs := loadQuota(t, st, 1)
			if logs.count("matter.controller.client.image_cut_at_load") != 1 {
				t.Fatal("the cut at load was not logged")
			}
			if re.Endpoint(unnamed) != nil {
				t.Errorf("endpoint %d, which no PartsList names, survived the cut at load", unnamed)
			}
			if got := storeRows(t, st); !stdmaps.Equal(got, want) {
				t.Errorf("after the cut at load the store holds %d rows, want %d (only endpoint %d's erased): missing %v",
					len(got), len(want), unnamed, missing(want, got))
			}

			restarted, _ := loadQuota(t, st, 1)
			if !restarted.IsCommissioned() {
				t.Error("the node's record did not survive the cut at load")
			}
			if tc.structure {
				ep := restarted.Endpoint(2)
				if ep == nil || ep.Cluster(onoff.ClusterID) == nil {
					t.Fatal("a restart after the cut at load does not serve the plug's OnOff")
				}
				if _, ok := ep.Cluster(onoff.ClusterID).Value(0); !ok {
					t.Error("a restart after the cut at load does not serve the plug's OnOff value")
				}
			}
			if got := storeRows(t, st); !stdmaps.Equal(got, want) {
				t.Errorf("a second load above the quota changed the store: missing %v", missing(want, got))
			}
		})
	}
}

// missing is the keys of want that got lacks or holds differently, sorted.
func missing(want, got map[string]string) []string {
	var out []string
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
