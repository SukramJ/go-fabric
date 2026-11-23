// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// packs reports whether the record set of svc, with host as the SRV
// target, packs into one DNS message that a receiver can unpack — what
// every announcement and answer for it must do. The unpack is part of it:
// the packer emits a name over 255 octets, which every receiver (the
// unpacker here included) refuses.
func packs(svc Service, host string) error {
	m := new(dns.Msg)
	m.Response = true
	m.Answer = recordSet(svc, namesOf(svc, host), svc.MarshalTXT(), svc.Addresses)
	b, err := m.Pack()
	if err != nil {
		return err
	}
	return new(dns.Msg).Unpack(b)
}

// TestValidateAcceptsOnlyWhatPacks is the standing guard of
// R2-C2-err-0252's class: Validate passes a Service only when its record
// set packs, so a name that cannot go on the wire fails Publish instead
// of publishing nothing. Each case is checked both ways — Validate's
// verdict against the packer's.
func TestValidateAcceptsOnlyWhatPacks(t *testing.T) {
	base := func() Service {
		return Service{
			InstanceName: "9C71D38FBE48F2E5-0000000012345678",
			ServiceType:  ServiceTypeCommissionable,
			Port:         5540,
			HostName:     "go-fabric",
			Addresses:    []net.IP{net.ParseIP("fd00::1")},
			TXT:          []TXTRecord{{Key: "D", Value: "3840"}},
			Subtypes:     []string{"_L3840", "_S15", "_CM"},
		}
	}
	label := func(n int) string { return strings.Repeat("a", n) }
	long := strings.Repeat(label(63)+".", 4) // 4×64 octets: over 255 with any domain
	cases := []struct {
		name string
		mod  func(*Service)
		ok   bool
	}{
		{"base", func(*Service) {}, true},
		{"host label 63", func(s *Service) { s.HostName = label(63) }, true},
		{"host label 64", func(s *Service) { s.HostName = label(64) }, false},
		{"host label 70 (the probe)", func(s *Service) { s.HostName = label(70) }, false},
		{"host with .local suffix", func(s *Service) { s.HostName = "go-fabric.local" }, true},
		{"host second label 64", func(s *Service) { s.HostName = "a." + label(64) }, false},
		{"host empty inner label", func(s *Service) { s.HostName = "a..b" }, false},
		{"host name over 255", func(s *Service) { s.HostName = strings.TrimSuffix(long, ".") }, false},
		{"subtype 63", func(s *Service) { s.Subtypes = []string{label(63)} }, true},
		{"subtype 64", func(s *Service) { s.Subtypes = []string{label(64)} }, false},
		{"domain label 64", func(s *Service) { s.Domain = label(64) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := base()
			tc.mod(&svc)
			verr := svc.Validate()
			perr := packs(svc, svc.HostName)
			if (verr == nil) != tc.ok {
				t.Fatalf("Validate() = %v, want ok=%v", verr, tc.ok)
			}
			if verr != nil && !errors.Is(verr, ErrInvalidService) {
				t.Fatalf("Validate() = %v, want ErrInvalidService", verr)
			}
			if (perr == nil) != (verr == nil) {
				t.Fatalf("Validate() = %v but Pack()+Unpack() = %v: the two must agree", verr, perr)
			}
		})
	}
}

// TestPublishRefusesUnpackableHost: the Responder's own host-name
// override is held to the same rule as the Service's. R2-C2-err-0252.
func TestPublishRefusesUnpackableHost(t *testing.T) {
	svc := Service{
		InstanceName: "9C71D38FBE48F2E5-0000000012345678",
		ServiceType:  ServiceTypeOperational,
		Port:         5540,
		HostName:     strings.Repeat("h", 70),
	}
	r := NewResponder()
	defer r.Close()
	if err := r.Publish(context.Background(), svc); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("Publish(host label 70) = %v, want ErrInvalidService", err)
	}
	r2 := NewResponder()
	r2.HostName = strings.Repeat("h", 70)
	defer r2.Close()
	svc.HostName = "go-fabric"
	if err := r2.Publish(context.Background(), svc); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("Publish(Responder.HostName label 70) = %v, want ErrInvalidService", err)
	}
}
