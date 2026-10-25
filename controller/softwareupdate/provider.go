// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"math"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/node"
)

// The provider side: the manager as the ports of node/'s OTA provider
// (cluster/core.OTAProvider) — what matter.js's
// OtaSoftwareUpdateProviderServer asks its SoftwareUpdateManager
// (packages/node/src/behaviors/ota-software-update-provider/OtaSoftwareUpdateProviderServer.ts
// at 862ad75f): checkUpdateAvailable → updateExistsFor (:519-528), the
// consent hook (:507-515), the apply delay (:466-482), the status
// reports (#updateInProgressDetails → onOtaStatusChange, :599-645), the
// startUp suppression (:221-224) and the transfer options (:293-298).

// ProviderConfig is the provider configuration that serves the manager's
// updates: a node.Config's OTAProvider. The controller mounts it on its
// provider node (controller.Config.OTA).
func (m *Manager) ProviderConfig() *node.OTAProviderConfig {
	return &node.OTAProviderConfig{
		ImageSource:       providerPorts{m},
		Consent:           providerPorts{m},
		ApplyPolicy:       providerPorts{m},
		BDXMaxBlockSize:   m.cfg.MaxBDXBlockSize,
		OnStatusChange:    m.providerStatus,
		RebootQuery:       func(p node.OTAPeer, sessionID uint16) { m.SuppressNextStartUp(peerOf(p), sessionID) },
		TransferBlockSize: func(p node.OTAPeer) uint16 { return m.TransferOptionsFor(peerOf(p)).MaxBlockSize },
	}
}

// SetBDX gives the manager the provider's BDX responder, once the
// provider node is built (node.Node.BDX).
func (m *Manager) SetBDX(r *bdx.Responder) {
	m.mu.Lock()
	m.cfg.BDX = r
	m.mu.Unlock()
}

func peerOf(p mattercore.OTAPeer) Peer { return Peer{FabricIndex: p.FabricIndex, NodeID: p.NodeID} }

type providerPorts struct{ m *Manager }

// QueryImage implements mattercore.OTAImageSource: checkUpdateAvailable
// (OtaSoftwareUpdateProviderServer.ts:519-528) over [Manager.UpdateExistsFor].
func (s providerPorts) QueryImage(ctx context.Context, p mattercore.OTAPeer, req provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	d, ok := s.m.UpdateExistsFor(ctx, peerOf(p), req)
	if !ok {
		return mattercore.OTAImage{}, false, nil
	}
	return mattercore.OTAImage{
		FileDesignator: bdx.FileDesignator(d.Filename), SoftwareVersion: d.NewSoftwareVersion,
		SoftwareVersionString: d.NewSoftwareVersionString, ConsentRequired: d.ConsentRequired,
	}, true, nil
}

// Open implements mattercore.OTAImageSource: the stored image.
func (s providerPorts) Open(ctx context.Context, file string) (bdx.Blob, error) {
	return s.m.cfg.Images.Open(ctx, file)
}

// RequestConsent implements mattercore.OTAConsent:
// requestUserConsentForUpdate (:507-515), Unknown without a hook.
func (s providerPorts) RequestConsent(ctx context.Context, p mattercore.OTAPeer, req provdef.QueryImageRequest, img mattercore.OTAImage) (mattercore.OTAConsentState, time.Duration, error) {
	if s.m.cfg.RequestConsent == nil {
		return mattercore.OTAConsentUnknown, 0, nil
	}
	d := AvailableDetails{
		NewSoftwareVersion: img.SoftwareVersion, NewSoftwareVersionString: img.SoftwareVersionString,
		Filename: string(img.FileDesignator), ConsentRequired: img.ConsentRequired,
	}
	state, delay := s.m.cfg.RequestConsent(ctx, peerOf(p), req, d)
	return state, delay, nil
}

// ApplyUpdate implements mattercore.OTAApplyPolicy: Proceed after the
// apply delay, in whole seconds rounded up and at most a day
// (:466-482); the manager arms its restart window with what it sent.
func (s providerPorts) ApplyUpdate(_ context.Context, p mattercore.OTAPeer, _ provdef.ApplyUpdateRequestRequest) (provdef.ApplyUpdateActionEnum, time.Duration, error) {
	delay := min(maxApplyDelay, max(0, time.Duration(math.Ceil(s.m.cfg.ApplyDelay.Seconds()))*time.Second))
	s.m.mu.Lock()
	s.m.applyDelays[peerOf(p)] = delay
	s.m.mu.Unlock()
	return provdef.ApplyUpdateActionProceed, delay, nil
}

// providerStatus takes the provider's status report (:599-645).
func (m *Manager) providerStatus(p mattercore.OTAPeer, status mattercore.OTAUpdateStatus, version uint32) {
	peer := peerOf(p)
	var delay time.Duration
	if status == mattercore.OTAUpdateApplying {
		m.mu.Lock()
		delay = m.applyDelays[peer]
		m.mu.Unlock()
	}
	var v *uint32
	if version != 0 {
		v = &version
	}
	m.OnStatusChange(peer, status, v, delay)
}
