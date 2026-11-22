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
// (support/chip-testing/src/cert/InProcessControllerAdapter.ts:628-814 at
// b25ed960, RecordingOtaProviderServer); a Go controller cannot subclass
// its provider, so the seam is [Config]'s ScriptQueryImage,
// ScriptApplyUpdate and OnProviderExchange, which [Manager.ProviderConfig]
// hands the controller as [ProviderCommands]. Without them the provider
// runs unwrapped.

// QueryImageScript is a scripted answer to one QueryImage — matter.js
// OtaScriptedQueryAnswer (packages/testing/src/chip/cert/
// controller-adapter.ts:560-582 at b25ed960).
type QueryImageScript struct {
	// Response, when set, is answered as it stands and the provider is not
	// asked: its answer stages an in-progress entry and registers the peer
	// for BDX, which a status stated over it afterwards would leave behind
	// (InProcessControllerAdapter.ts:681-694 at b25ed960). A scripted UpdateAvailable
	// names an image the provider does not hold; its fields are the
	// script's to fill (#unheldUpdate, InProcessControllerAdapter.ts:727-745 at
	// b25ed960).
	Response *provdef.QueryImageResponse
	// UserConsentNeeded, without Response, is laid over the provider's own
	// answer (withUserConsent, InProcessControllerAdapter.ts:944-949 at
	// b25ed960).
	UserConsentNeeded *bool
}

// ApplyUpdateScript is a scripted answer to one ApplyUpdateRequest —
// matter.js OtaScriptedApplyAnswer (controller-adapter.ts:597-610 at
// b25ed960). Each
// action reaches the requestor the way matter.js routes it
// (InProcessControllerAdapter.ts:747-799 at b25ed960):
//
//   - AwaitNextAction is stated directly, with DelayedActionTime (0 when
//     nil); the provider is not asked, so the requestor's next request
//     finds the transfer and the image it already downloaded.
//   - Discontinue withdraws the consent of the version the update token
//     applies ([Manager.RemoveConsent]), when the token names one, and the
//     provider answers by its apply table: a known token whose attempt does
//     not apply yet is refused, an applying one proceeds, an unknown token
//     with a version above 0 proceeds as a new attempt, version 0 under an
//     unknown token is refused; DelayedActionTime is not laid over it.
//   - Proceed runs the provider's own path with DelayedActionTime in place
//     of [Config].ApplyDelay; the provider still refuses an update it holds
//     no consent for, and the delay is not laid over that refusal.
type ApplyUpdateScript struct {
	Action            provdef.ApplyUpdateActionEnum
	DelayedActionTime *uint32
}

// ProviderExchange is one command the provider answered, with the answer —
// one entry of matter.js OtaProviderExchanges (controller-adapter.ts:481-485
// at b25ed960). Exactly one of the three exchange fields is set. A command the
// provider rejected is not reported: an answer is recorded once it was
// produced (InProcessControllerAdapter.ts:622-624 at b25ed960).
type ProviderExchange struct {
	Peer Peer
	// At is when the provider received the command, on the manager's
	// clock: only the difference between two exchanges means anything
	// (receivedAtMs, controller-adapter.ts:420-431 at b25ed960).
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
// (InProcessControllerAdapter.ts:676-719 at b25ed960).
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
// (InProcessControllerAdapter.ts:747-799 at b25ed960).
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
		// A scripted Discontinue removes the consent of the version the
		// token applies, when it names one, and the provider answers by its
		// apply table (F-b25ed960-C3).
		//
		// Mirrors matter.js support/chip-testing/src/cert/InProcessControllerAdapter.ts:765-772 at b25ed960
		// (applyUpdateRequest, the scripted Discontinue).
		if scripted && script.Action == provdef.ApplyUpdateActionDiscontinue {
			m.mu.Lock()
			version, ok := m.attempts.AppliedVersionOf(op, req.UpdateToken, req.NewVersion)
			m.mu.Unlock()
			if ok {
				m.RemoveConsent(p, version) //nolint:contextcheck // as matter.js, the consent change is not bound to the command
			}
		}
		if scripted && script.Action == provdef.ApplyUpdateActionProceed && script.DelayedActionTime != nil {
			ctx = context.WithValue(ctx, applyOverrideKey{}, applyOverride{delay: time.Duration(*script.DelayedActionTime) * time.Second, set: true})
		}
		var err error
		if resp, err = own(ctx); err != nil {
			return resp, err
		}
		// The provider answers with its own apply delay; a scripted
		// Proceed's delay, which the manager recorded with Applying,
		// replaces it (the scripted state.applyDelay,
		// InProcessControllerAdapter.ts:775-789 at b25ed960).
		if scripted && script.Action == provdef.ApplyUpdateActionProceed && script.DelayedActionTime != nil &&
			resp.Action == provdef.ApplyUpdateActionProceed {
			resp.DelayedActionTime = min(*script.DelayedActionTime, uint32(maxApplyDelay/time.Second))
		}
	}
	m.record(ProviderExchange{Peer: p, At: at, ApplyUpdate: &ApplyUpdateExchange{Request: req, Response: resp}})
	return resp, nil
}

// observedNotifyApplied is RecordingOtaProviderServer.notifyUpdateApplied
// (InProcessControllerAdapter.ts:801-813 at b25ed960).
func (m *Manager) observedNotifyApplied(ctx context.Context, op mattercore.OTAPeer, req provdef.NotifyUpdateAppliedRequest, own func(context.Context) error) error {
	at := m.clock.Now()
	if err := own(ctx); err != nil {
		return err
	}
	r := req
	m.record(ProviderExchange{Peer: peerOf(op), At: at, NotifyUpdateApplied: &r})
	return nil
}

// applyOverride is what a scripted ApplyUpdateRequest asks of the
// manager's apply ([providerPorts.ApplyUpdate]): a delay in place of
// [Config].ApplyDelay (the scripted Proceed's state.applyDelay,
// InProcessControllerAdapter.ts:775-789 at b25ed960).
type applyOverride struct {
	delay time.Duration
	set   bool
}

type applyOverrideKey struct{}

func applyOverrideFrom(ctx context.Context) (applyOverride, bool) {
	o, ok := ctx.Value(applyOverrideKey{}).(applyOverride)
	return o, ok
}

// wholeSeconds is a delay as the response carries it: whole seconds rounded
// up, at most a day (OtaSoftwareUpdateProviderServer.ts:346-351 at
// b25ed960).
func wholeSeconds(d time.Duration) time.Duration {
	return min(maxApplyDelay, max(0, time.Duration(math.Ceil(d.Seconds()))*time.Second))
}
