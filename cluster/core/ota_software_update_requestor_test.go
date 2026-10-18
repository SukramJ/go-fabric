// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// Every test names the OtaSoftwareUpdateRequestorServer.ts lines (matter.js
// HEAD) of the rule it holds the Go requestor to.

// --- fakes -----------------------------------------------------------------

// fakeClock is the test clock, a [clocktest.Fake].
type fakeClock struct{ *clocktest.Fake }

func newFakeClock() *fakeClock { return &fakeClock{clocktest.New(time.Unix(1_700_000_000, 0))} }

// pending is the time left on each armed timer.
func (c *fakeClock) pending() []time.Duration { return c.Armed() }

// advance moves the clock and fires every timer due.
func (c *fakeClock) advance(d time.Duration) { c.Advance(d) }

type recordedEvent struct {
	id   uint32
	data any
}

type eventRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (e *eventRecorder) MatterEmitEvent(_ uint16, clusterID, eventID uint32, data any, _ contract.EventPriority) {
	if clusterID != otadef.ClusterID {
		return
	}
	e.mu.Lock()
	e.events = append(e.events, recordedEvent{eventID, data})
	e.mu.Unlock()
}

func (e *eventRecorder) transitions() []otadef.StateTransitionEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []otadef.StateTransitionEvent
	for _, ev := range e.events {
		if s, ok := ev.data.(otadef.StateTransitionEvent); ok {
			out = append(out, s)
		}
	}
	return out
}

func (e *eventRecorder) of(id uint32) []any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []any
	for _, ev := range e.events {
		if ev.id == id {
			out = append(out, ev.data)
		}
	}
	return out
}

type memOtaStore struct {
	mu       sync.Mutex
	defaults map[uint8][]store.OTAProviderLocation
	state    []byte
}

func newMemOtaStore() *memOtaStore {
	return &memOtaStore{defaults: map[uint8][]store.OTAProviderLocation{}}
}

func (s *memOtaStore) ListDefaultOTAProviders(context.Context) ([]store.OTAProviderLocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.OTAProviderLocation
	for f := uint8(1); f < 255; f++ {
		for _, e := range s.defaults[f] {
			e.FabricIndex = f
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *memOtaStore) ReplaceDefaultOTAProviders(_ context.Context, f uint8, e []store.OTAProviderLocation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaults[f] = slices.Clone(e)
	return nil
}

func (s *memOtaStore) LoadOTARequestorState(context.Context) (state []byte, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.state != nil, nil
}

func (s *memOtaStore) SaveOTARequestorState(_ context.Context, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = slices.Clone(b)
	return nil
}

type fakeApplier struct {
	mu       sync.Mutex
	consent  bool
	consentE error
	applyErr error
	applied  []uint32
	files    []string
	asked    int
}

func (a *fakeApplier) RequestUserConsent(context.Context, uint32, string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked++
	return a.consent, a.consentE
}

func (a *fakeApplier) ApplyUpdate(_ context.Context, v uint32, file string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applied = append(a.applied, v)
	a.files = append(a.files, file)
	return a.applyErr
}

// fakeProvider scripts the provider's answers and serves an image over BDX.
type fakeProvider struct {
	t       *testing.T
	nodeID  uint64
	mu      sync.Mutex
	queries []provdef.QueryImageRequest
	applies []provdef.ApplyUpdateRequestRequest
	notify  []provdef.NotifyUpdateAppliedRequest
	// answer returns the QueryImageResponse (or an error) for query n.
	answer func(n int) (provdef.QueryImageResponse, error)
	// apply returns the ApplyUpdateResponse for request n.
	apply func(n int) provdef.ApplyUpdateResponse
	image []byte
	// bdxFail, when set, answers each ReceiveInit with this BDX status.
	bdxFail     bdx.StatusCode
	transfers   int
	connectErr  error
	connections int
}

func (p *fakeProvider) ConnectOtaProvider(_ context.Context, _ uint8, node uint64) (OtaProviderSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connections++
	if p.connectErr != nil {
		return nil, p.connectErr
	}
	if node != p.nodeID {
		return nil, fmt.Errorf("no provider %x", node)
	}
	return p, nil
}

func nodeOf(t *testing.T, v spec.Encodable) *spec.Node {
	t.Helper()
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(b)
	el, _ := dec.Next()
	n, err := spec.ReadContainer(dec, el)
	if err != nil {
		t.Fatal(err)
	}
	return &n
}

func (p *fakeProvider) Invoke(_ context.Context, endpoint uint16, clusterID, cmd uint32, fields spec.Encodable) (*spec.Node, error) {
	if clusterID != provdef.ClusterID || endpoint != 0 {
		return nil, fmt.Errorf("invoke on 0x%04x ep %d", clusterID, endpoint)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch cmd {
	case provdef.CmdQueryImage:
		p.queries = append(p.queries, fields.(provdef.QueryImageRequest)) //nolint:forcetypeassert // test
		resp, err := p.answer(len(p.queries) - 1)
		if err != nil {
			return nil, err
		}
		return nodeOf(p.t, resp), nil
	case provdef.CmdApplyUpdateRequest:
		p.applies = append(p.applies, fields.(provdef.ApplyUpdateRequestRequest)) //nolint:forcetypeassert // test
		return nodeOf(p.t, p.apply(len(p.applies)-1)), nil
	case provdef.CmdNotifyUpdateApplied:
		p.notify = append(p.notify, fields.(provdef.NotifyUpdateAppliedRequest)) //nolint:forcetypeassert // test
		return nil, nil
	}
	return nil, fmt.Errorf("command %d", cmd)
}

// pipe is an in-memory exchange.
type pipe struct {
	in, out chan bdx.Message
}

func (p *pipe) Send(_ context.Context, proto uint16, op uint8, payload []byte) error {
	p.out <- bdx.Message{ProtocolID: proto, Opcode: op, Payload: slices.Clone(payload)}
	return nil
}

func (p *pipe) Receive(ctx context.Context) (bdx.Message, error) {
	select {
	case m := <-p.in:
		return m, nil
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}
func (p *pipe) MaxPayloadSize() int { return 1178 }
func (p *pipe) Close() error        { return nil }

func (p *fakeProvider) OpenExchange(context.Context) (bdx.Exchange, error) {
	a2b, b2a := make(chan bdx.Message, 64), make(chan bdx.Message, 64)
	req := &pipe{in: b2a, out: a2b}
	prov := &pipe{in: a2b, out: b2a}
	p.mu.Lock()
	p.transfers++
	image, fail := p.image, p.bdxFail
	p.mu.Unlock()
	go serveImage(prov, image, fail)
	return req, nil
}

// serveImage is a receiver-driven BDX sender of image.
func serveImage(p *pipe, image []byte, fail bdx.StatusCode) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := p.Receive(ctx)
	if err != nil {
		return
	}
	if fail != 0 {
		_ = p.Send(ctx, bdx.SecureChannelProtocolID, bdx.OpcodeStatusReport, bdx.EncodeStatusReport(bdx.GeneralFailure, fail))
		return
	}
	init, _ := bdx.DecodeInit(m.Payload)
	l := uint64(len(image))
	acc, _ := bdx.ReceiveAccept{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: init.MaxBlockSize, Length: &l}.Encode()
	_ = p.Send(ctx, bdx.ProtocolID, uint8(bdx.MessageReceiveAccept), acc)
	size := int(init.MaxBlockSize)
	for off := 0; ; {
		q, err := p.Receive(ctx)
		if err != nil || bdx.MessageType(q.Opcode) != bdx.MessageBlockQuery {
			return
		}
		c, _ := bdx.DecodeCounterOnly(q.Payload)
		end := min(off+size, len(image))
		t := bdx.MessageBlock
		if end == len(image) {
			t = bdx.MessageBlockEOF
		}
		_ = p.Send(ctx, bdx.ProtocolID, uint8(t), bdx.Block{BlockCounter: c.BlockCounter, Data: image[off:end]}.Encode())
		off = end
		if t == bdx.MessageBlockEOF {
			_, _ = p.Receive(ctx) // BlockAckEof
			return
		}
	}
}

// --- harness -----------------------------------------------------------------

const testProvider = uint64(0x0000_0000_0000_00AA)

type otaHarness struct {
	t        *testing.T
	r        *OTASoftwareUpdateRequestor
	clock    *fakeClock
	events   *eventRecorder
	store    *memOtaStore
	applier  *fakeApplier
	provider *fakeProvider
	info     OtaNodeInformation
	infoMu   sync.Mutex
	dir      string
}

func newOtaHarness(t *testing.T, mutate func(*OTARequestorConfig)) *otaHarness {
	t.Helper()
	h := &otaHarness{
		t: t, clock: newFakeClock(), events: &eventRecorder{}, store: newMemOtaStore(), applier: &fakeApplier{consent: true},
		info: OtaNodeInformation{VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 1, SoftwareVersionString: "1.0", HardwareVersion: 1, Location: "XX"},
		dir:  t.TempDir(),
	}
	h.provider = &fakeProvider{t: t, nodeID: testProvider, image: h.image(2), apply: func(int) provdef.ApplyUpdateResponse {
		return provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionProceed}
	}}
	h.provider.answer = func(int) (provdef.QueryImageResponse, error) { return h.available(2), nil }
	cfg := OTARequestorConfig{
		NodeInformation: func() OtaNodeInformation {
			h.infoMu.Lock()
			defer h.infoMu.Unlock()
			return h.info
		},
		Connector:   h.provider,
		Applier:     h.applier,
		Store:       h.store,
		DownloadDir: h.dir,
		TimeSource:  h.clock,
		Rand:        func(int) int { return 0 },
	}
	if mutate != nil {
		mutate(&cfg)
	}
	r, err := NewOTASoftwareUpdateRequestor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r.SetMatterEventEmitter(h.events)
	h.r = r
	t.Cleanup(r.Stop)
	return h
}

func (h *otaHarness) image(version uint32) []byte {
	img, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: version, SoftwareVersionString: fmt.Sprintf("%d.0", version),
		Payload: make([]byte, 2500),
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return img
}

func (h *otaHarness) available(version uint32) provdef.QueryImageResponse {
	uri := fmt.Sprintf("bdx://%016X/fff1-8001-v%d.ota", testProvider, version)
	s := fmt.Sprintf("%d.0", version)
	token := []byte("token-12345678")
	return provdef.QueryImageResponse{Status: provdef.StatusUpdateAvailable, ImageUri: &uri, SoftwareVersion: &version, SoftwareVersionString: &s, UpdateToken: &token}
}

func (h *otaHarness) addActive(t *testing.T) otadef.ProviderLocation {
	t.Helper()
	loc := otadef.ProviderLocation{FabricIndex: 1, ProviderNodeId: testProvider, Endpoint: 0}
	h.r.addActiveOtaProvider(context.Background(), loc, nil)
	return loc
}

func (h *otaHarness) state() otadef.UpdateStateEnum {
	v, _ := h.r.MatterRead(otadef.AttrUpdateState)
	return otadef.UpdateStateEnum(v.(uint8)) //nolint:forcetypeassert // test
}

func states(ts []otadef.StateTransitionEvent) []string {
	out := make([]string, len(ts))
	for i, s := range ts {
		out[i] = fmt.Sprintf("%d>%d/%d", s.PreviousState, s.NewState, s.Reason)
	}
	return out
}

func fabricCtx(f uint8) context.Context {
	return im.WithFabricFilter(context.Background(), true, f)
}

// --- tests -------------------------------------------------------------------

// TestOTARequestor_Initialize: an Unknown state starts Idle (:175-178);
// the endpoint lists OtaRequestor (:181) at the snapshot's revision;
// UpdatePossible is the model default, true.
func TestOTARequestor_Initialize(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	if h.state() != otadef.UpdateStateIdle {
		t.Fatalf("state %d", h.state())
	}
	if v, _ := h.r.MatterRead(otadef.AttrUpdatePossible); v != true {
		t.Fatal("UpdatePossible not true")
	}
	if v, _ := h.r.MatterRead(otadef.AttrUpdateStateProgress); v != nil {
		t.Fatalf("progress %v", v)
	}
	if dt := OtaRequestorDeviceType(); dt.DeviceType != 0x0012 || dt.Revision != 1 {
		t.Fatalf("device type %+v", dt)
	}
	if !slices.Contains(h.r.MatterAcceptedCommands(), otadef.CmdAnnounceOtaProvider) || len(h.r.MatterEvents()) != 3 {
		t.Fatal("AnnounceOTAProvider accepted and the three events listed")
	}
	if _, err := NewOTASoftwareUpdateRequestor(OTARequestorConfig{DownloadDir: "x"}); err == nil {
		t.Fatal("no NodeInformation accepted")
	}
}

// TestOTARequestor_OnlineAddsDefaultsAndSchedules: #online (:220-238) adds
// the default providers when none is active and schedules the first query
// 2-12 minutes out.
func TestOTARequestor_OnlineAddsDefaultsAndSchedules(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, func(c *OTARequestorConfig) { c.Rand = func(int) int { return 5 } })
	_ = h.store.ReplaceDefaultOTAProviders(context.Background(), 1, []store.OTAProviderLocation{{ProviderNodeID: testProvider}})
	if err := h.r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.r.active) != 1 || h.r.active[0].Location.ProviderNodeId != testProvider {
		t.Fatalf("active %+v", h.r.active)
	}
	if got := h.clock.pending(); len(got) != 1 || got[0] != 125*time.Second {
		t.Fatalf("pending %v, want [125s] (rand 5 + 120)", got)
	}
	if err := h.r.Start(context.Background()); err == nil {
		t.Fatal("second Start accepted")
	}
}

// TestOTARequestor_PreviousUpdateApplied: #handlePreviousUpdateOnStart
// (:255-313) with the new version running: VersionApplied, Applying → Idle
// (Success), NotifyUpdateApplied; the next query at the regular interval
// (:232-237).
func TestOTARequestor_PreviousUpdateApplied(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.info.SoftwareVersion = 2
	loc := otadef.ProviderLocation{FabricIndex: 1, ProviderNodeId: testProvider}
	h.r.active = []activeProvider{{Location: loc}}
	h.r.inProgress = &updateInProgress{NewSoftwareVersion: 2, Location: &loc, UpdateToken: []byte("token-12345678")}
	h.r.persist(context.Background())
	h.r.active, h.r.inProgress = nil, nil
	if err := h.r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.r.wg.Wait()
	if got := h.events.of(otadef.EventVersionApplied); len(got) != 1 || got[0].(otadef.VersionAppliedEvent).SoftwareVersion != 2 || got[0].(otadef.VersionAppliedEvent).ProductId != 0x8001 { //nolint:forcetypeassert // test
		t.Fatalf("VersionApplied %+v", got)
	}
	tr := h.events.transitions()
	if len(tr) != 1 || tr[0].PreviousState != otadef.UpdateStateApplying || tr[0].NewState != otadef.UpdateStateIdle || tr[0].Reason != otadef.ChangeReasonSuccess || !tr[0].TargetSoftwareVersion.Null {
		t.Fatalf("transition %+v", tr)
	}
	if len(h.provider.notify) != 1 || h.provider.notify[0].SoftwareVersion != 2 || string(h.provider.notify[0].UpdateToken) != "token-12345678" {
		t.Fatalf("NotifyUpdateApplied %+v", h.provider.notify)
	}
	if h.r.inProgress != nil {
		t.Fatal("details not cleared")
	}
	if got := h.clock.pending(); len(got) != 1 || got[0] != 24*time.Hour {
		t.Fatalf("pending %v, want the daily interval", got)
	}
}

// TestOTARequestor_PreviousUpdateFailed: a node still on another version
// reports Applying → Idle (Failure) and asks the same provider again 2-12
// minutes later, "like a busy provider" (:258-277).
func TestOTARequestor_PreviousUpdateFailed(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	loc := otadef.ProviderLocation{FabricIndex: 1, ProviderNodeId: testProvider}
	h.r.active = []activeProvider{{Location: loc}}
	h.r.inProgress = &updateInProgress{NewSoftwareVersion: 2, Location: &loc}
	h.r.online(context.Background())
	h.r.wg.Wait()
	tr := h.events.transitions()
	if len(tr) != 1 || tr[0].Reason != otadef.ChangeReasonFailure {
		t.Fatalf("transition %+v", tr)
	}
	if len(h.events.of(otadef.EventVersionApplied)) != 0 {
		t.Fatal("VersionApplied for a failed update")
	}
	if h.r.selected == nil || h.r.providerRetries != 1 {
		t.Fatalf("selected %+v retries %d", h.r.selected, h.r.providerRetries)
	}
	if got := h.clock.pending(); len(got) != 1 || got[0] != 120*time.Second {
		t.Fatalf("pending %v", got)
	}
}

// TestOTARequestor_EnableDisable: #enableOrDisableUpdates (:316-336).
func TestOTARequestor_EnableDisable(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, func(c *OTARequestorConfig) { c.Rand = func(int) int { return 7 } })
	h.addActive(t)
	h.r.SetUpdatePossible(false)
	if len(h.clock.pending()) != 0 {
		t.Fatal("timer left")
	}
	h.r.state = otadef.UpdateStateDelayedOnQuery
	h.r.SetUpdatePossible(true)
	if got := h.clock.pending(); len(got) != 1 || got[0] != 67*time.Second {
		t.Fatalf("pending %v, want 67s (60 + rand 7)", got)
	}
	h.r.SetUpdatePossible(false)
	if h.state() != otadef.UpdateStateIdle || len(h.clock.pending()) != 0 {
		t.Fatalf("DelayedOnQuery not reset: %d", h.state())
	}
	// No query while updates are not possible (:504-506).
	d := time.Second
	if h.r.scheduleUpdateQuery(&d, reasonNewQuery, nil) {
		t.Fatal("scheduled while UpdatePossible is false")
	}
}

// TestOTARequestor_DefaultProvidersWrite: the fabric-scoped write, one
// entry per fabric (#assertDefaultOtaProviders :339-349), persisted, and
// #handleUpdatedDefaultOtaProviders (:352-368).
func TestOTARequestor_DefaultProvidersWrite(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, func(c *OTARequestorConfig) { c.Rand = func(int) int { return 1 } })
	list := spec.List[otadef.ProviderLocation]{{ProviderNodeId: testProvider, Endpoint: 0, FabricIndex: 9}}
	if err := h.r.MatterWrite(fabricCtx(1), otadef.AttrDefaultOtaProviders, list); err != nil {
		t.Fatal(err)
	}
	if err := h.r.MatterWrite(fabricCtx(2), otadef.AttrDefaultOtaProviders, spec.List[otadef.ProviderLocation]{{ProviderNodeId: 0xBB}}); err != nil {
		t.Fatal(err)
	}
	v, _ := h.r.MatterReadFiltered(fabricCtx(1), otadef.AttrDefaultOtaProviders)
	if got := v.(spec.List[otadef.ProviderLocation]); len(got) != 1 || got[0].FabricIndex != 1 || got[0].ProviderNodeId != testProvider { //nolint:forcetypeassert // test
		t.Fatalf("fabric 1 reads %+v", got)
	}
	all, _ := h.r.MatterReadFiltered(im.WithFabricFilter(context.Background(), false, 1), otadef.AttrDefaultOtaProviders)
	if len(all.(spec.List[otadef.ProviderLocation])) != 2 { //nolint:forcetypeassert // test
		t.Fatalf("unfiltered read %+v", all)
	}
	if rows, _ := h.store.ListDefaultOTAProviders(context.Background()); len(rows) != 2 {
		t.Fatalf("persisted %+v", rows)
	}
	// The first write's provider became active and a query was scheduled
	// 1-11 minutes out; the second found a timer and scheduled none.
	if len(h.r.active) != 2 {
		t.Fatalf("active %+v", h.r.active)
	}
	if got := h.clock.pending(); len(got) != 1 || got[0] != 61*time.Second {
		t.Fatalf("pending %v", got)
	}

	two := spec.List[otadef.ProviderLocation]{{ProviderNodeId: 1}, {ProviderNodeId: 2}}
	var se interface{ MatterStatusCode() im.StatusCode }
	if err := h.r.MatterWrite(fabricCtx(1), otadef.AttrDefaultOtaProviders, two); !errors.As(err, &se) || se.MatterStatusCode() != im.StatusConstraintError {
		t.Fatalf("two entries: %v", err)
	}
	if err := h.r.MatterWrite(context.Background(), otadef.AttrDefaultOtaProviders, list); !errors.As(err, &se) || se.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("no fabric: %v", err)
	}
	if err := h.r.MatterWrite(fabricCtx(1), otadef.AttrDefaultOtaProviders, badOtaValue{}); !errors.As(err, &se) || se.MatterStatusCode() != im.StatusConstraintError {
		t.Fatalf("bad value: %v", err)
	}
	if err := h.r.MatterWrite(fabricCtx(1), otadef.AttrUpdateState, uint8(1)); err == nil {
		t.Fatal("UpdateState written")
	}
	h.r.FabricRemoved(2)
	if v, _ := h.r.MatterRead(otadef.AttrDefaultOtaProviders); len(v.(spec.List[otadef.ProviderLocation])) != 1 { //nolint:forcetypeassert // test
		t.Fatal("fabric 2's entry survived its removal")
	}
}

// TestOTARequestor_DecodeDefaultProvidersWrite: a client leaves the
// FabricIndex of a fabric-scoped struct out of its write.
func TestOTARequestor_DecodeDefaultProvidersWrite(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartArray(tlv.AnonymousTag())
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(1), testProvider)
	enc.PutUint(tlv.ContextTag(2), 0)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	b, _ := enc.Bytes()
	dec := tlv.NewDecoder(b)
	el, _ := dec.Next()
	v, err := DecodeDefaultOTAProvidersWrite(dec, el, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.(spec.List[otadef.ProviderLocation]); len(got) != 1 || got[0].ProviderNodeId != testProvider || got[0].FabricIndex != 0 { //nolint:forcetypeassert // test
		t.Fatalf("%+v", got)
	}
	// One list-append item.
	enc = tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(1), 5)
	_ = enc.EndContainer()
	b, _ = enc.Bytes()
	dec = tlv.NewDecoder(b)
	el, _ = dec.Next()
	if v, _ := DecodeDefaultOTAProvidersWrite(dec, el, true); v != (badOtaValue{}) {
		t.Fatalf("entry without Endpoint decoded: %+v", v)
	}
}

// TestOTARequestor_Announce: announceOtaProvider (:376-420).
func TestOTARequestor_Announce(t *testing.T) {
	t.Parallel()
	quick := 250 * time.Millisecond
	h := newOtaHarness(t, func(c *OTARequestorConfig) { c.AnnouncedUpdateQueryDelay = &quick })
	req := otadef.AnnounceOtaProviderRequest{ProviderNodeId: testProvider, VendorId: 0xFFF1, AnnouncementReason: otadef.AnnouncementReasonSimpleAnnouncement}
	var se interface{ MatterStatusCode() im.StatusCode }
	if _, err := h.r.MatterInvoke(context.Background(), otadef.CmdAnnounceOtaProvider, req); !errors.As(err, &se) || se.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("without a fabric: %v", err)
	}
	// Simple: the query is scheduled at the regular interval.
	if _, err := h.r.MatterInvoke(fabricCtx(1), otadef.CmdAnnounceOtaProvider, req); err != nil {
		t.Fatal(err)
	}
	if got := h.clock.pending(); len(got) != 1 || got[0] != 24*time.Hour {
		t.Fatalf("simple: pending %v", got)
	}
	// UpdateAvailable: the announced delay, against that provider.
	req.AnnouncementReason = otadef.AnnouncementReasonUpdateAvailable
	if _, err := h.r.MatterInvoke(fabricCtx(1), otadef.CmdAnnounceOtaProvider, &req); err != nil {
		t.Fatal(err)
	}
	if got := h.clock.pending(); len(got) != 1 || got[0] != quick {
		t.Fatalf("urgent: pending %v", got)
	}
	if h.r.selected == nil || h.r.selected.ProviderNodeId != testProvider || h.r.selected.FabricIndex != 1 {
		t.Fatalf("selected %+v", h.r.selected)
	}
	if _, err := h.r.MatterInvoke(fabricCtx(1), 0x05, nil); !errors.Is(err, im.ErrUnsupportedCommand) {
		t.Fatal(err)
	}
	// Without a configured delay the wait is 1-600 s (:410).
	h2 := newOtaHarness(t, func(c *OTARequestorConfig) { c.Rand = func(int) int { return 599 } })
	if _, err := h2.r.MatterInvoke(fabricCtx(1), otadef.CmdAnnounceOtaProvider, req); err != nil {
		t.Fatal(err)
	}
	if got := h2.clock.pending(); len(got) != 1 || got[0] != 600*time.Second {
		t.Fatalf("random: pending %v", got)
	}
}

// TestOTARequestor_ChooseProvider: #chooseUpdateProvider (:525-550), the
// oldest previousQueryTimestamp first, a never-queried one before all.
func TestOTARequestor_ChooseProvider(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	ts := func(v int64) *int64 { return &v }
	h.r.active = []activeProvider{
		{Location: otadef.ProviderLocation{ProviderNodeId: 1}, PreviousQueryTimestamp: ts(30)},
		{Location: otadef.ProviderLocation{ProviderNodeId: 2}, PreviousQueryTimestamp: ts(10)},
		{Location: otadef.ProviderLocation{ProviderNodeId: 3}},
	}
	if p := h.r.chooseUpdateProvider(); p.ProviderNodeId != 3 {
		t.Fatalf("chose %d", p.ProviderNodeId)
	}
	h.r.active[0].PreviousQueryTimestamp = ts(50)
	if p := h.r.chooseUpdateProvider(); p.ProviderNodeId != 2 {
		t.Fatalf("chose %d", p.ProviderNodeId)
	}
	h.r.selected = &otadef.ProviderLocation{ProviderNodeId: 9}
	if p := h.r.chooseUpdateProvider(); p.ProviderNodeId != 9 || h.r.selected != nil {
		t.Fatal("selected provider not used once")
	}
}

// TestOTARequestor_FullUpdate: the whole query (:584-957) — QueryImage with
// the node's identity and BDX only, Querying → Downloading (target) →
// Applying (target), the BDX download in 1024-byte receiver-driven blocks,
// ApplyUpdateRequest, the apply after the query (:1258-1285) and Idle.
func TestOTARequestor_FullUpdate(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	h.r.performUpdateQuery(context.Background())

	q := h.provider.queries[0]
	if q.VendorId != 0xFFF1 || q.ProductId != 0x8001 || q.SoftwareVersion != 1 || !slices.Equal(q.ProtocolsSupported, []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous}) ||
		*q.HardwareVersion != 1 || *q.Location != "XX" || q.RequestorCanConsent != nil {
		t.Fatalf("QueryImage %+v", q)
	}
	want := []string{"1>2/1", "2>4/1", "4>5/1", "5>1/1"}
	if got := states(h.events.transitions()); !slices.Equal(got, want) {
		t.Fatalf("transitions %v, want %v", got, want)
	}
	tr := h.events.transitions()
	if tr[1].TargetSoftwareVersion.Null || tr[1].TargetSoftwareVersion.Value != 2 || tr[2].TargetSoftwareVersion.Value != 2 || !tr[0].TargetSoftwareVersion.Null {
		t.Fatalf("target versions %+v", tr)
	}
	if a := h.provider.applies; len(a) != 1 || a[0].NewVersion != 2 || string(a[0].UpdateToken) != "token-12345678" {
		t.Fatalf("ApplyUpdateRequest %+v", a)
	}
	if len(h.applier.applied) != 1 || h.applier.applied[0] != 2 || h.applier.files[0] != filepath.Join(h.dir, "ota-65521-32769") {
		t.Fatalf("applied %+v %+v", h.applier.applied, h.applier.files)
	}
	if h.r.inProgress == nil || h.r.inProgress.NewSoftwareVersion != 2 || h.r.inProgress.Location == nil {
		t.Fatal("update in progress not recorded")
	}
	if v, _ := h.r.MatterRead(otadef.AttrUpdateStateProgress); v != nil {
		t.Fatalf("progress after Idle %v", v)
	}
}

// TestOTARequestor_NotAvailableAndBusy: Busy is asked again after
// max(delay, minimumQueryInterval) in DelayedOnQuery, three times at most;
// NotAvailable marks the provider (:669-690).
func TestOTARequestor_NotAvailableAndBusy(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	loc := h.addActive(t)
	h.provider.answer = func(int) (provdef.QueryImageResponse, error) {
		d := uint32(10)
		return provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: &d}, nil
	}
	for i := range 3 {
		h.r.performUpdateQuery(context.Background())
		if h.state() != otadef.UpdateStateDelayedOnQuery {
			t.Fatalf("query %d: state %d", i, h.state())
		}
		if got := h.clock.pending(); len(got) != 1 || got[0] != 120*time.Second {
			t.Fatalf("query %d: pending %v", i, got)
		}
	}
	h.r.performUpdateQuery(context.Background())
	if h.state() != otadef.UpdateStateIdle || h.r.active[h.r.activeIndexLocked(loc)].PreviousQueryTimestamp == nil {
		t.Fatal("fourth Busy not treated as no update")
	}
	last := h.events.transitions()
	if l := last[len(last)-1]; l.NewState != otadef.UpdateStateIdle || l.Reason != otadef.ChangeReasonSuccess {
		t.Fatalf("last %+v", l)
	}
	if !slices.Contains(states(last), "2>3/4") {
		t.Fatalf("no DelayByProvider transition: %v", states(last))
	}

	h.provider.answer = func(int) (provdef.QueryImageResponse, error) {
		return provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}, nil
	}
	h.r.performUpdateQuery(context.Background())
	if h.state() != otadef.UpdateStateIdle {
		t.Fatal("NotAvailable")
	}
}

// TestOTARequestor_DownloadProtocolNotSupported removes the provider from
// the active and the default list (:691-698, #removeOtaProvider :445-460).
func TestOTARequestor_DownloadProtocolNotSupported(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	if err := h.r.MatterWrite(fabricCtx(1), otadef.AttrDefaultOtaProviders, spec.List[otadef.ProviderLocation]{{ProviderNodeId: testProvider}}); err != nil {
		t.Fatal(err)
	}
	h.provider.answer = func(int) (provdef.QueryImageResponse, error) {
		return provdef.QueryImageResponse{Status: provdef.StatusDownloadProtocolNotSupported}, nil
	}
	h.r.performUpdateQuery(context.Background())
	if len(h.r.active) != 0 || len(h.r.defaults) != 0 {
		t.Fatalf("active %+v defaults %+v", h.r.active, h.r.defaults)
	}
	if rows, _ := h.store.ListDefaultOTAProviders(context.Background()); len(rows) != 0 {
		t.Fatalf("store keeps %+v", rows)
	}
}

// TestOTARequestor_InvalidAnswers: missing mandatory fields and a BDX URI
// naming another node are Failure (:701-726); a version not newer is
// ignored (:728-736); a provider that cannot be reached counts as no update
// (:632-637).
func TestOTARequestor_InvalidAnswers(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		answer func(h *otaHarness) provdef.QueryImageResponse
		reason otadef.ChangeReasonEnum
	}{
		"missing fields": {func(*otaHarness) provdef.QueryImageResponse {
			return provdef.QueryImageResponse{Status: provdef.StatusUpdateAvailable}
		}, otadef.ChangeReasonFailure},
		"foreign uri": {func(h *otaHarness) provdef.QueryImageResponse {
			r := h.available(2)
			u := "bdx://00000000000000BB/x.ota"
			r.ImageUri = &u
			return r
		}, otadef.ChangeReasonFailure},
		"not newer": {func(h *otaHarness) provdef.QueryImageResponse { return h.available(1) }, otadef.ChangeReasonSuccess},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newOtaHarness(t, nil)
			loc := h.addActive(t)
			h.provider.answer = func(int) (provdef.QueryImageResponse, error) { return tc.answer(h), nil }
			h.r.performUpdateQuery(context.Background())
			tr := h.events.transitions()
			if l := tr[len(tr)-1]; l.NewState != otadef.UpdateStateIdle || l.Reason != tc.reason {
				t.Fatalf("last %+v", l)
			}
			if h.r.active[h.r.activeIndexLocked(loc)].PreviousQueryTimestamp == nil || h.provider.transfers != 0 {
				t.Fatal("provider not marked, or a download started")
			}
		})
	}
	h := newOtaHarness(t, nil)
	loc := h.addActive(t)
	h.provider.connectErr = errors.New("unreachable")
	h.r.performUpdateQuery(context.Background())
	if h.r.active[h.r.activeIndexLocked(loc)].PreviousQueryTimestamp == nil || h.state() != otadef.UpdateStateIdle {
		t.Fatal("unreachable provider")
	}
	if got := states(h.events.transitions()); !slices.Equal(got, []string{"1>2/1", "2>1/1"}) {
		t.Fatalf("transitions %v (matter.js resets with Success, :639-641)", got)
	}
}

// TestOTARequestor_Consent: RequestorCanConsent only with canConsent and
// local config enabled (:855); a consent answer goes through
// DelayedOnUserConsent (:880-899); a provider wanting consent from a
// requestor that cannot give it is ignored (:738-745), and with local
// config disabled it is a Failure (:747-751).
func TestOTARequestor_Consent(t *testing.T) {
	t.Parallel()
	consentAnswer := func(h *otaHarness) {
		h.provider.answer = func(int) (provdef.QueryImageResponse, error) {
			r := h.available(2)
			yes := true
			r.UserConsentNeeded = &yes
			return r, nil
		}
	}
	h := newOtaHarness(t, func(c *OTARequestorConfig) { c.CanConsent = true })
	h.addActive(t)
	consentAnswer(h)
	h.applier.consent = false
	h.r.performUpdateQuery(context.Background())
	if q := h.provider.queries[0]; q.RequestorCanConsent == nil || !*q.RequestorCanConsent {
		t.Fatal("RequestorCanConsent not sent")
	}
	if got := states(h.events.transitions()); !slices.Equal(got, []string{"1>2/1", "2>8/1", "8>1/1"}) {
		t.Fatalf("denied: %v", got)
	}
	h.applier.consent, h.applier.consentE = true, errors.New("ui gone")
	h.r.performUpdateQuery(context.Background())
	if tr := h.events.transitions(); tr[len(tr)-1].Reason != otadef.ChangeReasonFailure {
		t.Fatal("consent error not a Failure")
	}
	h.applier.consentE = nil
	h.r.performUpdateQuery(context.Background())
	if len(h.applier.applied) != 1 {
		t.Fatal("consented update not applied")
	}

	h2 := newOtaHarness(t, nil) // canConsent false
	h2.addActive(t)
	consentAnswer(h2)
	h2.r.performUpdateQuery(context.Background())
	if h2.applier.asked != 0 || h2.provider.transfers != 0 || h2.events.transitions()[1].Reason != otadef.ChangeReasonSuccess {
		t.Fatal("update needing consent not ignored")
	}

	h3 := newOtaHarness(t, func(c *OTARequestorConfig) { c.CanConsent = true })
	h3.info.LocalConfigDisabled = true
	h3.addActive(t)
	consentAnswer(h3)
	h3.r.performUpdateQuery(context.Background())
	if h3.provider.queries[0].RequestorCanConsent != nil || h3.events.transitions()[1].Reason != otadef.ChangeReasonFailure {
		t.Fatal("local config disabled")
	}
}

// TestOTARequestor_DownloadFailure: three BDX attempts (:965-1004), then a
// DownloadError with UpdateState Unknown's value as platform code — the
// peer's StatusReport is no BdxError — and no bytes counted (:994-1002,
// :927-929), Idle (Failure), the partial file deleted
// (:931-936).
func TestOTARequestor_DownloadFailure(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	h.provider.bdxFail = bdx.StatusFileDesignatorUnknown
	h.r.performUpdateQuery(context.Background())
	if h.provider.transfers != 3 {
		t.Fatalf("%d attempts", h.provider.transfers)
	}
	errs := h.events.of(otadef.EventDownloadError)
	if len(errs) != 1 {
		t.Fatalf("DownloadError %+v", errs)
	}
	de := errs[0].(otadef.DownloadErrorEvent) //nolint:forcetypeassert // test
	if de.SoftwareVersion != 2 || de.BytesDownloaded != 0 || de.PlatformCode.Null || de.PlatformCode.Value != 0 || de.ProgressPercent.Null || de.ProgressPercent.Value != 0 {
		t.Fatalf("DownloadError %+v", de)
	}
	tr := h.events.transitions()
	if l := tr[len(tr)-1]; l.PreviousState != otadef.UpdateStateDownloading || l.Reason != otadef.ChangeReasonFailure {
		t.Fatalf("last %+v", l)
	}
	if _, err := os.Stat(h.r.downloadPath()); !os.IsNotExist(err) {
		t.Fatal("partial file left")
	}
}

// TestOTARequestor_InvalidImage: an image whose version is not the offered
// one fails validateUpdateFile (:1141-1145) — DownloadError with the file's
// size and UpdateState Unknown's value as code (:1148-1156).
func TestOTARequestor_InvalidImage(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	h.provider.image = h.image(3)
	h.r.performUpdateQuery(context.Background())
	errs := h.events.of(otadef.EventDownloadError)
	if len(errs) != 1 {
		t.Fatalf("DownloadError %+v", errs)
	}
	de := errs[0].(otadef.DownloadErrorEvent) //nolint:forcetypeassert // test
	if de.BytesDownloaded != uint64(len(h.provider.image)) || de.PlatformCode.Value != 0 {
		t.Fatalf("DownloadError %+v", de)
	}
	if len(h.applier.applied) != 0 {
		t.Fatal("invalid image applied")
	}
}

// TestOTARequestor_ApplyActions: AwaitNextAction waits
// min(max(delay, minimumApplyDelay), 24 h) in DelayedOnApply and asks again
// (:801-814); Proceed with a delay waits min(delay, 24 h) (:822-834);
// Discontinue deletes the file (:792-800).
func TestOTARequestor_ApplyActions(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	h.provider.apply = func(n int) provdef.ApplyUpdateResponse {
		if n == 0 {
			return provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionAwaitNextAction, DelayedActionTime: 5}
		}
		return provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionProceed, DelayedActionTime: 90000}
	}
	done := make(chan struct{})
	go func() {
		h.r.performUpdateQuery(context.Background())
		close(done)
	}()
	waitPending := func(want time.Duration) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !slices.Contains(h.clock.pending(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("no %v wait; pending %v", want, h.clock.pending())
			}
			time.Sleep(5 * time.Millisecond)
		}
		h.clock.advance(want)
	}
	waitPending(2 * time.Minute) // max(5 s, minimumApplyDelay)
	waitPending(24 * time.Hour)  // min(90000 s, 24 h)
	<-done
	if got := states(h.events.transitions()); !slices.Equal(got, []string{"1>2/1", "2>4/1", "4>5/1", "5>6/4", "6>5/1", "5>6/4", "6>5/1", "5>1/1"}) {
		t.Fatalf("transitions %v", got)
	}
	if len(h.applier.applied) != 1 {
		t.Fatal("not applied")
	}

	h2 := newOtaHarness(t, nil)
	h2.addActive(t)
	h2.provider.apply = func(int) provdef.ApplyUpdateResponse {
		return provdef.ApplyUpdateResponse{Action: provdef.ApplyUpdateActionDiscontinue}
	}
	h2.r.performUpdateQuery(context.Background())
	if _, err := os.Stat(h2.r.downloadPath()); !os.IsNotExist(err) || len(h2.applier.applied) != 0 || h2.r.inProgress != nil {
		t.Fatal("Discontinue")
	}
}

// TestOTARequestor_ApplyFailure: a failing applyUpdate forgets the update
// and returns to Idle with Failure (:1273-1281).
func TestOTARequestor_ApplyFailure(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	h.applier.applyErr = errors.New("flash failed")
	h.r.performUpdateQuery(context.Background())
	tr := h.events.transitions()
	if l := tr[len(tr)-1]; l.PreviousState != otadef.UpdateStateApplying || l.Reason != otadef.ChangeReasonFailure || h.r.inProgress != nil {
		t.Fatalf("last %+v, in progress %+v", l, h.r.inProgress)
	}
	// Without an applier the default throws (:1293-1295).
	h2 := newOtaHarness(t, func(c *OTARequestorConfig) { c.Applier = nil })
	h2.addActive(t)
	h2.r.performUpdateQuery(context.Background())
	if tr := h2.events.transitions(); tr[len(tr)-1].Reason != otadef.ChangeReasonFailure {
		t.Fatal("no applier")
	}
}

// TestOTARequestor_FileAlreadyDownloaded: a valid newer file is applied
// without asking a provider; an invalid one is deleted (:591-610).
func TestOTARequestor_FileAlreadyDownloaded(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	if err := os.WriteFile(h.r.downloadPath(), h.image(2), 0o600); err != nil {
		t.Fatal(err)
	}
	h.r.performUpdateQuery(context.Background())
	if len(h.provider.queries) != 0 || len(h.applier.applied) != 1 || h.r.inProgress.Location != nil {
		t.Fatalf("queries %d applied %v", len(h.provider.queries), h.applier.applied)
	}
	h2 := newOtaHarness(t, nil)
	h2.addActive(t)
	if err := os.WriteFile(h2.r.downloadPath(), h2.image(1), 0o600); err != nil {
		t.Fatal(err)
	}
	h2.provider.answer = func(int) (provdef.QueryImageResponse, error) {
		return provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}, nil
	}
	h2.r.performUpdateQuery(context.Background())
	if _, err := os.Stat(h2.r.downloadPath()); !os.IsNotExist(err) || len(h2.provider.queries) != 1 {
		t.Fatal("stale file kept")
	}
}

// TestOTARequestor_QueryTimerFires: the scheduled timer runs the query, and
// a timer that fired stays set (:365, the absence test of
// #handleUpdatedDefaultOtaProviders); no query while Applying (:586-589).
func TestOTARequestor_QueryTimerFires(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	h.provider.answer = func(int) (provdef.QueryImageResponse, error) {
		return provdef.QueryImageResponse{Status: provdef.StatusNotAvailable}, nil
	}
	d := time.Minute
	h.r.scheduleUpdateQuery(&d, reasonNewQuery, nil)
	h.clock.advance(time.Minute)
	h.r.wg.Wait()
	if len(h.provider.queries) != 1 {
		t.Fatalf("%d queries", len(h.provider.queries))
	}
	h.r.mu.Lock()
	h.r.state = otadef.UpdateStateApplying
	h.r.mu.Unlock()
	h.r.performUpdateQuery(context.Background())
	if len(h.provider.queries) != 1 {
		t.Fatal("query while Applying")
	}
	// A schedule without delay outside Idle is refused (:508-513).
	h.r.mu.Lock()
	h.r.timer = nil
	h.r.mu.Unlock()
	if h.r.scheduleUpdateQuery(nil, reasonNewQuery, nil) {
		t.Fatal("scheduled outside Idle")
	}
	// Commissioned: 30 s (:251-253).
	h.r.mu.Lock()
	h.r.state = otadef.UpdateStateIdle
	h.r.mu.Unlock()
	h.r.Commissioned()
	if got := h.clock.pending(); !slices.Contains(got, 30*time.Second) {
		t.Fatalf("pending %v", got)
	}
}

// TestOTARequestor_UpdateStateRules: #updateState (:1164-1204) carries the
// target only into Downloading, Applying and RollingBack, emits nothing
// without a change, and resets the progress.
func TestOTARequestor_UpdateStateRules(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	v := uint32(7)
	h.r.updateState(otadef.UpdateStateQuerying, otadef.ChangeReasonSuccess, &v)
	h.r.updateState(otadef.UpdateStateQuerying, otadef.ChangeReasonSuccess, nil)
	tr := h.events.transitions()
	if len(tr) != 1 || !tr[0].TargetSoftwareVersion.Null {
		t.Fatalf("%+v", tr)
	}
	ten := uint8(10)
	h.r.setProgress(&ten)
	h.r.updateState(otadef.UpdateStateDownloading, otadef.ChangeReasonSuccess, &v)
	if p, _ := h.r.MatterRead(otadef.AttrUpdateStateProgress); p != uint8(0) {
		t.Fatalf("progress %v", p)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Applying without a target accepted")
		}
	}()
	h.r.updateState(otadef.UpdateStateApplying, otadef.ChangeReasonSuccess, nil)
}

// TestOTARequestor_UpdateProgress: #updateProgress (:1064-1070).
func TestOTARequestor_UpdateProgress(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	total := uint64(1000)
	h.r.updateProgress(500, &total)
	if p, _ := h.r.MatterRead(otadef.AttrUpdateStateProgress); p != uint8(49) {
		t.Fatalf("%v", p)
	}
	h.r.updateProgress(1_000_000, nil)
	if p, _ := h.r.MatterRead(otadef.AttrUpdateStateProgress); p != uint8(62) {
		t.Fatalf("%v (99*(1-e^-1) = 62.58)", p)
	}
}

// TestOTARequestor_Persistence: the active providers and the update in
// progress are kept (:1354, :1362) and restored by Start.
func TestOTARequestor_Persistence(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	h.r.markActiveOtaProviderNoUpdate(context.Background(), otadef.ProviderLocation{FabricIndex: 1, ProviderNodeId: testProvider})
	h.r.prepareApplyUpdate(context.Background(), 2, h.r.downloadPath(), nil, []byte("token-12345678"))

	r2, err := NewOTASoftwareUpdateRequestor(OTARequestorConfig{
		NodeInformation: h.r.cfg.NodeInformation, DownloadDir: h.dir, Store: h.store, TimeSource: h.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Stop()
	if err := r2.load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r2.active) != 1 || r2.active[0].PreviousQueryTimestamp == nil || r2.inProgress == nil || r2.inProgress.NewSoftwareVersion != 2 || string(r2.inProgress.UpdateToken) != "token-12345678" {
		t.Fatalf("restored %+v %+v", r2.active, r2.inProgress)
	}
}

// TestOTARequestor_ReadsAndGlobals: the remaining read surface.
func TestOTARequestor_ReadsAndGlobals(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	if v, ok := h.r.MatterRead(cluster.AttrGlobalClusterRevision); !ok || v != uint16(1) {
		t.Fatalf("%v", v)
	}
	if _, ok := h.r.MatterRead(0x99); ok {
		t.Fatal("unknown attribute")
	}
	if v, _ := h.r.MatterReadFiltered(context.Background(), otadef.AttrUpdateState); v != uint8(otadef.UpdateStateIdle) {
		t.Fatalf("%v", v)
	}
	h.r.SetEndpoint(0)
	before := h.r.MatterDataVersion()
	h.r.SetUpdatePossible(false)
	if h.r.MatterDataVersion() == before {
		t.Fatal("UpdatePossible change did not bump the data version")
	}
}

// TestOTARequestor_BusyReportsDelayedOnQuery pins TC-SU-2.2 step 1 on the
// cluster side: a Busy answer with DelayedActionTime 60 s moves UpdateState
// Querying → DelayedOnQuery (DelayByProvider) and names UpdateState to the
// change listeners at each move, with the data version bumped first; the
// retry is floored at minimumQueryInterval, 120 s
// (OtaSoftwareUpdateRequestorServer.ts :674-684), which is the case's "no
// QueryImage before 120 s". The bridge half — a root listener reaching the
// subscription — is bridge/attribute_notifier_test.go
// TestAttributeChangeNotifierOnRootIsReported.
func TestOTARequestor_BusyReportsDelayedOnQuery(t *testing.T) {
	t.Parallel()
	h := newOtaHarness(t, nil)
	h.addActive(t)
	h.provider.answer = func(int) (provdef.QueryImageResponse, error) {
		d := uint32(60)
		return provdef.QueryImageResponse{Status: provdef.StatusBusy, DelayedActionTime: &d}, nil
	}
	var (
		mu       sync.Mutex
		reported []uint8
		versions []uint32
	)
	unsub := h.r.OnMatterAttributesChanged(func(attrs []uint32) {
		if !slices.Contains(attrs, otadef.AttrUpdateState) {
			return
		}
		v, _ := h.r.MatterRead(otadef.AttrUpdateState)
		mu.Lock()
		reported = append(reported, v.(uint8))
		versions = append(versions, h.r.MatterDataVersion())
		mu.Unlock()
	})
	defer unsub()
	before := h.r.MatterDataVersion()

	h.r.performUpdateQuery(context.Background())

	mu.Lock()
	defer mu.Unlock()
	want := []uint8{uint8(otadef.UpdateStateQuerying), uint8(otadef.UpdateStateDelayedOnQuery)}
	if !slices.Equal(reported, want) {
		t.Fatalf("reported UpdateState %v, want %v", reported, want)
	}
	if versions[0] == before || versions[1] == versions[0] {
		t.Fatalf("data versions %v (before %d) not bumped per change", versions, before)
	}
	if got := h.clock.pending(); len(got) != 1 || got[0] != 120*time.Second {
		t.Fatalf("pending %v, want the 120 s floor over the provider's 60 s", got)
	}
	tr := h.events.transitions()
	if l := tr[len(tr)-1]; l.NewState != otadef.UpdateStateDelayedOnQuery || l.Reason != otadef.ChangeReasonDelayByProvider {
		t.Fatalf("last transition %+v", l)
	}
}

// reliablePipe is a pipe that states its channel.
type reliablePipe struct {
	pipe
	reliable bool
}

func (p *reliablePipe) Reliable() bool { return p.reliable }

// TestOtaRequestorBdxBlockSizeByChannel: 8192-byte blocks over a TCP
// channel, 1024 otherwise — an exchange that does not state its channel
// counts as not TCP (OtaSoftwareUpdateRequestorServer.ts:54-55, :962-963).
func TestOtaRequestorBdxBlockSizeByChannel(t *testing.T) {
	for _, tc := range []struct {
		name string
		ex   bdx.Exchange
		want uint16
	}{
		{"unstated", &pipe{}, 1024},
		{"udp", &reliablePipe{}, 1024},
		{"tcp", &reliablePipe{reliable: true}, 8192},
	} {
		if got := bdxMaxBlockSize(tc.ex); got != tc.want {
			t.Errorf("%s: block size %d, want %d", tc.name, got, tc.want)
		}
	}
}
