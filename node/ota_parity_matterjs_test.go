// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/ota/OtaTest.ts at
// 862ad75f: the requestor's cases, on two devices of one site.
//
// matter.js hosts the OTA provider endpoint and its SoftwareUpdateManager
// on the controller and the requestor on the device (initOtaSite,
// ota-utils.ts:202-239). The Go provider is a node's
// (node.Config.OTAProvider), so the port commissions two fabrictest
// devices onto one controller (otaSite): the provider device, whose
// OTAImageSource holds the test image (addTestOtaImage, ota-utils.ts
// :169-199: 50 KiB, the requestor's version + 1) and whose OnStatusChange
// records the update's states, and the requestor device, whose OtaApplier
// records what applyUpdate got (InstrumentedOtaRequestorServer,
// ota-utils.ts:271-345). The manager's two moves are the controller's:
// announceAsDefaultProvider is a DefaultOTAProviders write naming the
// provider device, forceUpdate the AnnounceOTAProvider command it sends
// (reason UpdateAvailable). The image travels over BDX on the site. The
// requestor's StateTransition events are the node's event log, its stored
// update the store's requestor state (ota-utils.ts storageFor, the key
// updateInProgressDetails). A device restart into a version is
// Device.Stop, the version the next start's BasicInformation carries, and
// Device.Start on the same store (restartDeviceFromStorage,
// ota-utils.ts:246-269).
package node_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// otaTestFile is the test image's file designator.
const otaTestFile = "fff1-8001-v1.ota"

// otaSource is the provider's image catalogue: the test image for a
// requestor below its version (checkUpdateAvailable, ota-utils.ts
// :483-497, which defers to the provider's default check), or none.
type otaSource struct {
	mu      sync.Mutex
	img     mattercore.OTAImage
	bytes   []byte
	none    bool
	queries int
	// hold, when set, keeps a query until it is closed (a provider whose
	// queryImage blocks).
	hold chan struct{}
	// arrived is closed by the first query.
	arrived chan struct{}
}

func (s *otaSource) QueryImage(ctx context.Context, _ mattercore.OTAPeer, req provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	s.mu.Lock()
	s.queries++
	hold, arrived := s.hold, s.arrived
	if arrived != nil {
		s.arrived = nil
	}
	img, none := s.img, s.none
	s.mu.Unlock()
	if arrived != nil {
		close(arrived)
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return mattercore.OTAImage{}, false, ctx.Err()
		}
	}
	if none || req.SoftwareVersion >= img.SoftwareVersion {
		return mattercore.OTAImage{}, false, nil
	}
	return img, true, nil
}

func (s *otaSource) Open(_ context.Context, file string) (bdx.Blob, error) {
	if file != otaTestFile {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(s.bytes), nil
}

func (s *otaSource) queryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queries
}

// otaApplied is one applyUpdate the requestor's applier got.
type otaApplied struct {
	version uint32
	image   []byte
}

// otaRecApplier is the requestor's OtaApplier: it records the version and
// the image's bytes (InstrumentedOtaRequestorServer.applyUpdate,
// ota-utils.ts:290-320) and runs during, when set — a failure, a slow
// apply.
type otaRecApplier struct {
	mu      sync.Mutex
	applied []otaApplied
	during  func(ctx context.Context, file string) error
}

func (a *otaRecApplier) RequestUserConsent(context.Context, uint32, string) (bool, error) {
	return false, nil
}

func (a *otaRecApplier) ApplyUpdate(ctx context.Context, version uint32, file string) error {
	b, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.applied = append(a.applied, otaApplied{version: version, image: b})
	during := a.during
	a.mu.Unlock()
	if during != nil {
		return during(ctx, file)
	}
	return nil
}

func (a *otaRecApplier) applies() []otaApplied {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.applied)
}

// otaStatuses records the provider's update states.
type otaStatuses struct {
	mu  sync.Mutex
	got []mattercore.OTAUpdateStatus
}

func (l *otaStatuses) add(_ mattercore.OTAPeer, s mattercore.OTAUpdateStatus, _ uint32) {
	l.mu.Lock()
	l.got = append(l.got, s)
	l.mu.Unlock()
}

func (l *otaStatuses) all() []mattercore.OTAUpdateStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.got)
}

// otaSite is initOtaSite with addTestOtaImage.
type otaSite struct {
	site       *fabrictest.Site
	c          *fabrictest.Controller
	prov, req  *fabrictest.Device
	reqPair    *fabrictest.Pair
	provEP     uint16
	provNodeID uint64
	reqFabric  uint8
	src        *otaSource
	applier    *otaRecApplier
	statuses   *otaStatuses
	// version is the SoftwareVersion the requestor starts with.
	version atomic.Uint32
	// consent, when set, is the provider's consent port.
	consent mattercore.OTAConsent
}

// otaSiteOptions shapes an otaSite.
type otaSiteOptions struct {
	// consent is the provider's consent port; the image then needs a
	// consent.
	consent mattercore.OTAConsent
	// prepare edits the source before the devices start.
	prepare func(*otaSource)
}

// newOTASite is initOtaSite (ota-utils.ts:202-239) and addTestOtaImage
// (:169-199): the provider and the requestor (version 0) commissioned onto
// one controller, the requestor's DefaultOTAProviders naming the provider.
func newOTASite(t *testing.T, o otaSiteOptions) *otaSite {
	t.Helper()
	s := &otaSite{site: steppedSite(t), applier: &otaRecApplier{}, statuses: &otaStatuses{}, consent: o.consent}
	payload := make([]byte, 50*1024)
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a seeded payload, generateTestPayload
	for i := range payload {
		payload[i] = byte(rng.UintN(256))
	}
	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 1, SoftwareVersionString: "1", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.src = &otaSource{
		img:   mattercore.OTAImage{FileDesignator: bdx.FileDesignator(otaTestFile), SoftwareVersion: 1, SoftwareVersionString: "1", ConsentRequired: o.consent != nil},
		bytes: image,
	}
	if o.prepare != nil {
		o.prepare(s.src)
	}
	s.c = s.site.AddController(fabrictest.ControllerOptions{})
	s.prov = s.site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.OTAProvider = &node.OTAProviderConfig{ImageSource: s.src, Consent: s.consent, OnStatusChange: s.statuses.add}
	}})
	dir := filepath.Join(t.TempDir(), "ota")
	s.req = s.site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
		v := s.version.Load()
		// The string is derived from the number (BasicInformationServer.ts:71);
		// a "0" would be read as a build string and floored at 1.
		cfg.BasicInformation.SoftwareVersion, cfg.BasicInformation.SoftwareVersionString = v, ""
		cfg.OTA = &node.OtaConfig{Dir: dir, Applier: s.applier}
	}})
	// The requestor was built at version 1 by AddDevice's start; it runs
	// version 0, as the matter.js device does (BasicInformation default).
	if err := s.req.Restart(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	pp, err := s.c.Commission(parityCtx(t), s.prov, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s.reqPair, err = s.c.Commission(parityCtx(t), s.req, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	s.provNodeID = pp.Address.NodeID
	ep := providerEndpoint(t, s.prov.Node())
	if ep == nil {
		t.Fatal("the provider device has no provider endpoint")
	}
	s.provEP = ep.ID
	fabrics, err := s.req.Node().FabricInfos(parityCtx(t))
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("requestor fabrics %v, %v", fabrics, err)
	}
	s.reqFabric = fabrics[0].FabricIndex
	// announceAsDefaultProvider: the provider in DefaultOTAProviders.
	if err := fabrictest.Write(parityCtx(t), s.reqPair, im.ConcreteAttributePath{
		Endpoint: 0, Cluster: otadef.ClusterID, Attribute: otadef.AttrDefaultOtaProviders, HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}, spec.List[otadef.ProviderLocation]{{ProviderNodeId: s.provNodeID, Endpoint: s.provEP, FabricIndex: s.reqFabric}}); err != nil {
		t.Fatalf("DefaultOTAProviders: %v", err)
	}
	return s
}

// requestor is the requestor device's OTA requestor.
func (s *otaSite) requestor() *mattercore.OTASoftwareUpdateRequestor {
	return s.req.Node().Root().OTARequestor
}

// announce is forceUpdate's announcement: AnnounceOTAProvider naming the
// provider, reason reason.
func (s *otaSite) announce(t *testing.T, reason otadef.AnnouncementReasonEnum) error {
	t.Helper()
	_, err := fabrictest.Invoke(parityCtx(t), s.reqPair, im.ConcreteCommandPath{Endpoint: 0, Cluster: otadef.ClusterID, Command: otadef.CmdAnnounceOtaProvider},
		otadef.AnnounceOtaProviderRequest{ProviderNodeId: s.provNodeID, VendorId: 0xFFF1, AnnouncementReason: reason, Endpoint: s.provEP})
	return err
}

// transitions are the requestor's StateTransition events of its current
// run.
func (s *otaSite) transitions() []otadef.StateTransitionEvent {
	var out []otadef.StateTransitionEvent
	for _, rec := range s.req.Bridge().EventLog().Query(0, otadef.ClusterID, otadef.EventStateTransition, 0) {
		if ev, ok := rec.Payload.(otadef.StateTransitionEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}

// versionsApplied are the requestor's VersionApplied events of its run.
func (s *otaSite) versionsApplied() []uint32 {
	var out []uint32
	for _, rec := range s.req.Bridge().EventLog().Query(0, otadef.ClusterID, otadef.EventVersionApplied, 0) {
		if ev, ok := rec.Payload.(otadef.VersionAppliedEvent); ok {
			out = append(out, ev.SoftwareVersion)
		}
	}
	return out
}

// updateState is the requestor's UpdateState.
func (s *otaSite) updateState() otadef.UpdateStateEnum {
	v, _ := s.requestor().MatterRead(otadef.AttrUpdateState)
	if u, ok := v.(uint8); ok {
		return otadef.UpdateStateEnum(u)
	}
	if u, ok := v.(otadef.UpdateStateEnum); ok {
		return u
	}
	return 0xFF
}

// storedInProgress reports whether the requestor's stored state holds an
// update in progress (storageFor(device)[…].updateInProgressDetails).
func (s *otaSite) storedInProgress(t *testing.T, d *fabrictest.Device) bool {
	t.Helper()
	b, ok, err := d.Store().LoadOTARequestorState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return false
	}
	var st map[string]json.RawMessage
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	v, has := st["updateInProgressDetails"]
	return has && string(v) != "null"
}

// otaStep is the step the OTA cases move the site's fake clock by. The
// requestor's waits are minutes long (an announcement's query 1-600 s
// later, the minimum query interval) and the nodes keep timers of a few
// hundred milliseconds running, so stepping deadline by deadline
// (Site.Eventually) would take minutes of wall time; a second at a time,
// settling after each, every exchange still completes within its step —
// matter.js's MockTime.advance stepping the timers in order.
const otaStep = time.Second

// eventually waits for cond, moving the site's clock a step at a time
// (Site.Step: quiet, advance, quiet).
func (s *otaSite) eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx := parityCtx(t)
	if err := s.site.Settle(ctx); err != nil {
		t.Fatalf("waiting for %s: %v", what, err)
	}
	for !cond() {
		if err := s.site.Step(ctx, otaStep); err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
	}
}

// advance moves the site's clock by d a step at a time (Site.Step). The
// first step waits for the site to be quiet before it moves the clock: a
// provider the test just released answers before time passes (the
// TestOtaParityMatterJS flake, brief CG §3).
func (s *otaSite) advance(t *testing.T, d time.Duration) {
	t.Helper()
	ctx := parityCtx(t)
	for ; d > 0; d -= otaStep {
		if err := s.site.Step(ctx, min(d, otaStep)); err != nil {
			t.Fatal(err)
		}
	}
}

// restartInto is restartDeviceFromStorage: the requestor stopped and
// started again on its store at version.
func (s *otaSite) restartInto(t *testing.T, version uint32) {
	t.Helper()
	if err := s.site.Run(parityCtx(t), s.req.Stop); err != nil {
		t.Fatal(err)
	}
	s.version.Store(version)
	if err := s.site.Run(parityCtx(t), s.req.Start); err != nil {
		t.Fatal(err)
	}
}

type otaTransition struct {
	from, to otadef.UpdateStateEnum
	reason   otadef.ChangeReasonEnum
}

func transitionsOf(evs []otadef.StateTransitionEvent) []otaTransition {
	out := make([]otaTransition, 0, len(evs))
	for _, e := range evs {
		out = append(out, otaTransition{e.PreviousState, e.NewState, e.Reason})
	}
	return out
}

var errSimulatedApply = errors.New("simulated apply failure")

// TestOtaParityMatterJS ports the requestor's cases of the describe "Ota"
// (OtaTest.ts:60-2819).
func TestOtaParityMatterJS(t *testing.T) {
	holdSite(t)

	// :91-219.
	t.Run("Successfully processes a software update", func(t *testing.T) {
		s := newOTASite(t, otaSiteOptions{})
		if v, _ := s.requestor().MatterRead(otadef.AttrDefaultOtaProviders); v == nil {
			t.Fatal("no DefaultOTAProviders")
		}
		if err := s.announce(t, otadef.AnnouncementReasonUpdateAvailable); err != nil {
			t.Fatalf("AnnounceOTAProvider: %v", err)
		}
		s.eventually(t, "applyUpdate", func() bool { return len(s.applier.applies()) == 1 })
		a := s.applier.applies()[0]
		if a.version != 1 || !bytes.Equal(a.image, s.src.bytes) {
			t.Fatalf("applyUpdate version %d, %d bytes; want 1 and the test image", a.version, len(a.image))
		}
		s.eventually(t, "Applying → Idle", func() bool { return len(s.transitions()) == 4 })
		before := s.transitions()
		s.restartInto(t, 1)
		s.eventually(t, "NotifyUpdateApplied", func() bool { return slices.Contains(s.statuses.all(), mattercore.OTAUpdateDone) })
		want := []otaTransition{
			{otadef.UpdateStateIdle, otadef.UpdateStateQuerying, otadef.ChangeReasonSuccess},
			{otadef.UpdateStateQuerying, otadef.UpdateStateDownloading, otadef.ChangeReasonSuccess},
			{otadef.UpdateStateDownloading, otadef.UpdateStateApplying, otadef.ChangeReasonSuccess},
			{otadef.UpdateStateApplying, otadef.UpdateStateIdle, otadef.ChangeReasonSuccess},
		}
		if got := transitionsOf(before); !slices.Equal(got, want) {
			t.Fatalf("transitions %v, want %v", got, want)
		}
		targets := []bool{true, false, false, true} // targetSoftwareVersion null, 1, 1, null
		for i, e := range before {
			if e.TargetSoftwareVersion.Null != targets[i] || (!targets[i] && e.TargetSoftwareVersion.Value != 1) {
				t.Errorf("transition %d target %+v", i, e.TargetSoftwareVersion)
			}
		}
	})

	// applyUpdateThenRestart (:221-307): the update to its apply, the
	// requestor's Applying → Idle, then a restart into restartVersion.
	applyThenRestart := func(t *testing.T, restartVersion func(target, previous uint32) uint32) (s *otaSite, persisted bool) {
		t.Helper()
		s = newOTASite(t, otaSiteOptions{})
		s.applier.during = func(context.Context, string) error {
			persisted = s.storedInProgress(t, s.req)
			return nil
		}
		if err := s.announce(t, otadef.AnnouncementReasonUpdateAvailable); err != nil {
			t.Fatal(err)
		}
		s.eventually(t, "the apply", func() bool {
			tr := s.transitions()
			return len(tr) > 0 && tr[len(tr)-1].PreviousState == otadef.UpdateStateApplying
		})
		s.restartInto(t, restartVersion(1, 0))
		return s, persisted
	}

	// :309-357. The controller's outcome after five minutes is
	// SoftwareUpdateManager's (the register names it); the provider's Done
	// is its counterpart here.
	t.Run("reports the update applied once restarted from storage into the new version", func(t *testing.T) {
		s, persisted := applyThenRestart(t, func(target, _ uint32) uint32 { return target })
		if !persisted {
			t.Error("the update in progress had not reached storage during the apply")
		}
		s.eventually(t, "NotifyUpdateApplied", func() bool { return slices.Contains(s.statuses.all(), mattercore.OTAUpdateDone) })
		if got := transitionsOf(s.transitions()); !slices.Equal(got, []otaTransition{{otadef.UpdateStateApplying, otadef.UpdateStateIdle, otadef.ChangeReasonSuccess}}) {
			t.Errorf("transitions %v, want Applying → Idle (Success)", got)
		}
		if got := s.versionsApplied(); !slices.Equal(got, []uint32{1}) {
			t.Errorf("VersionApplied %v, want [1]", got)
		}
		// The record is cleared after the outcome is reported, as matter.js
		// sets updateInProgressDetails to null once #handlePreviousUpdateOnStart
		// returned (OtaSoftwareUpdateRequestorServer.ts:241-244): wait for it.
		s.eventually(t, "the update in progress cleared from storage", func() bool { return !s.storedInProgress(t, s.req) })
	})

	// :359-383.
	t.Run("reports a failed update once restarted from storage into the previous version", func(t *testing.T) {
		s, _ := applyThenRestart(t, func(_, previous uint32) uint32 { return previous })
		s.eventually(t, "Applying → Idle", func() bool { return len(s.transitions()) > 0 })
		if got := transitionsOf(s.transitions()); !slices.Equal(got, []otaTransition{{otadef.UpdateStateApplying, otadef.UpdateStateIdle, otadef.ChangeReasonFailure}}) {
			t.Errorf("transitions %v, want Applying → Idle (Failure)", got)
		}
		if got := s.versionsApplied(); len(got) != 0 {
			t.Errorf("VersionApplied %v, want none", got)
		}
		if slices.Contains(s.statuses.all(), mattercore.OTAUpdateDone) {
			t.Error("the provider was told the update applied")
		}
		// The record is cleared after the outcome is reported, as matter.js
		// sets updateInProgressDetails to null once #handlePreviousUpdateOnStart
		// returned (OtaSoftwareUpdateRequestorServer.ts:241-244): wait for it.
		s.eventually(t, "the update in progress cleared from storage", func() bool { return !s.storedInProgress(t, s.req) })
	})

	// :385-458.
	t.Run("returns to idle, forgets the update and asks the provider again on the next announcement when applying it fails", func(t *testing.T) {
		s := newOTASite(t, otaSiteOptions{})
		var storedDuring bool
		s.applier.during = func(_ context.Context, file string) error {
			storedDuring = s.storedInProgress(t, s.req)
			_ = os.Remove(file)
			return errSimulatedApply
		}
		if err := s.announce(t, otadef.AnnouncementReasonUpdateAvailable); err != nil {
			t.Fatal(err)
		}
		s.eventually(t, "Applying → Idle", func() bool {
			tr := s.transitions()
			return len(tr) > 0 && tr[len(tr)-1].PreviousState == otadef.UpdateStateApplying
		})
		tr := s.transitions()
		if last := tr[len(tr)-1]; last.NewState != otadef.UpdateStateIdle || last.Reason != otadef.ChangeReasonFailure {
			t.Errorf("last transition %+v, want Applying → Idle (Failure)", last)
		}
		if !storedDuring {
			t.Error("the update in progress was not stored during the apply")
		}
		if s.storedInProgress(t, s.req) {
			t.Error("the update in progress stayed stored")
		}
		mark := len(tr)
		queries := s.src.queryCount()
		if err := s.announce(t, otadef.AnnouncementReasonUpdateAvailable); err != nil {
			t.Fatal(err)
		}
		s.eventually(t, "the next query", func() bool { return len(s.transitions()) > mark })
		if got := s.transitions()[mark]; got.NewState != otadef.UpdateStateQuerying {
			t.Errorf("after the failure %+v, want Querying", got)
		}
		s.eventually(t, "the provider asked again", func() bool { return s.src.queryCount() > queries })
	})

	// :460-531.
	t.Run("does not query while an update is being applied", func(t *testing.T) {
		s := newOTASite(t, otaSiteOptions{})
		release := make(chan struct{})
		s.applier.during = func(ctx context.Context, file string) error {
			_ = os.Remove(file)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		}
		if err := s.announce(t, otadef.AnnouncementReasonUpdateAvailable); err != nil {
			t.Fatal(err)
		}
		s.eventually(t, "applyUpdate", func() bool { return len(s.applier.applies()) == 1 })
		if err := s.announce(t, otadef.AnnouncementReasonUpdateAvailable); err != nil {
			t.Fatal(err)
		}
		s.advance(t, 700*time.Second)
		if got := s.updateState(); got != otadef.UpdateStateApplying {
			t.Errorf("UpdateState %d, want Applying", got)
		}
		for _, e := range s.transitions() {
			if e.PreviousState == otadef.UpdateStateApplying {
				t.Errorf("a transition out of Applying while it applies: %+v", e)
			}
		}
		close(release)
		s.eventually(t, "Idle", func() bool { return s.updateState() == otadef.UpdateStateIdle })
	})

	// :533-579. The provider's queryImage blocks; the source holding the
	// query is its Go form.
	t.Run("handles an announcement only after a running query, so no second query overlaps it", func(t *testing.T) {
		hold, arrived := make(chan struct{}), make(chan struct{})
		s := newOTASite(t, otaSiteOptions{prepare: func(src *otaSource) { src.hold, src.arrived, src.none = hold, arrived, true }})
		if err := s.announce(t, otadef.AnnouncementReasonUpdateAvailable); err != nil {
			t.Fatal(err)
		}
		s.eventually(t, "the first query", func() bool {
			select {
			case <-arrived:
				return true
			default:
				return false
			}
		})
		// The announcement arrives while the query runs. matter.js holds its
		// answer until the query's transaction ends; the Go requestor
		// answers at once and handles it after the query (fix: the
		// announcement's query is scheduled once the running one ended,
		// cluster/core TestOTARequestor_AnnouncementDuringQuery). The
		// provider answers the held query after 20 s rather than matter.js's
		// 700: a provider answering later than the requestor's exchange
		// waits is a lost answer here (the report names it).
		if err := s.announce(t, otadef.AnnouncementReasonUrgentUpdateAvailable); err != nil {
			t.Fatalf("the announcement during the query: %v", err)
		}
		s.advance(t, 20*time.Second)
		if got := s.src.queryCount(); got != 1 {
			t.Errorf("%d queries while the first runs, want 1", got)
		}
		close(hold)
		s.advance(t, 700*time.Second)
		if got := s.src.queryCount(); got != 2 {
			t.Errorf("%d queries after the first, want 2", got)
		}
	})
}

// otaBusyConsent is BusyOtaProviderServer (OtaTest.ts:583-598): every
// query answered Busy with a delayedActionTime of 60 s, the time of each
// answer recorded. The Go provider answers Busy with the consent port's
// delay while it obtains a consent the requestor cannot give (OP:269-273),
// so the port that is always obtaining is its Go form.
type otaBusyConsent struct {
	clk interface{ Now() time.Time }
	mu  sync.Mutex
	at  []time.Time
}

func (b *otaBusyConsent) RequestConsent(context.Context, mattercore.OTAPeer, provdef.QueryImageRequest, mattercore.OTAImage) (mattercore.OTAConsentState, time.Duration, error) {
	b.mu.Lock()
	b.at = append(b.at, b.clk.Now())
	b.mu.Unlock()
	return mattercore.OTAConsentObtaining, 60 * time.Second, nil
}

func (b *otaBusyConsent) answeredAt() []time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.at)
}

// TestOtaBusyParityMatterJS ports the describe "with a provider that
// answers Busy" (OtaTest.ts:581-705). The first query is the requestor's
// own, 30 s after its commissioning (#scheduleInitialQuery), as in
// matter.js.
func TestOtaBusyParityMatterJS(t *testing.T) {
	holdSite(t)
	newBusySite := func(t *testing.T) (*otaSite, *otaBusyConsent) {
		t.Helper()
		busy := &otaBusyConsent{clk: clockNow{}}
		s := newOTASite(t, otaSiteOptions{consent: busy})
		busy.mu.Lock()
		busy.clk = s.site.Clock()
		busy.mu.Unlock()
		return s, busy
	}
	into := func(s *otaSite, state otadef.UpdateStateEnum, count int) func() bool {
		return func() bool {
			n := 0
			for _, e := range s.transitions() {
				if e.NewState == state {
					n++
				}
			}
			return n >= count
		}
	}

	// :635-660.
	t.Run("waits in DelayedOnQuery while the retry is armed, and queries again when it is due", func(t *testing.T) {
		s, busy := newBusySite(t)
		s.eventually(t, "the second DelayedOnQuery", into(s, otadef.UpdateStateDelayedOnQuery, 2))
		transitions := s.transitions()
		states := make([]otadef.UpdateStateEnum, 0, len(transitions))
		for _, e := range transitions {
			states = append(states, e.NewState)
		}
		want := []otadef.UpdateStateEnum{otadef.UpdateStateQuerying, otadef.UpdateStateDelayedOnQuery, otadef.UpdateStateQuerying, otadef.UpdateStateDelayedOnQuery}
		if !slices.Equal(states, want) {
			t.Fatalf("transitions %v, want %v", states, want)
		}
		at := busy.answeredAt()
		if len(at) != 2 {
			t.Fatalf("%d answers, want 2", len(at))
		}
		// The provider asked for 60 s; the minimum query interval of 120 s
		// is longer.
		if gap := at[1].Sub(at[0]); gap < 120*time.Second || gap > 130*time.Second {
			t.Errorf("the second query %v after the first, want 120-130 s", gap)
		}
	})

	// :662-679.
	t.Run("gives up on the provider after three Busy retries", func(t *testing.T) {
		s, busy := newBusySite(t)
		s.eventually(t, "Idle", into(s, otadef.UpdateStateIdle, 1))
		if got := len(busy.answeredAt()); got != 4 {
			t.Fatalf("%d answers, want 4", got)
		}
		transitions := len(s.transitions())
		s.advance(t, 10*time.Minute)
		if got := len(s.transitions()); got != transitions {
			t.Errorf("%d transitions after ten more minutes, want %d", got, transitions)
		}
		if got := len(busy.answeredAt()); got != 4 {
			t.Errorf("%d answers after ten more minutes, want 4", got)
		}
	})

	// :681-704. updatePossible = false is the requestor's SetUpdatePossible.
	t.Run("returns to Idle when updates are disabled while it waits", func(t *testing.T) {
		s, busy := newBusySite(t)
		s.eventually(t, "DelayedOnQuery", into(s, otadef.UpdateStateDelayedOnQuery, 1))
		s.requestor().SetUpdatePossible(false)
		if err := s.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if got := s.updateState(); got != otadef.UpdateStateIdle {
			t.Fatalf("UpdateState %d, want Idle", got)
		}
		transitions := len(s.transitions())
		s.advance(t, 10*time.Minute)
		if got := len(s.transitions()); got != transitions {
			t.Errorf("%d transitions after ten minutes, want %d", got, transitions)
		}
		if got := len(busy.answeredAt()); got != 1 {
			t.Errorf("%d answers, want 1", got)
		}
	})
}

// clockNow is the wall clock, the busy port's until the site's is known.
type clockNow struct{}

func (clockNow) Now() time.Time { return time.Now() }
