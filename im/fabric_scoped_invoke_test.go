// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// TestPASERefusesEveryFabricScopedClusterFamily invokes one fabric-scoped
// request of each cluster the hand-written table used to miss over a PASE
// session: each answers UnsupportedAccess and none reaches the dispatcher.
// Before the table was generated from parity/schema.json, a PASE session
// provisioned a TLS root certificate under fabric 0 (review II,
// R2-P6-acl-1301). TestFabricScopedInvokeMatchesSnapshot (schema) holds the
// whole table; this holds the gate that reads it.
func TestPASERefusesEveryFabricScopedClusterFamily(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name             string
		cluster, command uint32
	}{
		{"TlsCertificateManagement.ProvisionRootCertificate", 0x0801, 0x00},
		{"TlsClientManagement.ProvisionEndpoint", 0x0802, 0x00},
		{"Messages.PresentMessagesRequest", 0x0097, 0x00},
		{"AccountLogin.GetSetupPin", 0x050E, 0x00},
		{"WebRtcTransportProvider.SolicitOffer", 0x0553, 0x00},
		{"PushAvStreamTransport.AllocatePushTransport", 0x0555, 0x00},
	} {
		pase := WithAuthModePASE(WithFabricFilter(context.Background(), false, 0))
		d := &grantingDispatcher{}
		resp := HandleInvokeRequest(pase, d, InvokeRequest{Invokes: []CommandInvocation{{
			Path: ConcreteCommandPath{Endpoint: 1, Cluster: c.cluster, Command: c.command, HasEndpoint: true, HasCluster: true, HasCommand: true},
		}}})
		if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != StatusUnsupportedAccess || len(d.invoked) != 0 {
			t.Errorf("%s over PASE: answered %+v and ran %v, want UnsupportedAccess and no dispatch", c.name, resp.Responses, d.invoked)
		}
	}
}
