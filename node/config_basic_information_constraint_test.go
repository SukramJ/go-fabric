// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/node"
)

// TestValidateRefusesBasicInformationBeyondItsConstraint (review II,
// R2-P6-api-2407, decision 201): Config.Validate refuses each
// BasicInformation string one byte beyond the maximum length of its
// attribute's constraint in the generated definition, with
// ErrInvalidConfig naming the attribute; the maximum itself is valid.
func TestValidateRefusesBasicInformationBeyondItsConstraint(t *testing.T) {
	t.Parallel()
	for attr, set := range map[uint32]func(*node.BasicInformation, string){
		basicdef.AttrVendorName:            func(b *node.BasicInformation, v string) { b.VendorName = v },
		basicdef.AttrProductName:           func(b *node.BasicInformation, v string) { b.ProductName = v },
		basicdef.AttrProductLabel:          func(b *node.BasicInformation, v string) { b.ProductLabel = v },
		basicdef.AttrNodeLabel:             func(b *node.BasicInformation, v string) { b.NodeLabel = v },
		basicdef.AttrSerialNumber:          func(b *node.BasicInformation, v string) { b.SerialNumber = v },
		basicdef.AttrHardwareVersionString: func(b *node.BasicInformation, v string) { b.HardwareVersionString = v },
		basicdef.AttrSoftwareVersionString: func(b *node.BasicInformation, v string) { b.SoftwareVersionString = v },
	} {
		a := basicdef.Definition.Attribute(attr)
		limit := maxLength(t, a)
		t.Run(a.Name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Path: t.TempDir() + "/n.db"}
			set(&cfg.BasicInformation, strings.Repeat("x", limit))
			if a.ID == basicdef.AttrNodeLabel || a.ID == basicdef.AttrProductName {
				cfg.ProductDescription.Name = "short" // the advertised name has its own bound
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("%s of %d bytes: %v, want valid", a.Name, limit, err)
			}
			set(&cfg.BasicInformation, strings.Repeat("x", limit+1))
			err := cfg.Validate()
			if !errors.Is(err, node.ErrInvalidConfig) || !strings.Contains(err.Error(), a.Name) {
				t.Fatalf("%s of %d bytes: %v, want ErrInvalidConfig naming it", a.Name, limit+1, err)
			}
		})
	}
}

// maxLength is the maximum length a's constraint states.
func maxLength(t *testing.T, a *spec.Attribute) int {
	t.Helper()
	if a == nil || a.Constraint.Max == nil {
		t.Fatalf("%v states no maximum length", a)
	}
	return int(a.Constraint.Max.Int)
}
