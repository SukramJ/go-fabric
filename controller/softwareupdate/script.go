// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"math"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

// The scripted provider: a test harness has the controller's OTA provider
// answer a command as a certification case needs, and reads back what it
// answered. matter.js keeps both in its cert harness, as the provider
// behavior it installs on the controller's node
// (support/chip-testing/src/cert/InProcessControllerAdapter.ts:456-638 at
// 862ad75f, RecordingOtaProviderServer); a Go controller cannot subclass
// its provider, so the seam is [Config]'s ScriptQueryImage,
// ScriptApplyUpdate and OnProviderExchange, which [Manager.ProviderConfig]
// hands the controller as [ProviderCommands]. Without them the provider
// runs unwrapped.

// QueryImageScript is a scripted answer to one QueryImage — matter.js
// OtaScriptedQueryAnswer (packages/testing/src/chip/cert/
// controller-adapter.ts:559-581 at 862ad75f).
type QueryImageScript struct {
	// Response, when set, is answered as it stands and the provider is not
	// asked: its answer stages an in-progress entry and registers the peer
	// for BDX, which a status stated over it afterwards would leave behind
	// (InProcessControllerAdapter.ts:508-519). A scripted UpdateAvailable
	// names an image the provider does not hold; its fields are the
	// script's to fill (#unheldUpdate, :552-573).
	Response *provdef.QueryImageResponse
	// UserConsentNeeded, without Response, is laid over the provider's own
	// answer (withUserConsent, :768-773).
	UserConsentNeeded *bool
}

// ApplyUpdateScript is a scripted answer to one ApplyUpdateRequest —
// matter.js OtaScriptedApplyAnswer (controller-adapter.ts:596-609). Each
// action reaches the requestor the way matter.js routes it
// (InProcessControllerAdapter.ts:575-623):
//
//   - AwaitNextAction is stated directly, with DelayedActionTime (0 when
//     nil); the provider is not asked, so the requestor's next request
//     finds the transfer and the image it already downloaded.
//   - Discontinue withdraws the update's consent ([Manager.RemoveConsent])
//     and lets the provider refuse for itself, so the state it is left in
//     agrees with the answer; DelayedActionTime is not laid over it.
//   - Proceed runs the provider's own path with DelayedActionTime in place
//     of [Config].ApplyDelay; the provider still refuses an update it holds
//     no consent for, and the delay is not laid over that refusal.
type ApplyUpdateScript struct {
	Action            provdef.ApplyUpdateActionEnum
	DelayedActionTime *uint32
}

// ProviderExchange is one command the provider answered, with the answer —
// one entry of matter.js OtaProviderExchanges (controller-adapter.ts:
// 480-484). Exactly one of the three exchange fields is set. A command the
// provider rejected is not reported: an answer is recorded once it was
// produced (InProcessControllerAdapter.ts:450-452).
type ProviderExchange struct {
	Peer Peer
	// At is when the provider received the command, on the manager's
	// clock: only the difference between two exchanges means anything
	// (receivedAtMs, controller-adapter.ts:420-431).
	At                  time.Time
	QueryImage          *QueryImageExchange
	ApplyUpdate         *ApplyUpdateExchange
	NotifyUpdateApplied *provdef.NotifyUpdateAppliedRequest
}

// QueryImageExchange is a QueryImage and the answer the provider gave.
type QueryImageExchange struct {
	Request  QueryImageRequest
	Response provdef.QueryImageResponse
}

// ApplyUpdateExchange is an ApplyUpdateRequest and the answer the provider
// gave.
type ApplyUpdateExchange struct {
	Request  provdef.ApplyUpdateRequestRequest
	Response provdef.ApplyUpdateResponse
}

// ProviderCommands wraps the provider's command handlers: each gets the
// requestor, the request and the provider's own handler, and answers in
// its place. The controller installs it on its provider
// (controller/ota_provider.go wrapProviderCommands).
type ProviderCommands struct {
	QueryImage          func(ctx context.Context, p mattercore.OTAPeer, req provdef.QueryImageRequest, own func(context.Context) (provdef.QueryImageResponse, error)) (provdef.QueryImageResponse, error)
	ApplyUpdate         func(ctx context.Context, p mattercore.OTAPeer, req provdef.ApplyUpdateRequestRequest, own func(context.Context) (provdef.ApplyUpdateResponse, error)) (provdef.ApplyUpdateResponse, error)
	NotifyUpdateApplied func(ctx context.Context, p mattercore.OTAPeer, req provdef.NotifyUpdateAppliedRequest, own func(context.Context) error) error
}

// providerCommands is the wrapper of [Config]'s script and observer, nil
// without either.
func (m *Manager) providerCommands() *ProviderCommands {
	if m.cfg.ScriptQueryImage == nil && m.cfg.ScriptApplyUpdate == nil && m.cfg.OnProviderExchange == nil {
		return nil
	}
	return &ProviderCommands{QueryImage: m.scriptedQueryImage, ApplyUpdate: m.scriptedApplyUpdate, NotifyUpdateApplied: m.observedNotifyApplied}
}

func (m *Manager) record(x ProviderExchange) {
	if fn := m.cfg.OnProviderExchange; fn != nil {
		fn(x)
	}
}

// scriptedQueryImage is RecordingOtaProviderServer.queryImage
// (InProcessControllerAdapter.ts:504-549).
func (m *Manager) scriptedQueryImage(ctx context.Context, op mattercore.OTAPeer, req provdef.QueryImageRequest, own func(context.Context) (provdef.QueryImageResponse, error)) (provdef.QueryImageResponse, error) {
	at := m.clock.Now()
	p := peerOf(op)
	var script QueryImageScript
	scripted := false
	if fn := m.cfg.ScriptQueryImage; fn != nil {
		script, scripted = fn(p, req)
	}
	var resp provdef.QueryImageResponse
	if scripted && script.Response != nil {
		resp = *script.Response
	} else {
		var err error
		if resp, err = own(ctx); err != nil {
			return resp, err
		}
		if scripted && script.UserConsentNeeded != nil {
			v := *script.UserConsentNeeded
			resp.UserConsentNeeded = &v
		}
	}
	m.record(ProviderExchange{Peer: p, At: at, QueryImage: &QueryImageExchange{Request: req, Response: resp}})
	return resp, nil
}

// scriptedApplyUpdate is RecordingOtaProviderServer.applyUpdateRequest
// (InProcessControllerAdapter.ts:575-623).
func (m *Manager) scriptedApplyUpdate(ctx context.Context, op mattercore.OTAPeer, req provdef.ApplyUpdateRequestRequest, own func(context.Context) (provdef.ApplyUpdateResponse, error)) (provdef.ApplyUpdateResponse, error) {
	at := m.clock.Now()
	p := peerOf(op)
	var script ApplyUpdateScript
	scripted := false
	if fn := m.cfg.ScriptApplyUpdate; fn != nil {
		script, scripted = fn(p, req)
	}
	var resp provdef.ApplyUpdateResponse
	switch {
	case scripted && script.Action == provdef.ApplyUpdateActionAwaitNextAction:
		resp = provdef.ApplyUpdateResponse{Action: script.Action}
		if script.DelayedActionTime != nil {
			resp.DelayedActionTime = *script.DelayedActionTime
		}
	default:
		if scripted && script.Action == provdef.ApplyUpdateActionDiscontinue {
			m.RemoveConsent(p, req.NewVersion) //nolint:contextcheck // as matter.js, the consent change is not bound to the command
			ctx = context.WithValue(ctx, applyOverrideKey{}, applyOverride{refuse: true})
		}
		if scripted && script.Action == provdef.ApplyUpdateActionProceed && script.DelayedActionTime != nil {
			ctx = context.WithValue(ctx, applyOverrideKey{}, applyOverride{delay: time.Duration(*script.DelayedActionTime) * time.Second, set: true})
		}
		var err error
		if resp, err = own(ctx); err != nil {
			return resp, err
		}
	}
	m.record(ProviderExchange{Peer: p, At: at, ApplyUpdate: &ApplyUpdateExchange{Request: req, Response: resp}})
	return resp, nil
}

// observedNotifyApplied is RecordingOtaProviderServer.notifyUpdateApplied
// (InProcessControllerAdapter.ts:625-637).
func (m *Manager) observedNotifyApplied(ctx context.Context, op mattercore.OTAPeer, req provdef.NotifyUpdateAppliedRequest, own func(context.Context) error) error {
	at := m.clock.Now()
	if err := own(ctx); err != nil {
		return err
	}
	r := req
	m.record(ProviderExchange{Peer: peerOf(op), At: at, NotifyUpdateApplied: &r})
	return nil
}

// applyOverride is what a scripted ApplyUpdateRequest asks of the apply
// policy the provider consults ([providerPorts.ApplyUpdate]): a delay in
// place of [Config].ApplyDelay (the scripted Proceed's state.applyDelay,
// InProcessControllerAdapter.ts:596-614), or the refusal the provider
// gives an update without consent (the scripted Discontinue, :590-594).
type applyOverride struct {
	delay  time.Duration
	set    bool
	refuse bool
}

type applyOverrideKey struct{}

func applyOverrideFrom(ctx context.Context) (applyOverride, bool) {
	o, ok := ctx.Value(applyOverrideKey{}).(applyOverride)
	return o, ok
}

// applyRefusalDelay is the delay the provider's own refusal names: two
// minutes (OtaSoftwareUpdateProviderServer.ts:459-463).
const applyRefusalDelay = 2 * time.Minute

// wholeSeconds is a delay as the response carries it: whole seconds rounded
// up, at most a day (OtaSoftwareUpdateProviderServer.ts:470-473).
func wholeSeconds(d time.Duration) time.Duration {
	return min(maxApplyDelay, max(0, time.Duration(math.Ceil(d.Seconds()))*time.Second))
}
