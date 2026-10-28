// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// VendorInfo is one vendor — matter.js VendorInfo
// (packages/protocol/src/dcl/DclVendorInfoService.ts:41-48 at 862ad75f):
// the ledger's vendor record normalised, an unset preferred name or
// landing page "" (#normalizeDclVendor, :390-401).
type VendorInfo struct {
	VendorID             uint16 `json:"vendorId"`
	VendorName           string `json:"vendorName"`
	CompanyLegalName     string `json:"companyLegalName"`
	CompanyPreferredName string `json:"companyPreferredName"`
	VendorLandingPageURL string `json:"vendorLandingPageUrl"`
	Creator              string `json:"creator"`
}

// ProductInfo is a product as a commissioning UI shows it — matter.js
// ProductInfo (DclVendorInfoService.ts:54-135): the device model of the
// ledger with its bitmaps kept as the ledger states them (matter.js decodes
// them into flag objects) and the maintenance URL under its ProductInfo
// name. An unset URL is "", an unset revision 0.
type ProductInfo struct {
	DeviceTypeID                               uint32
	ProductName                                string
	ProductLabel                               string
	PartNumber                                 string
	DiscoveryCapabilities                      uint32
	CommissioningFlow                          uint8
	CommissioningCustomFlowURL                 string
	CommissioningModeInitialStepsHint          uint32
	CommissioningModeInitialStepsInstruction   string
	CommissioningModeSecondaryStepsHint        uint32
	CommissioningModeSecondaryStepsInstruction string
	CommissioningFallbackURL                   string
	UserManualURL                              string
	SupportURL                                 string
	ProductURL                                 string
	LSFURL                                     string
	LSFRevision                                uint32
	EnhancedSetupFlowOptions                   uint32
	EnhancedSetupFlowTCURL                     string
	EnhancedSetupFlowTCRevision                uint32
	EnhancedSetupFlowMaintenanceURL            string
}

// VendorSeed is a vendor list bundled with the program — matter.js
// SeedSource of VendorEntry (packages/protocol/src/dcl/SeedTypes.ts):
// controller/dcl/snapshot's vendor file (Snapshot.VendorSeed).
type VendorSeed struct {
	BuiltAt       string
	ExpectedCount int
	Vendors       []VendorInfo
}

// staticVendors are the vendors known without the ledger — STATIC_VENDORS
// (DclVendorInfoService.ts:136-158): the specification's test vendor ids
// 0xFFF1–0xFFF4 and Nabu Casa.
func staticVendors() []VendorInfo {
	out := make([]VendorInfo, 0, 5)
	for _, id := range []uint16{0xFFF1, 0xFFF2, 0xFFF3, 0xFFF4} {
		out = append(out, VendorInfo{
			VendorID: id, VendorName: "Test Vendor", CompanyLegalName: "Test Vendor Inc.", CompanyPreferredName: "Test Vendor",
			VendorLandingPageURL: "https://test.example.com", Creator: "System",
		})
	}
	return append(out, VendorInfo{
		VendorID: 4939, VendorName: "Nabu Casa", CompanyLegalName: "Nabu Casa, Inc.", CompanyPreferredName: "Nabu Casa",
		VendorLandingPageURL: "https://www.nabucasa.com", Creator: "System",
	})
}

// DefaultVendorUpdateInterval is the period of the vendor refresh —
// matter.js's Days.one (DclVendorInfoService.ts:194-198).
const DefaultVendorUpdateInterval = 24 * time.Hour

// VendorInfoOptions configure a [VendorInfoService] — matter.js
// DclVendorInfoService.Options (DclVendorInfoService.ts:470-496).
type VendorInfoOptions struct {
	// Client is the ledger; nil is the production ledger (DclConfig.production).
	Client *Client
	// Store keeps the fetched vendors across runs — matter.js's "vendors"
	// storage context "info" (:184-186); nil is a [MemoryStore].
	Store Store
	// UpdateInterval is the period of the refresh; 0 is
	// [DefaultVendorUpdateInterval], negative disables it (matter.js's null).
	UpdateInterval time.Duration
	// Offline never asks the ledger: the service serves the stored, seeded
	// and static vendors and answers every product as absent (:214-216,
	// :238-241, :264-267).
	Offline bool
	// Seed, when set, replaces a store holding fewer vendors than its
	// ExpectedCount (#consumeVendorSeed, :405-456).
	Seed *VendorSeed
	// Clock paces the refresh; nil is the system clock.
	Clock clock.Clock
	// Logger receives the diagnostics; nil discards.
	Logger *slog.Logger
}

// VendorUpdate is what an observer of a [VendorInfoService] learns after a
// refresh: the vendors the ledger answered, or the refresh's error.
type VendorUpdate struct {
	Vendors int
	Err     error
}

// VendorInfoService resolves vendor ids to vendor information and vendor
// and product ids to product information — matter.js DclVendorInfoService
// (DclVendorInfoService.ts:166-468). Its vendors are, in order of
// precedence, the ledger's last answer (kept in its Store), a seed for an
// emptier store, and the static vendors; it refreshes from the ledger at
// once and every UpdateInterval unless offline. It is safe for concurrent
// use.
type VendorInfoService struct {
	opts   VendorInfoOptions
	client *Client
	store  Store
	log    *slog.Logger

	mu     sync.Mutex
	index  map[uint16]VendorInfo
	closed bool
	ticker clock.Ticker
	stop   chan struct{}
	loop   sync.WaitGroup

	fetchMu  sync.Mutex // one fetch at a time (#fetchPromise, :243-256)
	fetching chan struct{}

	obsMu     sync.Mutex
	observers map[int]func(VendorUpdate)
	nextObs   int
}

// NewVendorInfoService builds the service — the construction of
// DclVendorInfoService (:176-212): the stored vendors, then the seed when
// the store holds fewer than its ExpectedCount, then the static vendors
// for every id still unknown, then a refresh (unless offline) and, unless
// offline or disabled, the periodic refresh. A failed refresh is logged;
// a store that does not load is the error.
func NewVendorInfoService(ctx context.Context, opts VendorInfoOptions) (*VendorInfoService, error) {
	s := &VendorInfoService{opts: opts, client: opts.Client, store: opts.Store, log: opts.Logger, index: map[uint16]VendorInfo{}, stop: make(chan struct{})}
	if s.client == nil {
		s.client = NewClient(ProductionURL)
	}
	if s.store == nil {
		s.store = &MemoryStore{}
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.opts.Clock == nil {
		s.opts.Clock = clock.System{}
	}
	interval := opts.UpdateInterval
	if interval == 0 {
		interval = DefaultVendorUpdateInterval
	}
	s.log.Info("matter.controller.dcl.vendor_info.initialize", slog.String("source", s.client.baseURL()),
		slog.String("interval", intervalString(interval)), slog.Bool("offline", opts.Offline))
	if err := s.loadVendors(ctx); err != nil {
		return nil, err
	}
	if opts.Seed != nil {
		if err := s.consumeSeed(ctx, *opts.Seed); err != nil {
			return nil, err
		}
	}
	s.addStaticVendors()
	_ = s.Update(ctx)
	if !opts.Offline && interval > 0 {
		s.mu.Lock()
		if !s.closed {
			s.ticker = s.opts.Clock.NewTicker(interval)
			s.loop.Go(func() { s.run(s.ticker) })
		}
		s.mu.Unlock()
	}
	return s, nil
}

func intervalString(d time.Duration) string {
	if d < 0 {
		return "disabled"
	}
	return d.String()
}

func (s *VendorInfoService) run(t clock.Ticker) {
	for {
		select {
		case <-s.stop:
			return
		case <-t.C():
			_ = s.Update(context.Background())
		}
	}
}

// Offline reports whether the service never asks the ledger (:214-216).
func (s *VendorInfoService) Offline() bool { return s.opts.Offline }

// InfoFor is the vendor of id; false when the service knows none
// (infoFor, :221-224).
func (s *VendorInfoService) InfoFor(id uint16) (VendorInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.index[id]
	return v, ok
}

// Vendors is every vendor the service knows, in id order (the vendors
// getter, :228-231).
func (s *VendorInfoService) Vendors() []VendorInfo {
	s.mu.Lock()
	out := make([]VendorInfo, 0, len(s.index))
	for _, v := range s.index {
		out = append(out, v)
	}
	s.mu.Unlock()
	slices.SortFunc(out, func(a, b VendorInfo) int { return int(a.VendorID) - int(b.VendorID) })
	return out
}

// Update refreshes the vendors from the ledger and stores what it answered
// — update with #fetchVendorsFromDcl (:238-262, :369-388): nothing on a
// closed or offline service; a refresh already running is waited for
// instead of a second; a failure is logged and returned, the vendors
// known stay. Each refresh is reported to the observers ([Observe]).
func (s *VendorInfoService) Update(ctx context.Context) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || s.opts.Offline {
		return nil
	}
	s.fetchMu.Lock()
	if wait := s.fetching; wait != nil {
		s.fetchMu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
	done := make(chan struct{})
	s.fetching = done
	s.fetchMu.Unlock()
	n, err := s.fetchVendors(ctx)
	s.fetchMu.Lock()
	s.fetching = nil
	close(done)
	s.fetchMu.Unlock()
	if err != nil {
		s.log.Info("matter.controller.dcl.vendor_info.update_failed", slog.Any("err", err))
	}
	s.notify(VendorUpdate{Vendors: n, Err: err})
	return err
}

func (s *VendorInfoService) fetchVendors(ctx context.Context) (int, error) {
	s.log.Info("matter.controller.dcl.vendor_info.fetching")
	vendors, err := s.client.Vendors(ctx)
	if err != nil {
		return 0, err
	}
	stored := make([]VendorInfo, 0, len(vendors))
	s.mu.Lock()
	for _, v := range vendors {
		info := VendorInfo{
			VendorID: v.VendorID, VendorName: v.VendorName, CompanyLegalName: v.CompanyLegalName,
			CompanyPreferredName: v.CompanyPreferredName, VendorLandingPageURL: v.VendorLandingPageURL, Creator: v.Creator,
		}
		s.index[info.VendorID] = info
		stored = append(stored, info)
	}
	s.mu.Unlock()
	if err := s.save(ctx, stored); err != nil {
		return len(vendors), err
	}
	s.log.Info("matter.controller.dcl.vendor_info.stored", slog.Int("vendors", len(stored)))
	return len(vendors), nil
}

// ProductInfoFor is the product vid/pid from the ledger — productInfoFor
// (:264-279) with #normalizeProductModel (:281-342): false on a closed or
// offline service and for a product the ledger does not have (NOT_FOUND);
// another error of the ledger is returned.
func (s *VendorInfoService) ProductInfoFor(ctx context.Context, vid, pid uint16) (ProductInfo, bool, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || s.opts.Offline {
		return ProductInfo{}, false, nil
	}
	m, err := s.client.Model(ctx, vid, pid)
	if IsNotFound(err) {
		s.log.Debug("matter.controller.dcl.vendor_info.product_not_found", slog.Int("vendor", int(vid)), slog.Int("product", int(pid)))
		return ProductInfo{}, false, nil
	}
	if err != nil {
		return ProductInfo{}, false, err
	}
	return ProductInfo{
		DeviceTypeID: m.DeviceTypeID, ProductName: m.ProductName, ProductLabel: m.ProductLabel, PartNumber: m.PartNumber,
		DiscoveryCapabilities: m.DiscoveryCapabilitiesBitmask, CommissioningFlow: m.CommissioningCustomFlow,
		CommissioningCustomFlowURL:                 m.CommissioningCustomFlowURL,
		CommissioningModeInitialStepsHint:          m.CommissioningModeInitialStepsHint,
		CommissioningModeInitialStepsInstruction:   m.CommissioningModeInitialStepsInstruction,
		CommissioningModeSecondaryStepsHint:        m.CommissioningModeSecondaryStepsHint,
		CommissioningModeSecondaryStepsInstruction: m.CommissioningModeSecondaryStepsInstruction,
		CommissioningFallbackURL:                   m.CommissioningFallbackURL, UserManualURL: m.UserManualURL, SupportURL: m.SupportURL,
		ProductURL: m.ProductURL, LSFURL: m.LSFURL, LSFRevision: m.LSFRevision,
		EnhancedSetupFlowOptions: m.EnhancedSetupFlowOptions, EnhancedSetupFlowTCURL: m.EnhancedSetupFlowTCURL,
		EnhancedSetupFlowTCRevision: m.EnhancedSetupFlowTCRevision, EnhancedSetupFlowMaintenanceURL: m.MaintenanceURL,
	}, true, nil
}

// loadVendors is #loadVendors (:344-356).
func (s *VendorInfoService) loadVendors(ctx context.Context) error {
	b, err := s.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("dcl: vendor store: %w", err)
	}
	if len(b) == 0 {
		return nil
	}
	var stored []VendorInfo
	if err := json.Unmarshal(b, &stored); err != nil {
		return fmt.Errorf("dcl: vendor store: %w", err)
	}
	for _, v := range stored {
		s.index[v.VendorID] = v
	}
	s.log.Info("matter.controller.dcl.vendor_info.loaded", slog.Int("vendors", len(s.index)))
	return nil
}

// consumeSeed is #consumeVendorSeed (:405-456): skipped when the store
// already holds ExpectedCount vendors; otherwise the seed replaces the
// index and is stored. The snapshot validated its entries when it loaded.
func (s *VendorInfoService) consumeSeed(ctx context.Context, seed VendorSeed) error {
	if len(s.index) >= seed.ExpectedCount {
		s.log.Info("matter.controller.dcl.vendor_info.seed_skipped", slog.Int("stored", len(s.index)), slog.Int("expected", seed.ExpectedCount))
		return nil
	}
	if len(seed.Vendors) == 0 {
		return nil
	}
	s.index = map[uint16]VendorInfo{}
	for _, v := range seed.Vendors {
		s.index[v.VendorID] = v
	}
	if err := s.save(ctx, seed.Vendors); err != nil {
		return err
	}
	s.log.Info("matter.controller.dcl.vendor_info.seeded", slog.Int("vendors", len(seed.Vendors)), slog.String("built_at", seed.BuiltAt))
	return nil
}

// addStaticVendors is #addStaticVendors (:358-367): a static vendor only
// for an id no record names. The static vendors are never stored.
func (s *VendorInfoService) addStaticVendors() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range staticVendors() {
		if _, ok := s.index[v.VendorID]; !ok {
			s.index[v.VendorID] = v
		}
	}
}

func (s *VendorInfoService) save(ctx context.Context, vendors []VendorInfo) error {
	b, err := json.Marshal(vendors)
	if err != nil {
		return err
	}
	if err := s.store.Save(ctx, b); err != nil {
		return fmt.Errorf("dcl: vendor store: %w", err)
	}
	return nil
}

// Observe calls fn after every refresh from the ledger, until the returned
// function is called. matter.js's service has no observable (its users
// read it); the hook is this module's, for a UI that shows the vendors.
// fn runs on the refresh's goroutine and must not block.
func (s *VendorInfoService) Observe(fn func(VendorUpdate)) (unsubscribe func()) {
	s.obsMu.Lock()
	defer s.obsMu.Unlock()
	if s.observers == nil {
		s.observers = map[int]func(VendorUpdate){}
	}
	id := s.nextObs
	s.nextObs++
	s.observers[id] = fn
	return func() {
		s.obsMu.Lock()
		delete(s.observers, id)
		s.obsMu.Unlock()
	}
}

func (s *VendorInfoService) notify(u VendorUpdate) {
	s.obsMu.Lock()
	fns := make([]func(VendorUpdate), 0, len(s.observers))
	for _, fn := range s.observers {
		fns = append(fns, fn)
	}
	s.obsMu.Unlock()
	for _, fn := range fns {
		fn(u)
	}
}

// Close stops the periodic refresh and waits for a running one — close
// (:458-468). Idempotent; a concurrent Close returns once the first has
// cleaned up.
func (s *VendorInfoService) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.ticker != nil {
			s.ticker.Stop()
		}
		close(s.stop)
	}
	s.mu.Unlock()
	s.loop.Wait()
	s.fetchMu.Lock()
	wait := s.fetching
	s.fetchMu.Unlock()
	if wait != nil {
		<-wait
	}
}
