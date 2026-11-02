// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/netinfra"
	tdir "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	wdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkmanagement"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// octets is a bare octet-string attribute value for a write.
type octets []byte

func (o octets) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.PutOctets(tag, o) }

// TestNetinfraOverTheWire runs both servers on a commissioned pair: the
// bridge stamps the CASE session the passphrase and the dataset need
// (bridge/receive_dispatch.go:511-515), the timed AddNetwork stores a
// network, the controller's write of PreferredExtendedPanID passes chip's
// rule, and an untimed AddNetwork is refused by the definition's timed
// requirement.
func TestNetinfraOverTheWire(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	wifi, err := netinfra.NewWiFiManagement(netinfra.WiFiManagementConfig{Clock: site.Clock()})
	mustT(t, err)
	mustT(t, wifi.SetNetworkCredentials(testSSID, testPassphrase))
	dir, err := netinfra.NewThreadDirectory(ctx, netinfra.ThreadDirectoryConfig{})
	mustT(t, err)
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{fabrictest.Endpoint("nim", 0x0090, wifi, dir)},
	}})
	ep, ok := pair.Device.EndpointOf("nim")
	if !ok {
		t.Fatal("no nim endpoint")
	}

	res, err := fabrictest.Invoke(ctx, pair, client.CommandAt(ep, wdef.ClusterID, wdef.CmdNetworkPassphraseRequest), wdef.NetworkPassphraseRequestRequest{})
	mustT(t, err)
	var pass wdef.NetworkPassphraseResponse
	mustT(t, pass.DecodeTLV(res.Fields))
	if !bytes.Equal(pass.Passphrase, testPassphrase) {
		t.Error("NetworkPassphraseResponse does not carry the passphrase")
	}

	add := client.CommandAt(ep, tdir.ClusterID, tdir.CmdAddNetwork)
	_, err = fabrictest.InvokeUntimed(ctx, pair, add, tdir.AddNetworkRequest{OperationalDataset: dataset1})
	var se *fabrictest.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusNeedsTimedInteraction {
		t.Errorf("untimed AddNetwork: %v, want NEEDS_TIMED_INTERACTION", err)
	}
	_, err = fabrictest.InvokeTimed(ctx, pair, add, tdir.AddNetworkRequest{OperationalDataset: dataset1})
	mustT(t, err)

	pref := client.AttributeAt(ep, tdir.ClusterID, tdir.AttrPreferredExtendedPanId)
	mustT(t, fabrictest.Write(ctx, pair, pref, octets(exPanID1)))
	err = fabrictest.Write(ctx, pair, pref, octets(exPanID2))
	if !errors.As(err, &se) || se.Status != im.StatusConstraintError {
		t.Errorf("write of an unknown network: %v, want CONSTRAINT_ERROR", err)
	}

	res, err = fabrictest.Invoke(ctx, pair, client.CommandAt(ep, tdir.ClusterID, tdir.CmdGetOperationalDataset), tdir.GetOperationalDatasetRequest{ExtendedPanId: exPanID1})
	mustT(t, err)
	var ds tdir.OperationalDatasetResponse
	mustT(t, ds.DecodeTLV(res.Fields))
	if !bytes.Equal(ds.OperationalDataset, dataset1) {
		t.Error("OperationalDatasetResponse does not carry the stored dataset")
	}
}
