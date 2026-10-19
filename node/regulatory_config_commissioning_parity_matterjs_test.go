// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/RegulatoryConfigCommissioningTest.ts
// at 862ad75f: the commissioner sends SetRegulatoryConfig, with its
// mandatory country code, to a device with a Wi-Fi NetworkCommissioning
// interface — the default "XX" when the caller names none, the caller's
// otherwise.
//
// matter.js truncates its flow after the regulatory step and checks that
// the step passed, with no "countryCode" in the error; the module's
// commissioner has no flow-class seam, so the port commissions a Wi-Fi
// device on fabrictest to the end (the driver accepts the network) and
// checks that commissioning succeeded — the regulatory step is mandatory
// for the radio device, so it passed. What the device keeps of the code is
// not asserted: the Go device drops SetRegulatoryConfig's CountryCode on
// decode (bridge/fields_reader.go decodeSetRegulatoryConfigRequest, a
// deliberate revert after an Apple Home regression), where matter.js writes
// it to BasicInformation.Location (GeneralCommissioningServer.ts:146-170,
// 206-208) — a register candidate.
package node_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// acceptingWiFi is a Wi-Fi driver that finds and joins every network.
type acceptingWiFi struct{}

func (acceptingWiFi) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", []contract.WiFiScanResult{{SSID: append([]byte(nil), ssid...), BSSID: make([]byte, 6), Channel: 6}}
}

func (acceptingWiFi) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	return 0, "", 0
}

func TestRegulatoryConfigCommissioningParity(t *testing.T) {
	t.Parallel()

	commission := func(t *testing.T, countryCode string) {
		t.Helper()
		site := quietSite(t)
		u := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
			cfg.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: acceptingWiFi{}}
		}}})
		ctx := parityCtx(t)
		p, err := u.Controller.Commission(ctx, u.Device, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
			o.WiFiNetwork = &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"}
			o.RegulatoryCountryCode = countryCode
		}})
		if err != nil {
			if strings.Contains(err.Error(), "ountryCode") {
				t.Fatalf("the regulatory step failed on the country code: %v", err)
			}
			t.Fatalf("commissioning: %v", err)
		}
		if !p.Commissioned() {
			t.Fatal("not commissioned")
		}
	}

	t.Run("sends the default country code when the caller omits regulatoryCountryCode", func(t *testing.T) {
		t.Parallel()
		commission(t, "")
	})

	t.Run("sends an explicit country code when provided", func(t *testing.T) {
		t.Parallel()
		commission(t, "NL")
	})
}
