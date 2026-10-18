// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange_test

// A port of matter.js packages/protocol/test/session/SessionParametersTest.ts
// at 862ad75f: the SessionParameters factory (exchange.PeerParameters) — its
// intervals, maxPathsPerInvoke fallback and TCP spec-version gate — and
// SessionParameters.reported, whose Go counterpart is the decoded
// sessionparams.Parameters itself (a zero field is "not sent").

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/tlv"
)

func TestSessionParameters(t *testing.T) {
	t.Parallel()
	t.Run("accepts idle/active intervals above one hour", func(t *testing.T) {
		t.Parallel()
		twoHours := uint32(2 * time.Hour / time.Millisecond)
		p := exchange.PeerParameters(&sessionparams.Parameters{SessionIdleInterval: twoHours, SessionActiveInterval: twoHours})
		if p.SessionIdleInterval != twoHours || p.SessionActiveInterval != twoHours {
			t.Fatalf("idle/active = %d/%d, want %d", p.SessionIdleInterval, p.SessionActiveInterval, twoHours)
		}
	})
}

func TestSessionParametersMaxPathsPerInvoke(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		reported *sessionparams.Parameters
		want     uint16
	}{
		{"honors a peer-reported limit above one", &sessionparams.Parameters{MaxPathsPerInvoke: 9}, 9},
		{"assumes one when the peer reports zero", &sessionparams.Parameters{MaxPathsPerInvoke: 0}, 1},
		// A Go Parameters carries "no limit" as nil or a zero field.
		{"assumes one when the peer reports no limit", nil, 1},
		{"honors the highest limit the wire can carry", &sessionparams.Parameters{MaxPathsPerInvoke: 0xffff}, 0xffff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := exchange.PeerParameters(tc.reported).MaxPathsPerInvoke; got != tc.want {
				t.Fatalf("MaxPathsPerInvoke = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSessionParametersTCPSpecVersionGate(t *testing.T) {
	t.Parallel()
	tcp := sessionparams.TransportTCPClient | sessionparams.TransportTCPServer
	t.Run("clears TCP support when the spec version is unknown", func(t *testing.T) {
		t.Parallel()
		if got := exchange.PeerParameters(&sessionparams.Parameters{SupportedTransports: tcp}).SupportedTransports; got&tcp != 0 {
			t.Fatalf("SupportedTransports = %#x, want TCP cleared", got)
		}
	})
	t.Run("decodes a numeric supportedTransports bitmap before gating", func(t *testing.T) {
		t.Parallel()
		p := exchange.PeerParameters(&sessionparams.Parameters{
			SpecificationVersion: sessionparams.MinTCPSpecificationVersion,
			SupportedTransports:  0b100, // tcpServer (bit 2)
		})
		if p.SupportedTransports&sessionparams.TransportTCPServer == 0 {
			t.Fatalf("SupportedTransports = %#x, want tcpServer", p.SupportedTransports)
		}
	})
}

// sessionParametersReported decodes a SessionParameters structure written by
// fill — the parameters as the peer sent them, before any fallback.
func sessionParametersReported(t *testing.T, fill func(enc *tlv.Encoder)) sessionparams.Parameters {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	fill(enc)
	if err := enc.EndContainer(); err != nil {
		t.Fatal(err)
	}
	buf, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(buf)
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	p, err := sessionparams.DecodeFields(dec)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSessionParametersReported(t *testing.T) {
	t.Parallel()
	t.Run("keeps only the values the peer sent, without fallbacks", func(t *testing.T) {
		t.Parallel()
		got := sessionParametersReported(t, func(enc *tlv.Encoder) { enc.PutUint32(tlv.ContextTag(1), 1234) })
		if want := (sessionparams.Parameters{SessionIdleInterval: 1234}); got != want {
			t.Fatalf("reported = %+v, want %+v", got, want)
		}
	})
	t.Run("decodes supported transports sent as a bitmap", func(t *testing.T) {
		t.Parallel()
		got := sessionParametersReported(t, func(enc *tlv.Encoder) { enc.PutUint16(tlv.ContextTag(8), 0b110) })
		want := sessionparams.Parameters{SupportedTransports: sessionparams.TransportTCPClient | sessionparams.TransportTCPServer}
		if got != want {
			t.Fatalf("reported = %+v, want %+v", got, want)
		}
	})
	t.Run("is undefined when the peer sent no parameters", func(t *testing.T) {
		t.Parallel()
		if got := sessionParametersReported(t, func(*tlv.Encoder) {}); !got.IsEmpty() {
			t.Fatalf("reported = %+v, want empty", got)
		}
		if !(sessionparams.Parameters{SessionIdleInterval: 0}).IsEmpty() {
			t.Fatal("a parameter set with only an undefined interval is not empty")
		}
	})
}
