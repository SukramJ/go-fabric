// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The fuzz target of the TLS certificate tables: connectedhomeip's three
// FUZZ_TEST of FuzzTLSCertificateManagementPW.cpp at 6170af84
// (RootUpsertDoesNotCrash :359, ClientUpdateDoesNotCrash :461,
// ClientIntermediateCertsDoNotCrash :542) drive the storage engine's
// upserts with arbitrary ids, fabrics and certificate bytes; here one
// target drives the same upserts through the commands and checks the
// tables' invariants after each.
package tls_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tcmdef "github.com/SukramJ/go-fabric/cluster/spec/tlscertificatemanagement"
	"github.com/SukramJ/go-fabric/cluster/tls"
)

func FuzzCertificateUpserts(f *testing.F) {
	f.Add([]byte{0, 1, 5, 'a', 'b', 'c', 1, 1, 0, 2, 1, 3, 'x', 'y', 'z'})
	f.Add([]byte{1, 2, 0, 3, 2, 0, 9, 0xff, 0xff, 4, 1, 2})
	f.Fuzz(func(t *testing.T, ops []byte) {
		keys := &mockKeys{invalid: map[string]bool{}}
		c, err := tls.NewCertificateManagement(context.Background(), tls.CertificateManagementConfig{
			MaxRootCertificates: 5, MaxClientCertificates: 5, Port: keys,
		})
		if err != nil {
			t.Fatal(err)
		}
		next := func() byte {
			if len(ops) == 0 {
				return 0
			}
			b := ops[0]
			ops = ops[1:]
			return b
		}
		blob := func() []byte {
			n := int(next())
			if n > len(ops) {
				n = len(ops)
			}
			b := append([]byte(nil), ops[:n]...)
			ops = ops[n:]
			return b
		}
		id := func() spec.Nullable[uint16] {
			if v := next(); v < 128 {
				return spec.ValueOf(uint16(v) | uint16(next())<<8)
			}
			return spec.NullOf[uint16]()
		}
		for len(ops) > 0 {
			fabric := next()%3 + 1
			ctx := peer(fabric)
			switch next() % 5 {
			case 0: // RootUpsertDoesNotCrash
				_, _ = c.MatterInvoke(ctx, tcmdef.CmdProvisionRootCertificate, tcmdef.ProvisionRootCertificateRequest{Certificate: blob(), Caid: id()})
			case 1:
				nonce := make([]byte, 32)
				_, _ = c.MatterInvoke(ctx, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: nonce, Ccdid: id()})
			case 2: // ClientUpdateDoesNotCrash, ClientIntermediateCertsDoNotCrash
				inter := make([][]byte, next()%12)
				for i := range inter {
					inter[i] = blob()
				}
				_, _ = c.MatterInvoke(ctx, tcmdef.CmdProvisionClientCertificate, tcmdef.ProvisionClientCertificateRequest{Ccdid: id().Value, ClientCertificate: blob(), IntermediateCertificates: inter})
			case 3:
				_, _ = c.MatterInvoke(ctx, tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: id().Value})
			case 4:
				_, _ = c.MatterInvoke(ctx, tcmdef.CmdRemoveClientCertificate, tcmdef.RemoveClientCertificateRequest{Ccdid: id().Value})
			}
			checkTables(t, c)
		}
	})
}

// checkTables holds the invariants: node-wide unique ids, the per-fabric
// capacities, the bounds of every stored certificate.
func checkTables(t *testing.T, c *tls.CertificateManagement) {
	t.Helper()
	roots := readAs[[]tcmdef.TLSCertStruct](t, c.MatterRead, tcmdef.AttrProvisionedRootCertificates)
	seen, per := map[uint16]bool{}, map[uint8]int{}
	for _, r := range roots {
		if seen[r.Caid] || r.Caid > tls.MaxCertificateID || len(*r.Certificate) > tls.MaxCertificateBytes {
			t.Fatalf("root table %+v", roots)
		}
		seen[r.Caid] = true
		per[r.FabricIndex]++
		if per[r.FabricIndex] > 5 {
			t.Fatalf("fabric %d holds %d root certificates", r.FabricIndex, per[r.FabricIndex])
		}
	}
	clients := readAs[[]tcmdef.TLSClientCertificateDetailStruct](t, c.MatterRead, tcmdef.AttrProvisionedClientCertificates)
	seen, per = map[uint16]bool{}, map[uint8]int{}
	for _, cl := range clients {
		if seen[cl.Ccdid] || (cl.IntermediateCertificates != nil && len(*cl.IntermediateCertificates) > tls.MaxIntermediateCertificates) {
			t.Fatalf("client table %+v", clients)
		}
		seen[cl.Ccdid] = true
		per[cl.FabricIndex]++
		if per[cl.FabricIndex] > 5 {
			t.Fatalf("fabric %d holds %d client certificates", cl.FabricIndex, per[cl.FabricIndex])
		}
	}
}
