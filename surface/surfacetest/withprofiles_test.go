// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surfacetest

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/internal/surfaceseam"
	"github.com/SukramJ/go-fabric/surface"
)

func TestWithProfiles(t *testing.T) {
	prof := surface.Profile{Name: "synthetic"}
	p := WithProfiles(surface.Policies{Profiles: []string{"alexa"}}, prof)
	if !slices.Equal(p.Profiles, []string{"alexa", "synthetic"}) || p.Validate() != nil {
		t.Fatalf("%v %v", p.Profiles, p.Validate())
	}
	if (surface.Policies{Profiles: []string{"synthetic"}}).Validate() == nil {
		t.Fatal("an injected profile leaked into the register")
	}
}

func TestWithProfilesWithoutTheSeam(t *testing.T) {
	saved := surfaceseam.WithProfiles
	t.Cleanup(func() { surfaceseam.WithProfiles = saved })
	surfaceseam.WithProfiles = func(any, any) (any, bool) { return nil, false }
	defer func() {
		if recover() == nil {
			t.Fatal("no panic without the seam")
		}
	}()
	WithProfiles(surface.Policies{})
}
