// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// The responders' session parameters carry the node's TCP roles (issue
// #89): PBKDFParamResponse tag 5 and Sigma2's responderSessionParams name
// tcpServer / tcpClient as matter.js ServerNetworkRuntime.ts:355-363 sets
// them on the SessionManager.

import (
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// pbkdfParamRequest is a minimal PBKDFParamRequest: initiatorRandom (1),
// initiatorSessionId (2) 42, passcodeId (3) 0, hasPBKDFParameters (4) false.
func pbkdfParamRequest() []byte {
	out := []byte{0x15, 0x30, 0x01, byte(spake2.PBKDFRandomSize)}
	out = append(out, make([]byte, spake2.PBKDFRandomSize)...)
	return append(out, 0x25, 0x02, 0x2A, 0x00, 0x25, 0x03, 0x00, 0x00, 0x28, 0x04, 0x18)
}

func TestResponderSessionParamsCarryTCP(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		tcp  bridge.TCPConfig
		want uint16
	}{
		"off":      {bridge.TCPConfig{}, 0},
		"incoming": {bridge.TCPConfig{Incoming: true}, sessionparams.TransportTCPServer},
		"outgoing": {bridge.TCPConfig{Outgoing: true}, sessionparams.TransportTCPClient},
		"both":     {bridge.TCPConfig{Incoming: true, Outgoing: true}, sessionparams.TransportTCPServer | sessionparams.TransportTCPClient},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := responderSessionParams(tc.tcp)
			if p.SupportedTransports != tc.want {
				t.Fatalf("SupportedTransports %#x, want %#x", p.SupportedTransports, tc.want)
			}
			d := bridge.DefaultSessionParameters()
			if p.SessionIdleInterval != d.SessionIdleInterval || p.SessionActiveInterval != d.SessionActiveInterval || p.MaxPathsPerInvoke != d.MaxPathsPerInvoke {
				t.Errorf("the MRP intervals and MaxPathsPerInvoke moved: %+v", *p)
			}
			if tc.want != 0 && p.MaxTCPMessageSize != sessionparams.DefaultMaxTCPMessageSize {
				t.Errorf("MaxTCPMessageSize %d with TCP, want %d", p.MaxTCPMessageSize, sessionparams.DefaultMaxTCPMessageSize)
			}

			sessions := operational.NewManager(testStore(t))
			// CASE: Sigma2's responderSessionParams (CaseServer.ts:272).
			ca, err := buildCaseAdapter(sessions, newCaseIdentities(slog.Default()), p, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			if got := ca.SnapshotResponder().SessionParameters(); got == nil || got.SupportedTransports != tc.want {
				t.Errorf("CASE responder parameters %+v, want transports %#x", got, tc.want)
			}
			// PASE: PBKDFParamResponse tag 5 (PaseServer.ts:151).
			vc, err := spake2.NewVerifierContext(20202021, []byte("SPAKE2P Key Salt"), 1000)
			if err != nil {
				t.Fatal(err)
			}
			pa, err := buildPaseAdapterFromContext(sessions, &RootClusters{}, vc, []byte("SPAKE2P Key Salt"), 1000, p)
			if err != nil {
				t.Fatal(err)
			}
			_, respBytes, err := pa.ProcessPBKDFParamRequest(pbkdfParamRequest())
			if err != nil {
				t.Fatalf("ProcessPBKDFParamRequest: %v", err)
			}
			resp, err := spake2.DecodePBKDFParamResponse(respBytes)
			if err != nil {
				t.Fatal(err)
			}
			if got := resp.ResponderSessionParams; got == nil || got.SupportedTransports != tc.want {
				t.Errorf("PBKDFParamResponse tag 5 %+v, want transports %#x", got, tc.want)
			}
		})
	}
}
