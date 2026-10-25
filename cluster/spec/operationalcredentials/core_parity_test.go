// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operationalcredentials_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestCoreServerMatchesDefinition holds the hand-written
// OperationalCredentials server of cluster/core (ADR 0013's skip list)
// against the generated definition: its cluster id, revision, lists and
// privileges (spectest.CheckHandWrittenServer) and its exported wire
// constants. The server is not built on the definition; the definition is
// the client role's typed view of the same cluster (ADR 0013, amendment
// of 2026-10-25).
func TestCoreServerMatchesDefinition(t *testing.T) {
	t.Parallel()
	srv, err := core.NewOperationalCredentials(memstore.New(), core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckHandWrittenServer(t, srv, operationalcredentials.Definition)
	for _, c := range []struct {
		name      string
		got, want uint8
	}{
		{"Ok", core.NOCStatusOK, uint8(operationalcredentials.NodeOperationalCertStatusOk)},
		{"InvalidPublicKey", core.NOCStatusInvalidPublicKey, uint8(operationalcredentials.NodeOperationalCertStatusInvalidPublicKey)},
		{"InvalidNodeOpId", core.NOCStatusInvalidNodeOpID, uint8(operationalcredentials.NodeOperationalCertStatusInvalidNodeOpId)},
		{"InvalidNoc", core.NOCStatusInvalidNOC, uint8(operationalcredentials.NodeOperationalCertStatusInvalidNoc)},
		{"MissingCsr", core.NOCStatusMissingCsr, uint8(operationalcredentials.NodeOperationalCertStatusMissingCsr)},
		{"TableFull", core.NOCStatusTableFull, uint8(operationalcredentials.NodeOperationalCertStatusTableFull)},
		{"InvalidAdminSubject", core.NOCStatusInvalidAdminSubject, uint8(operationalcredentials.NodeOperationalCertStatusInvalidAdminSubject)},
		{"FabricConflict", core.NOCStatusFabricConflict, uint8(operationalcredentials.NodeOperationalCertStatusFabricConflict)},
		{"LabelConflict", core.NOCStatusLabelConflict, uint8(operationalcredentials.NodeOperationalCertStatusLabelConflict)},
		{"InvalidFabricIndex", core.NOCStatusInvalidFabricIndex, uint8(operationalcredentials.NodeOperationalCertStatusInvalidFabricIndex)},
	} {
		if c.got != c.want {
			t.Errorf("NodeOperationalCertStatus %s: server %d, definition %d", c.name, c.got, c.want)
		}
	}
}
