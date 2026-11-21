// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"testing"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
)

// providerOf builds the OTA provider the controller builds over the
// manager (controller/ota_provider.go newOTAProvider), with src as its
// image catalogue and r as its BDX responder.
func providerOf(t *testing.T, r *managerRig, src mattercore.OTAImageSource, responder *bdx.Responder) *mattercore.OTAProvider {
	t.Helper()
	pc := r.m.ProviderConfig()
	p, err := mattercore.NewOTAProvider(mattercore.OTAProviderConfig{
		ImageSource: src, Consent: pc.Consent, Manager: pc.Manager, ApplyDelay: pc.ApplyDelay,
		BDX: responder, NodeID: func(uint8) (uint64, bool) { return 1, true },
		Clock: r.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if responder != nil {
		r.m.SetBDX(responder)
	}
	return p
}
