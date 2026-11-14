// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	icd "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
)

// readPrivilegesAboveView reads every attribute's read privilege above View
// from the embedded schema snapshot: (cluster, attribute) → "O", "M" or "A".
func readPrivilegesAboveView(t *testing.T) map[[2]uint32]string {
	t.Helper()
	var doc struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Attributes []struct {
				ID        uint32 `json:"id"`
				Effective struct {
					Access struct {
						ReadPriv string `json:"readPriv"`
					} `json:"access"`
				} `json:"effective"`
			} `json:"attributes"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &doc); err != nil {
		t.Fatal(err)
	}
	out := map[[2]uint32]string{}
	for _, c := range doc.Clusters {
		for _, a := range c.Attributes {
			if p := a.Effective.Access.ReadPriv; p != "" && p != "V" {
				out[[2]uint32{c.ID, a.ID}] = p
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("the schema names %d attributes above View; the extract was not understood", len(out))
	}
	return out
}

// TestViewSubjectReadsNoAttributeAboveItsReadPrivilege: a subject is given
// an attribute's value only when its ACL grants the attribute's read
// privilege, the data model's (parity/schema.json): a View subject reads
// no attribute whose read privilege is Operate, Manage or Administer,
// neither by a concrete path (UNSUPPORTED_ACCESS) nor inside a wildcard —
// ICDManagement's RegisteredClients and ICDCounter and GeneralCommissioning's
// terms-and-conditions attributes among them (review II, R2-P3-admin-02).
func TestViewSubjectReadsNoAttributeAboveItsReadPrivilege(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{newPlug(t)},
		Configure: func(cfg *node.Config) {
			cfg.ICD = &node.ICDConfig{
				IdleModeDuration: 4 * time.Second, MaximumCheckInBackoff: 4 * time.Second,
				ActiveModeDuration: 2 * time.Second, ActiveModeThreshold: time.Second,
				ClientsSupportedPerFabric: 2,
			}
			// The terms and conditions are an "Ahead of matter.js" row a
			// host enables (ADR 0030).
			cfg.Surface = surface.Policies{Extensions: surface.NewExtensions("BD-chip-GeneralCommissioning-TermsAndConditions")}
			cfg.Commissioning.TermsAndConditions = &node.TermsAndConditions{Version: 3, Acknowledgements: 1}
		},
	}, Commission: fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
		o.TermsAndConditions = &commissioner.TermsAndConditionsAcknowledgement{Version: 3, Acknowledgements: 1}
	}}})
	ctx := testContext(t)

	// The administrator registers a Check-In client, so RegisteredClients
	// holds an entry of the fabric.
	reg := icd.RegisterClientRequest{CheckInNodeId: 0xC0FFEE, MonitoredSubject: 0xC0FFEE, Key: bytes.Repeat([]byte{7}, 16)}
	if _, err := fabrictest.Invoke(ctx, p, im.ConcreteCommandPath{Endpoint: 0, Cluster: icd.ClusterID, Command: icd.CmdRegisterClient}, reg); err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if err := p.Grant(ctx, store.PrivilegeView); err != nil {
		t.Fatal(err)
	}
	above := readPrivilegesAboveView(t)

	r, err := fabrictest.ReadWith(ctx, p, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}})
	if err != nil {
		t.Fatalf("wildcard read as View: %v", err)
	}
	if len(r.Attributes) < 50 {
		t.Fatalf("the wildcard read returned %d attributes; the View subject reads next to nothing", len(r.Attributes))
	}
	for _, d := range r.Attributes {
		if priv, ok := above[[2]uint32{d.Path.Cluster, d.Path.Attribute}]; ok {
			t.Errorf("wildcard read as View returned %#06x/%#06x (read privilege %s)", d.Path.Cluster, d.Path.Attribute, priv)
		}
	}
	for _, at := range []im.ConcreteAttributePath{
		rootAttr(icd.ClusterID, icd.AttrRegisteredClients), rootAttr(icd.ClusterID, icd.AttrIcdCounter),
		rootAttr(gencomm.ClusterID, gencomm.AttrTcAcceptedVersion), rootAttr(gencomm.ClusterID, gencomm.AttrTcAcknowledgements),
	} {
		_, err := fabrictest.Read(ctx, p, at)
		var se *interaction.StatusError
		if !errors.As(err, &se) || se.Status != im.StatusUnsupportedAccess {
			t.Errorf("concrete read of %#06x/%#06x as View: %v, want UNSUPPORTED_ACCESS", at.Cluster, at.Attribute, err)
		}
	}
}
