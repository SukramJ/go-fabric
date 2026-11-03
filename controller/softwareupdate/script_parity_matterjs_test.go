// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

// The scripted provider (script.go) against matter.js's cert harness
// provider, RecordingOtaProviderServer
// (support/chip-testing/src/cert/InProcessControllerAdapter.ts:456-638 at
// 862ad75f): which answers state the script directly, which run the
// provider's own path with the script laid over it, and that every answer
// is recorded.

var testOTAPeer = mattercore.OTAPeer{FabricIndex: testPeer.FabricIndex, NodeID: testPeer.NodeID}

func ptrTo[T any](v T) *T { return &v }

// scriptRig is a manager with the script hooks, the exchanges it reported
// and the provider's own handlers as fakes that count their calls.
type scriptRig struct {
	r         *managerRig
	exchanges []ProviderExchange
	queries   []QueryImageScript
	applies   []ApplyUpdateScript
	ownCalls  int
}

func newScriptRig(t *testing.T) *scriptRig {
	t.Helper()
	s := &scriptRig{}
	s.r = newManagerRig(t, func(c *Config) {
		c.ApplyDelay = 5 * time.Second
		c.OnProviderExchange = func(x ProviderExchange) { s.exchanges = append(s.exchanges, x) }
		// One entry per command, in order; a spent list lets the provider
		// answer for itself (OtaProviderScript, controller-adapter.ts:617-620).
		c.ScriptQueryImage = func(Peer, QueryImageRequest) (QueryImageScript, bool) {
			if len(s.queries) == 0 {
				return QueryImageScript{}, false
			}
			q := s.queries[0]
			s.queries = s.queries[1:]
			return q, true
		}
		c.ScriptApplyUpdate = func(Peer, provdef.ApplyUpdateRequestRequest) (ApplyUpdateScript, bool) {
			if len(s.applies) == 0 {
				return ApplyUpdateScript{}, false
			}
			a := s.applies[0]
			s.applies = s.applies[1:]
			return a, true
		}
	})
	return s
}

func (s *scriptRig) commands(t *testing.T) *ProviderCommands {
	t.Helper()
	c := s.r.m.ProviderConfig().Commands
	if c == nil {
		t.Fatal("a manager with script hooks hands the node no command wrappers")
	}
	return c
}

// ownQuery is the provider's own answer: UpdateAvailable.
func (s *scriptRig) ownQuery(context.Context) (provdef.QueryImageResponse, error) {
	s.ownCalls++
	return provdef.QueryImageResponse{Status: provdef.StatusUpdateAvailable, SoftwareVersion: ptrTo[uint32](2)}, nil
}

// ownApply is the provider's own path as far as the policy port: the
// provider asks providerPorts.ApplyUpdate, as cluster/core's provider does.
func (s *scriptRig) ownApply(req provdef.ApplyUpdateRequestRequest) func(ctx context.Context) (provdef.ApplyUpdateResponse, error) {
	return func(ctx context.Context) (provdef.ApplyUpdateResponse, error) {
		s.ownCalls++
		action, delay, err := providerPorts{s.r.m}.ApplyUpdate(ctx, testOTAPeer, req)
		return provdef.ApplyUpdateResponse{Action: action, DelayedActionTime: uint32(delay / time.Second)}, err
	}
}

func TestScriptedProviderWithoutHooksIsUnwrapped(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	if c := r.m.ProviderConfig().Commands; c != nil {
		t.Fatalf("a manager without script hooks wraps the provider: %+v", c)
	}
}

// A scripted status is answered without asking the provider
// (InProcessControllerAdapter.ts:508-519). A scripted consent alone is laid
// over the provider's answer (withUserConsent, :768-773); a spent list
// leaves the answer to the provider.
func TestScriptedQueryImage(t *testing.T) {
	t.Parallel()
	s := newScriptRig(t)
	c := s.commands(t)
	ctx := context.Background()
	busy := provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: ptrTo[uint32](1)}
	s.queries = []QueryImageScript{{Response: &busy}, {UserConsentNeeded: ptrTo(true)}}
	req := provdef.QueryImageRequest{VendorId: 0xFFF1, ProductId: 0x8001, SoftwareVersion: 1}

	got, err := c.QueryImage(ctx, testOTAPeer, req, s.ownQuery)
	if err != nil || got.Status != provdef.StatusBusy || *got.DelayedActionTime != 1 || s.ownCalls != 0 {
		t.Fatalf("scripted status: %+v %v, provider asked %d times", got, err, s.ownCalls)
	}
	got, err = c.QueryImage(ctx, testOTAPeer, req, s.ownQuery)
	if err != nil || got.Status != provdef.StatusUpdateAvailable || got.UserConsentNeeded == nil || !*got.UserConsentNeeded || s.ownCalls != 1 {
		t.Fatalf("scripted consent: %+v %v, provider asked %d times", got, err, s.ownCalls)
	}
	got, err = c.QueryImage(ctx, testOTAPeer, req, s.ownQuery)
	if err != nil || got.Status != provdef.StatusUpdateAvailable || got.UserConsentNeeded != nil || s.ownCalls != 2 {
		t.Fatalf("spent script: %+v %v, provider asked %d times", got, err, s.ownCalls)
	}
	if len(s.exchanges) != 3 {
		t.Fatalf("%d exchanges recorded, want 3", len(s.exchanges))
	}
	for i, x := range s.exchanges {
		if x.QueryImage == nil || x.Peer != testPeer || x.QueryImage.Request.SoftwareVersion != 1 {
			t.Fatalf("exchange %d: %+v", i, x)
		}
	}
	if s.exchanges[0].QueryImage.Response.Status != provdef.StatusBusy {
		t.Fatalf("the record holds %+v, not the answer the requestor got", s.exchanges[0].QueryImage.Response)
	}
}

// AwaitNextAction is stated directly; Proceed runs the provider's path
// with the scripted delay in place of the configured one; Discontinue
// withdraws the consent and lets the provider refuse, two minutes, as it
// refuses an update without consent (InProcessControllerAdapter.ts:
// 575-623; OtaSoftwareUpdateProviderServer.ts:459-463).
func TestScriptedApplyUpdate(t *testing.T) {
	t.Parallel()
	s := newScriptRig(t)
	c := s.commands(t)
	ctx := context.Background()
	req := provdef.ApplyUpdateRequestRequest{UpdateToken: make([]byte, 8), NewVersion: 2}
	s.r.queue(2, nil)
	s.applies = []ApplyUpdateScript{
		{Action: provdef.ApplyUpdateActionAwaitNextAction, DelayedActionTime: ptrTo[uint32](60)},
		{Action: provdef.ApplyUpdateActionProceed, DelayedActionTime: ptrTo[uint32](180)},
		{Action: provdef.ApplyUpdateActionDiscontinue, DelayedActionTime: ptrTo[uint32](180)},
	}

	got, err := c.ApplyUpdate(ctx, testOTAPeer, req, s.ownApply(req))
	if err != nil || got != (provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionAwaitNextAction, DelayedActionTime: 60}) || s.ownCalls != 0 {
		t.Fatalf("AwaitNextAction: %+v %v, provider asked %d times", got, err, s.ownCalls)
	}
	got, err = c.ApplyUpdate(ctx, testOTAPeer, req, s.ownApply(req))
	if err != nil || got != (provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionProceed, DelayedActionTime: 180}) || s.ownCalls != 1 {
		t.Fatalf("Proceed: %+v %v, provider asked %d times", got, err, s.ownCalls)
	}
	if !s.r.m.HasConsent(testPeer, 2) {
		t.Fatal("the consent is gone before the Discontinue")
	}
	got, err = c.ApplyUpdate(ctx, testOTAPeer, req, s.ownApply(req))
	if err != nil || got != (provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionDiscontinue, DelayedActionTime: 120}) || s.ownCalls != 2 {
		t.Fatalf("Discontinue: %+v %v, provider asked %d times", got, err, s.ownCalls)
	}
	if s.r.m.HasConsent(testPeer, 2) {
		t.Fatal("a scripted Discontinue left the update's consent")
	}
	got, err = c.ApplyUpdate(ctx, testOTAPeer, req, s.ownApply(req))
	if err != nil || got != (provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionProceed, DelayedActionTime: 5}) {
		t.Fatalf("spent script: %+v %v, want the configured apply delay", got, err)
	}
	if len(s.exchanges) != 4 || s.exchanges[2].ApplyUpdate == nil || s.exchanges[2].ApplyUpdate.Response.Action != provdef.ApplyUpdateActionDiscontinue {
		t.Fatalf("exchanges %+v", s.exchanges)
	}
	if err := c.NotifyUpdateApplied(ctx, testOTAPeer, provdef.NotifyUpdateAppliedRequest{UpdateToken: req.UpdateToken, SoftwareVersion: 2}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if n := s.exchanges[len(s.exchanges)-1].NotifyUpdateApplied; n == nil || n.SoftwareVersion != 2 {
		t.Fatalf("NotifyUpdateApplied not recorded: %+v", s.exchanges[len(s.exchanges)-1])
	}
}
