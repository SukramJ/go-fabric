// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"encoding/json"
	"testing"
)

// TestForgottenNodeIsRemoved: a node that leaves the registry without a
// decommission — Registry.Forget fires Hooks.Deleted only — is forgotten
// by the camera service as a decommissioned one is: the requestor's
// sessions of the node go (review II, R2-P2-fabric-08).
func TestForgottenNodeIsRemoved(t *testing.T) {
	t.Parallel()
	d := newDevSite(t)
	d.startService()
	p, err := d.svc.Provider(d.n, d.ep)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"webRtcSessionId": nil, "sdp": "v=0", "streamUsage": 3, "videoStreams": []int{1}})
	d.run(func(ctx context.Context) error { _, _, err := p.Send(ctx, "ProvideOffer", payload); return err })
	if got := d.c.CameraRequestor().Sessions(); len(got) != 1 {
		t.Fatalf("requestor %+v", got)
	}
	d.run(func(ctx context.Context) error { return d.c.Registry().Forget(ctx, d.n) })
	d.eventually("the requestor dropped the forgotten node", func() bool { return len(d.c.CameraRequestor().Sessions()) == 0 })
}
