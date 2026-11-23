// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/internal/deviceseam"
)

// TestRefusedCompositeMovesNoComponent (R2-C2-fsm-2506): a generated
// composite constructor takes its components all or none. Camera is a
// bridged device a node holds, so NewFloodlightCamera is refused with
// ErrAddedElsewhere — and OnOffLight, the component before it, stays where
// it was: no owner, and a node may still add it. Before, OnOffLight had
// moved to the discarded device and a node refused it as a part.
func TestRefusedCompositeMovesNoComponent(t *testing.T) {
	t.Parallel()
	light, err := NewOnOffLight(testOptions("fc/light"), testWith(t, "OnOffLight")...)
	if err != nil {
		t.Fatal(err)
	}
	cam, err := NewCamera(testOptions("fc/cam"), testWith(t, "Camera")...)
	if err != nil {
		t.Fatal(err)
	}
	detach, _, err := deviceseam.Attach(cam, newTestHost(map[string]uint16{"test:fc/cam": 3}))
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if _, err := NewFloodlightCamera(testOptions("fc"), light, cam); !errors.Is(err, ErrAddedElsewhere) {
		t.Fatalf("NewFloodlightCamera with a Camera a node holds: %v, want ErrAddedElsewhere", err)
	}
	if o := light.owner.Load(); o != nil {
		t.Errorf("the refused constructor moved OnOffLight to %s", o.opts.StableKey)
	}
	detachLight, _, err := deviceseam.Attach(light, newTestHost(map[string]uint16{"test:fc/light": 4}))
	if err != nil {
		t.Fatalf("a node adds OnOffLight after the refused constructor: %v, want nil", err)
	}
	detachLight()
}
