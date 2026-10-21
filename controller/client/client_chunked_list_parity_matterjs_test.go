// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientChunkedListTest.ts at
// 862ad75f: the controller reassembles a list attribute the device chunks
// across report messages — 50 channels of a Channel server's ChannelList
// on the light — through the default subscription and through a read.
package client_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/media"
	"github.com/SukramJ/go-fabric/cluster/spec"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// channelCount is ClientChunkedListTest.ts:14 CHANNEL_COUNT.
const channelCount = 50

// generateChannels is ClientChunkedListTest.ts:17-25.
func generateChannels(n int) []channeldef.ChannelInfoStruct {
	out := make([]channeldef.ChannelInfoStruct, 0, n)
	for i := range n {
		name, id := fmt.Sprintf("Channel%d", i), fmt.Sprintf("%014d", i)
		typ := channeldef.ChannelTypeSatellite
		out = append(out, channeldef.ChannelInfoStruct{MajorNumber: uint16(i), MinorNumber: uint16(i), Name: &name, Identifier: &id, Type: &typ}) //nolint:gosec // a small test count
	}
	return out
}

// verifyChannelList is ClientChunkedListTest.ts:27-36 over the decoded
// list.
func verifyChannelList(t *testing.T, v any, n int) {
	t.Helper()
	list, ok := v.(spec.List[channeldef.ChannelInfoStruct])
	if !ok || len(list) != n {
		t.Fatalf("channel list %T of %d, want %d channels", v, len(list), n)
	}
	for i, c := range list {
		if int(c.MajorNumber) != i || int(c.MinorNumber) != i || c.Name == nil || *c.Name != fmt.Sprintf("Channel%d", i) ||
			c.Identifier == nil || *c.Identifier != fmt.Sprintf("%014d", i) {
			t.Fatalf("channel %d: %+v", i, c)
		}
	}
}

// channelPort answers no command; the list is all the tests read.
type channelPort struct{}

func (channelPort) ChangeChannel(context.Context, *media.ChannelState, channeldef.ChangeChannelRequest) (channeldef.ChangeChannelResponse, error) {
	return channeldef.ChangeChannelResponse{}, nil
}

func (channelPort) ChangeChannelByNumber(context.Context, *media.ChannelState, channeldef.ChangeChannelByNumberRequest) error {
	return nil
}

func (channelPort) SkipChannel(context.Context, *media.ChannelState, channeldef.SkipChannelRequest) error {
	return nil
}

// channelPair is the OnOffLight with the Channel server
// (ClientChunkedListTest.ts:44-52) on a commissioned client pair.
func channelPair(t *testing.T, o client.CommissionOptions) (*fabrictest.Site, *client.Node, uint16) {
	t.Helper()
	site := quietSite(t)
	ch, err := media.NewChannels(media.ChannelsConfig{Port: channelPort{}, Features: channeldef.FeatureChannelList, Initial: media.ChannelState{ChannelList: generateChannels(channelCount)}})
	if err != nil {
		t.Fatal(err)
	}
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light:1"), Name: "light:1"}, device.WithServer(ch))
	if err != nil {
		t.Fatal(err)
	}
	p := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	n, err := p.Controller.CommissionNode(parityCtx(t), p.Device, o)
	if err != nil {
		t.Fatal(err)
	}
	ep, _ := p.Device.EndpointOf("light:1")
	return site, n, ep
}

func TestClientChunkedListParity(t *testing.T) {
	t.Parallel()

	t.Run("reassembles chunked list via subscription", func(t *testing.T) {
		t.Parallel()
		site, n, ep := channelPair(t, client.CommissionOptions{})
		if err := site.Eventually(parityCtx(t), "the default subscription", n.SubscriptionActive); err != nil {
			t.Fatal(err)
		}
		c := n.Endpoint(ep).Cluster(channeldef.ClusterID)
		if c == nil {
			t.Fatal("no Channel cluster in the image")
		}
		v, err := c.Decoded(channeldef.AttrChannelList)
		if err != nil {
			t.Fatal(err)
		}
		verifyChannelList(t, v, channelCount)
	})

	t.Run("reassembles chunked list via direct read", func(t *testing.T) {
		t.Parallel()
		site, n, ep := channelPair(t, client.CommissionOptions{AutoSubscribe: boolPtr(false), NoInitialRead: true})
		var rep *interaction.Report
		if err := site.Run(parityCtx(t), func(ctx context.Context) error {
			var err error
			rep, err = n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{attributePath(ep, channeldef.ClusterID, channeldef.AttrChannelList)}}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		raw, ok := rep.Value(ep, channeldef.ClusterID, channeldef.AttrChannelList)
		if !ok {
			t.Fatal("no ChannelList in the read")
		}
		v, err := channeldef.Definition.Attribute(channeldef.AttrChannelList).Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		verifyChannelList(t, v, channelCount)
	})
}
