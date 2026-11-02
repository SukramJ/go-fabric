// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
)

// TestGenerateNOCValidatesTheCATList mirrors matter.js
// CaseAuthenticatedTag.validateNocTagList (datatype/CaseAuthenticatedTag.ts:96-105
// at 862ad75f: at most three tags, identifier values unique) and the
// constructor's version rule (:32-33: version 0 refused), which
// kinds/Certificate.ts:989 applies when the NOC subject becomes ASN.1.
// A NOC the device would refuse at AddNOC (InvalidNOC) is never signed.
func TestGenerateNOCValidatesTheCATList(t *testing.T) {
	t.Parallel()
	c, err := ca.New(ca.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pub := newKey(t)
	cases := []struct {
		name string
		cats []uint32
		ok   bool
	}{
		{"none", nil, true},
		{"three distinct", []uint32{0x0001_0001, 0x0002_0003, 0xABCD_FFFF}, true},
		{"four", []uint32{0x0001_0001, 0x0002_0001, 0x0003_0001, 0x0004_0001}, false},
		{"duplicate identifier", []uint32{0x0001_0001, 0x0001_0002}, false},
		{"version 0", []uint32{0x0001_0000}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			noc, err := c.GenerateNOC(pub, 7, 0x42, tc.cats)
			switch {
			case tc.ok && err != nil:
				t.Fatalf("GenerateNOC: %v", err)
			case tc.ok && len(noc) == 0:
				t.Fatal("no NOC")
			case !tc.ok && !errors.Is(err, ca.ErrInvalidCATs):
				t.Fatalf("got %v, want ErrInvalidCATs", err)
			}
		})
	}
}
