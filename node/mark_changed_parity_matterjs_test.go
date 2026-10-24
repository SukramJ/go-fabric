// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/internal/MarkChangedTest.ts
// at 862ad75f: a server reporting an attribute it computes on read, which
// change detection never sees change (ServerBehaviorBacking.ts:186-221
// markChanged).
//
// matter.js serves onOff from an accessor of the behavior's State
// (ComputedOnOffServer, MarkChangedTest.ts:22-42); the Go counterpart of a
// computed attribute is a spec.Server whose spec.Source answers the read
// ahead of the stored value, so a host's change of what the Source reads
// from moves no Set. markChanged is spec.Server.MarkChanged (the typed
// route <cluster>def.Server.MarkChanged). matter.js's `reported` list is
// the protocol's attrsChanged (:94-99); here it is the server's change
// listener, and the two cases that report run on a commissioned
// fabrictest pair, so a subscriber sees the report and the data version it
// carries. The servers are generic spec.Servers over the committed
// definitions with their commands left unimplemented: OnOff for the
// computed light, GeneralDiagnostics (UpTime: changes omitted, "C") and
// Identify (IdentifyTime: quieter, "Q").
package node_test

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gendiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/identify"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// computedOnOff is the Source of ComputedOnOffServer's accessor
// (MarkChangedTest.ts:28-42): source is what onOff is computed from, and
// not an attribute.
type computedOnOff struct{ source atomic.Bool }

func (c *computedOnOff) MatterAttribute(attrID uint32) (any, bool) {
	if attrID == onoffdef.AttrOnOff {
		return c.source.Load(), true
	}
	return nil, false
}

// computedServer is computedNode's server (MarkChangedTest.ts:91-104)
// and the attribute ids its change listener saw.
type computedServer struct {
	srv      *spec.Server
	computed *computedOnOff
	mu       sync.Mutex
	reported []uint32
}

func (c *computedServer) seen() []uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.reported)
}

func newComputedServer(t *testing.T) *computedServer {
	t.Helper()
	c := &computedServer{computed: &computedOnOff{}}
	srv, err := spec.NewServer(onoffdef.Definition, spec.Options{}, spec.ServerConfig{Source: c.computed, Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	c.srv = srv
	srv.OnAttributeChange(func(change contract.Change) {
		c.mu.Lock()
		c.reported = append(c.reported, change.Attributes...)
		c.mu.Unlock()
	})
	return c
}

// serverOf is a generic server over def with its commands unimplemented.
func serverOf(t *testing.T, def *spec.Cluster) *spec.Server {
	t.Helper()
	srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// computedPair commissions a device whose bridged light serves the
// computed OnOff server and subscribes to onOff; the reports arrive on
// the returned channel.
func computedPair(t *testing.T, c *computedServer) (*fabrictest.Site, *fabrictest.Pair, uint16, chan *interaction.Report) {
	t.Helper()
	site := quietSite(t)
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{fabrictest.Endpoint("computed:1", onoff.DeviceTypeOnOffLight, c.srv)},
	}})
	ep, ok := p.Device.EndpointOf("computed:1")
	if !ok {
		t.Fatal("the light has no endpoint")
	}
	reports := make(chan *interaction.Report, 16)
	sub, err := fabrictest.Subscribe(parityCtx(t), p, interaction.SubscribeRequest{
		Attributes: []fabrictest.AttributePath{client.AttributeAt(ep, onoffdef.ClusterID, onoffdef.AttrOnOff)},
		Updated:    func(r *interaction.Report) { reports <- r },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)
	return site, p, ep, reports
}

// awaitOnOff waits for a report of onOff = want and returns its entry.
func awaitOnOff(t *testing.T, site *fabrictest.Site, ep uint16, reports chan *interaction.Report, want bool) interaction.AttributeData {
	t.Helper()
	var got interaction.AttributeData
	err := site.Eventually(parityCtx(t), "the onOff report", func() bool {
		select {
		case r := <-reports:
			for _, a := range r.Attributes {
				if a.Path.Endpoint == ep && a.Path.Cluster == onoffdef.ClusterID && a.Path.Attribute == onoffdef.AttrOnOff && a.Value.El.Bool == want {
					got = a
					return true
				}
			}
		default:
		}
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestMarkChangedParityMatterJS(t *testing.T) {
	t.Run("reports an attribute the behavior computes on read", func(t *testing.T) {
		c := newComputedServer(t)
		site, _, ep, reports := computedPair(t, c)
		c.computed.source.Store(true)
		if err := c.srv.MarkChanged(onoffdef.AttrOnOff); err != nil {
			t.Fatal(err)
		}
		if got := c.seen(); !slices.Equal(got, []uint32{onoffdef.AttrOnOff}) {
			t.Fatalf("reported %v, want [onOff]", got)
		}
		awaitOnOff(t, site, ep, reports, true)
	})

	t.Run("advances the data version, which nothing else does for a computed value", func(t *testing.T) {
		c := newComputedServer(t)
		site, p, ep, reports := computedPair(t, c)
		before := c.srv.MatterDataVersion()
		// A bridged endpoint's wire version is the endpoint's, advanced by
		// the change the server reports (endpoint.Endpoint.ClusterDataVersion).
		read, err := fabrictest.Read(parityCtx(t), p, client.AttributeAt(ep, onoffdef.ClusterID, onoffdef.AttrOnOff))
		if err != nil {
			t.Fatal(err)
		}
		c.computed.source.Store(true)
		if c.srv.MatterDataVersion() != before {
			t.Fatal("the source's change moved the data version")
		}
		// The typed route: onoffdef.Server.MarkChanged reaches the same.
		if err := onoffdef.Wrap(c.srv).MarkChanged(onoffdef.AttrOnOff); err != nil {
			t.Fatal(err)
		}
		if got := c.srv.MatterDataVersion(); got != before+1 {
			t.Fatalf("data version %d, want %d", got, before+1)
		}
		if a := awaitOnOff(t, site, ep, reports, true); a.DataVersion != read.DataVersion+1 {
			t.Fatalf("the report carries data version %d, want %d", a.DataVersion, read.DataVersion+1)
		}
	})

	t.Run("refuses a report that names no attribute", func(t *testing.T) {
		c := newComputedServer(t)
		before := c.srv.MatterDataVersion()
		if err := c.srv.MarkChanged(); !errors.Is(err, spec.ErrNotReportable) || !refusedWith(err, "without naming an attribute") {
			t.Fatalf("MarkChanged() = %v, want ErrNotReportable without naming an attribute", err)
		}
		if c.srv.MatterDataVersion() != before {
			t.Fatal("a refused report moved the data version")
		}
	})

	t.Run("refuses an attribute the behavior does not support", func(t *testing.T) {
		c := newComputedServer(t)
		// OnTime is optional (LT) and not served without the Lighting feature.
		if err := c.srv.MarkChanged(onoffdef.AttrOnTime); !errors.Is(err, spec.ErrNotServed) || !refusedWith(err, "not an attribute") {
			t.Fatalf("MarkChanged(OnTime) = %v, want ErrNotServed, not an attribute", err)
		}
	})

	t.Run("refuses a state field that is not an attribute", func(t *testing.T) {
		c := newComputedServer(t)
		// The Source's own state has no attribute id; an id OnOff does not
		// define is the nearest the Go surface can name.
		if err := c.srv.MarkChanged(0x00F0); !errors.Is(err, spec.ErrNotServed) || !refusedWith(err, "not an attribute") {
			t.Fatalf("MarkChanged(0x00F0) = %v, want ErrNotServed, not an attribute", err)
		}
		if got := c.seen(); len(got) != 0 {
			t.Fatalf("reported %v", got)
		}
	})

	t.Run("refuses a quieter attribute, which reports through its own throttled event", func(t *testing.T) {
		srv := serverOf(t, identify.Definition)
		if err := srv.MarkChanged(identify.AttrIdentifyTime); !errors.Is(err, spec.ErrNotReportable) || !refusedWith(err, "so its throttle applies") {
			t.Fatalf("MarkChanged(IdentifyTime) = %v, want ErrNotReportable, so its throttle applies", err)
		}
	})

	t.Run("refuses an attribute the specification omits change reporting for", func(t *testing.T) {
		srv := serverOf(t, gendiagdef.Definition)
		if err := srv.MarkChanged(gendiagdef.AttrUpTime); !errors.Is(err, spec.ErrNotReportable) || !refusedWith(err, "omits change reporting") {
			t.Fatalf("MarkChanged(UpTime) = %v, want ErrNotReportable, omits change reporting", err)
		}
	})

	t.Run("carries an implementation that refuses, for backings that do not own their version", func(t *testing.T) {
		// A self-held server owns no spec.Server's version: the typed route
		// refuses it, as BehaviorBacking.markChanged refuses a backing that
		// is not a server's (BehaviorBacking.ts:440-444).
		self, err := onoff.New(onoff.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := spec.MarkChangedOf(self, onoffdef.AttrOnOff); !errors.Is(err, spec.ErrNoMarkChanged) || !refusedWith(err, "only a server") {
			t.Fatalf("MarkChangedOf(self-held) = %v, want ErrNoMarkChanged, only a server", err)
		}
	})

	t.Run("advances the data version when a computed quieter attribute reports through its throttle", func(t *testing.T) {
		t.Skip("MarkChangedTest.ts:185: spec.Server has no throttled report path for a quieter attribute served from a Source; the self-held servers bump the version at the change itself")
	})

	t.Run("serves the attribute from the accessor rather than a stored slot", func(t *testing.T) {
		c := newComputedServer(t)
		c.computed.source.Store(true)
		if v, ok := c.srv.MatterRead(onoffdef.AttrOnOff); !ok || v != true {
			t.Fatalf("onOff reads %v, %v; want true from the source", v, ok)
		}
		if v, _ := c.srv.Value(onoffdef.AttrOnOff); v == true {
			t.Fatalf("the stored slot holds %v; the source's change reached it", v)
		}
		if got := c.seen(); len(got) != 0 {
			t.Fatalf("reported %v, want nothing", got)
		}
	})
}

// refusedWith reports whether err carries s in its text.
func refusedWith(err error, s string) bool { return err != nil && strings.Contains(err.Error(), s) }
