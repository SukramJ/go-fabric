// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// Timing of the scanner. Mirrors matter.js
// packages/protocol/src/common/BleScanner.ts:92-115 (STALE_ENTRY_AGE,
// RECORD_MAX_AGE) and the default discovery timeout of
// findCommissionableDevices (:543-546) at 0ceb202d.
const (
	// StaleEntryAge is the silence after which a record is dropped once
	// its service data arrives from another address, and after which it
	// is no candidate when the client reports listening time.
	StaleEntryAge = 60 * time.Second
	// RecordMaxAge is the age after which a record is forgotten
	// (§5.4.2.3: the longest a device announces at the rapid interval).
	RecordMaxAge = 15 * time.Minute
	// DefaultDiscoveryTimeout is [Scanner.Find]'s timeout when none is given.
	DefaultDiscoveryTimeout = 10 * time.Second
)

// ErrScannerClosed is returned by a discovery on a closed scanner.
var ErrScannerClosed = errors.New("ble: scanner closed")

// ErrNoDevice is [Scanner.Device] without a usable record — matter.js's
// BleError of getDiscoveredDevice (common/BleScanner.ts:145-154).
var ErrNoDevice = errors.New("ble: no device")

// Device is a commissionable device a BLE advertisement describes —
// matter.js CommissionableDeviceData with its DiscoveredBleDevice
// (common/BleScanner.ts:66-75, built at :388-399).
type Device struct {
	// Peripheral is the advertising peripheral; its address is the
	// device identifier.
	Peripheral Peripheral
	// Discriminator is the 16-bit discriminator field, unmasked as
	// matter.js reads it (D).
	Discriminator uint16
	// VendorID and ProductID are the advertisement's (VP), 0 in an
	// extended announcement.
	VendorID, ProductID uint16
	// HasAdditionalAdvertisementData is the flag of C3 data.
	HasAdditionalAdvertisementData bool
}

// ShortDiscriminator is the discriminator's upper four bits (SD, :394).
func (d Device) ShortDiscriminator() uint8 { return uint8((d.Discriminator >> 8) & 0x0f) } //nolint:gosec // 4 bits

// Commissionable is the device as discovery describes one: the
// peripheral's address as instance id, D, VP, CM 1 (:391-397).
func (d Device) Commissionable() discovery.Device {
	vp := strconv.Itoa(int(d.VendorID)) + "+" + strconv.Itoa(int(d.ProductID))
	dd := discovery.Device{InstanceID: d.Peripheral.Address, D: d.Discriminator, CM: 1}
	dd.VP = &vp
	return dd
}

// record is a StoredDiscoveredBleDevice (:83-91).
type record struct {
	device         Device
	serviceDataHex string
	lastSeen       time.Time
	seenAt         time.Duration
	hasSeenAt      bool
}

// waiter is one discovery waiting for an advertisement it selects (:77-82).
type waiter struct {
	id       discovery.Identifier
	wake     chan struct{}
	canceled bool
	err      error
}

// ScannerOptions are a [Scanner]'s clock and logger; the zero value is
// the system clock and no log.
type ScannerOptions struct {
	Clock  exchange.Clock
	Logger *slog.Logger
}

// Scanner finds commissionable devices by their BLE advertisements —
// matter.js BleScanner (BleScanner.ts:124-674) at 0ceb202d. One radio
// serves every discovery: the scan starts with the first and stops with
// the last. It is safe for concurrent use.
type Scanner struct {
	client Central
	clock  exchange.Clock
	log    *slog.Logger

	// scanMu serializes the client's scan transitions, so no start and
	// stop overlap (#enqueueScanTransition, :160-172).
	scanMu sync.Mutex

	mu                sync.Mutex
	records           map[string]*record
	waiters           map[*waiter]struct{}
	activeDiscoveries int
	scanning          bool
	closed            bool
	lastScanEndedAt   time.Time
}

// NewScanner returns a scanner over client and installs its discovery
// callback.
func NewScanner(client Central, opts ScannerOptions) *Scanner {
	if opts.Clock == nil {
		opts.Clock = exchange.SystemClock{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Scanner{
		client: client, clock: opts.Clock, log: opts.Logger,
		records: map[string]*record{}, waiters: map[*waiter]struct{}{},
	}
	client.SetDiscoveryCallback(s.handleDiscovered)
	return s
}

func (s *Scanner) listeningTime() (time.Duration, bool) {
	if c, ok := s.client.(ListeningClock); ok {
		return c.ListeningTime(), true
	}
	return 0, false
}

func (s *Scanner) isReachable(address string) bool {
	if r, ok := s.client.(Reachability); ok {
		return r.IsPeripheralReachable(address)
	}
	return true
}

// handleDiscovered is #handleDiscoveredDevice (:384-441): the service
// data decoded, aged-out records forgotten, a record of another address
// with the same service data and silent past StaleEntryAge dropped (the
// device rotated its address), the record stored, and — when the
// peripheral is reachable — every discovery it answers woken.
func (s *Scanner) handleDiscovered(p Peripheral, serviceData []byte) {
	adv, err := btp.DecodeAdvertisementServiceData(serviceData)
	if err != nil {
		s.log.Debug("matter.ble.scan.not_matter", slog.String("address", p.Address), slog.String("err", err.Error()))
		return
	}
	dev := Device{Peripheral: p, Discriminator: adv.Discriminator, VendorID: adv.VendorID, ProductID: adv.ProductID, HasAdditionalAdvertisementData: adv.HasAdditionalAdvertisementData}
	listening, hasListening := s.listeningTime()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgetAgedOutLocked(listening, hasListening)
	dataHex := hex.EncodeToString(serviceData)
	for addr, other := range s.records {
		if addr == p.Address || other.serviceDataHex != dataHex {
			continue
		}
		if s.silenceOfLocked(other, listening, hasListening) <= StaleEntryAge {
			continue
		}
		s.log.Debug("matter.ble.scan.rotated", slog.String("dropped", addr), slog.String("address", p.Address))
		delete(s.records, addr)
	}
	s.records[p.Address] = &record{device: dev, serviceDataHex: dataHex, lastSeen: s.clock.Now(), seenAt: listening, hasSeenAt: hasListening}
	if !s.isReachable(p.Address) {
		return
	}
	dd := dev.Commissionable()
	for w := range s.waiters {
		if w.id.MatchesBLE(&dd) {
			s.finishLocked(w, nil)
		}
	}
}

// silenceOfLocked is #silenceOf (:253-258).
func (s *Scanner) silenceOfLocked(r *record, listening time.Duration, hasListening bool) time.Duration {
	if hasListening && r.hasSeenAt {
		return listening - r.seenAt
	}
	return s.clock.Now().Sub(r.lastSeen)
}

// isStaleLocked is #isStale (:260-270): only listening time can tell
// silence from nobody listening.
func (s *Scanner) isStaleLocked(r *record, listening time.Duration, hasListening bool) bool {
	if !hasListening || !r.hasSeenAt {
		return false
	}
	return s.silenceOfLocked(r, listening, hasListening) > StaleEntryAge
}

// ageOfLocked is #ageOf (:272-287).
func (s *Scanner) ageOfLocked(r *record, listening time.Duration, hasListening bool) time.Duration {
	if hasListening && r.hasSeenAt {
		return s.silenceOfLocked(r, listening, hasListening)
	}
	if s.activeDiscoveries > 0 {
		return 0
	}
	since := r.lastSeen
	if s.lastScanEndedAt.After(since) {
		since = s.lastScanEndedAt
	}
	return s.clock.Now().Sub(since)
}

// forgetAgedOutLocked is #forgetAgedOut (:289-297).
func (s *Scanner) forgetAgedOutLocked(listening time.Duration, hasListening bool) {
	for addr, r := range s.records {
		if age := s.ageOfLocked(r, listening, hasListening); age > RecordMaxAge {
			s.log.Debug("matter.ble.scan.forget", slog.String("address", addr), slog.Duration("age", age))
			delete(s.records, addr)
		}
	}
}

// devicesLocked is #getCommissionableDevices (:497-541): the records the
// identifier selects, newest first; unreachable and stale ones only with
// includeUnavailable.
func (s *Scanner) devicesLocked(id discovery.Identifier, includeUnavailable bool) []*record {
	listening, hasListening := s.listeningTime()
	s.forgetAgedOutLocked(listening, hasListening)
	var out []*record
	for _, r := range s.records {
		if !includeUnavailable && (!s.isReachable(r.device.Peripheral.Address) || s.isStaleLocked(r, listening, hasListening)) {
			continue
		}
		dd := r.device.Commissionable()
		if id.MatchesBLE(&dd) {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b *record) int { return b.lastSeen.Compare(a.lastSeen) })
	return out
}

func devicesOf(rs []*record) []Device {
	out := make([]Device, len(rs))
	for i, r := range rs {
		out[i] = r.device
	}
	return out
}

// Discovered is getDiscoveredCommissionableDevices (:652-654): the
// stored devices the identifier selects, newest first.
func (s *Scanner) Discovered(id discovery.Identifier) []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return devicesOf(s.devicesLocked(id, false))
}

// Device is the stored record of a peripheral — getDiscoveredDevice
// (common/BleScanner.ts:145-154): an error when there is none or the client
// cannot reach the peripheral now.
func (s *Scanner) Device(address string) (Device, error) {
	s.mu.Lock()
	r, ok := s.records[address]
	s.mu.Unlock()
	if !ok {
		return Device{}, fmt.Errorf("%w: no device found for address %s", ErrNoDevice, address)
	}
	if !s.isReachable(address) {
		return Device{}, fmt.Errorf("%w: device with address %s is currently not reachable", ErrNoDevice, address)
	}
	return r.device, nil
}

// reconcileScan is #reconcileScan (:174-211): the client driven to the
// scan the discoveries need; a failed start fails every waiting
// discovery and is returned to a starting caller.
func (s *Scanner) reconcileScan(ctx context.Context, starting bool) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	s.mu.Lock()
	wanted := s.activeDiscoveries > 0 && !s.closed
	scanning := s.scanning
	s.mu.Unlock()
	var err error
	switch {
	case wanted:
		err = s.client.StartScanning(ctx)
	case scanning:
		err = s.client.StopScanning(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.scanning = wanted
		return nil
	}
	// The client may scan or not after a failed call (:192-193).
	s.scanning = true
	if wanted {
		for w := range s.waiters {
			s.finishLocked(w, err)
		}
	} else {
		s.log.Warn("matter.ble.scan.stop_failed", slog.String("err", err.Error()))
	}
	if starting {
		return err
	}
	return nil
}

// startDiscovering is #startDiscovering (:213-225).
func (s *Scanner) startDiscovering(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrScannerClosed
	}
	s.activeDiscoveries++
	s.mu.Unlock()
	if err := s.reconcileScan(ctx, true); err != nil {
		s.mu.Lock()
		s.endDiscoveryLocked()
		last := s.activeDiscoveries == 0
		s.mu.Unlock()
		if last {
			_ = s.reconcileScan(ctx, false)
		}
		return err
	}
	return nil
}

// stopDiscovering is #stopDiscovering (:227-230).
func (s *Scanner) stopDiscovering(ctx context.Context) {
	s.mu.Lock()
	s.endDiscoveryLocked()
	s.mu.Unlock()
	_ = s.reconcileScan(context.WithoutCancel(ctx), false)
}

func (s *Scanner) endDiscoveryLocked() {
	s.activeDiscoveries--
	if s.activeDiscoveries == 0 {
		s.lastScanEndedAt = s.clock.Now()
	}
}

func (s *Scanner) addWaiterLocked(id discovery.Identifier) *waiter {
	w := &waiter{id: id, wake: make(chan struct{}, 1)}
	s.waiters[w] = struct{}{}
	return w
}

// finishLocked is #finishWaiter (:352-366): the waiter removed and woken,
// with err when the scan failed.
func (s *Scanner) finishLocked(w *waiter, err error) {
	if _, ok := s.waiters[w]; !ok {
		return
	}
	delete(s.waiters, w)
	w.err = err
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// wait blocks until w is woken, ctx ends or timeout (0: none) elapses.
// A timeout and ctx end the waiter alone (:326-345).
func (s *Scanner) wait(ctx context.Context, w *waiter, timeout time.Duration) error {
	var expired chan struct{}
	if timeout > 0 {
		expired = make(chan struct{})
		t := s.clock.AfterFunc(timeout, func() { close(expired) })
		defer t.Stop()
	}
	select {
	case <-w.wake:
		s.mu.Lock()
		defer s.mu.Unlock()
		return w.err
	case <-expired:
	case <-ctx.Done():
	}
	s.mu.Lock()
	s.finishLocked(w, nil)
	s.mu.Unlock()
	return nil
}

// Find is findCommissionableDevices (:543-575): the stored devices the
// identifier selects, else a scan until the first advertisement that
// selects one or timeout (0 is [DefaultDiscoveryTimeout]).
// ignoreExisting forgets the matching records first, for a fresh
// result. An identifier BLE cannot select by returns none.
func (s *Scanner) Find(ctx context.Context, id discovery.Identifier, timeout time.Duration, ignoreExisting bool) ([]Device, error) {
	if !id.SupportedOverBLE() {
		return nil, nil
	}
	if timeout == 0 {
		timeout = DefaultDiscoveryTimeout
	}
	s.mu.Lock()
	if ignoreExisting {
		for _, r := range s.devicesLocked(id, true) {
			delete(s.records, r.device.Peripheral.Address)
		}
	}
	var found []*record
	if !ignoreExisting {
		found = s.devicesLocked(id, false)
	}
	s.mu.Unlock()
	if len(found) > 0 {
		return devicesOf(found), nil
	}
	if err := s.startDiscovering(ctx); err != nil {
		return nil, err
	}
	defer s.stopDiscovering(ctx)
	s.mu.Lock()
	// An advertisement may have arrived while the scan started (:563-564).
	found = s.devicesLocked(id, false)
	if len(found) > 0 || s.closed {
		s.mu.Unlock()
		return devicesOf(found), nil
	}
	w := s.addWaiterLocked(id)
	s.mu.Unlock()
	if err := s.wait(ctx, w, timeout); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return devicesOf(s.devicesLocked(id, false)), nil
}

// FindContinuously is findCommissionableDevicesContinuously (:577-650):
// callback receives every device the identifier selects, once per
// peripheral, until timeout (0: none), ctx ends, [Scanner.Cancel] or
// [Scanner.Close]; it returns the devices then stored.
func (s *Scanner) FindContinuously(ctx context.Context, id discovery.Identifier, callback func(Device), timeout time.Duration) ([]Device, error) {
	if !id.SupportedOverBLE() {
		return nil, nil
	}
	var end time.Time
	if timeout > 0 {
		end = s.clock.Now().Add(timeout)
	}
	if err := s.startDiscovering(ctx); err != nil {
		return nil, err
	}
	defer s.stopDiscovering(ctx)
	seen := map[string]bool{}
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			break
		}
		news := devicesOf(s.devicesLocked(id, false))
		s.mu.Unlock()
		for _, d := range news {
			if !seen[d.Peripheral.Address] {
				seen[d.Peripheral.Address] = true
				callback(d)
			}
		}
		var remaining time.Duration
		if !end.IsZero() {
			if remaining = end.Sub(s.clock.Now()); remaining <= 0 {
				break
			}
		}
		if ctx.Err() != nil {
			break
		}
		s.mu.Lock()
		w := s.addWaiterLocked(id)
		s.mu.Unlock()
		if err := s.wait(ctx, w, remaining); err != nil {
			return nil, err
		}
		s.mu.Lock()
		canceled := w.canceled
		s.mu.Unlock()
		if canceled {
			break
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return devicesOf(s.devicesLocked(id, false)), nil
}

// Cancel ends every discovery waiting for the identifier
// (cancelCommissionableDeviceDiscovery, :374-382).
func (s *Scanner) Cancel(id discovery.Identifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for w := range s.waiters {
		if w.id == id {
			w.canceled = true
			s.finishLocked(w, nil)
		}
	}
}

// Close stops the scan and ends every discovery (close, :660-673).
func (s *Scanner) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.scanMu.Lock()
	err := s.client.StopScanning(ctx)
	s.scanMu.Unlock()
	s.mu.Lock()
	s.scanning = false
	for w := range s.waiters {
		w.canceled = true
		s.finishLocked(w, nil)
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("ble: stop scanning: %w", err)
	}
	return nil
}
