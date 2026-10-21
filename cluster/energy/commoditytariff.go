// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// CommodityTariff (0x0700). Four chip sources carry its rules, cited below
// by file name and line, all under src/app/clusters/commodity-tariff-server/:
//
//   - Server.cpp — commodity-tariff-server.cpp: the update's cross
//     validation and commit (Delegate::TariffDataUpdate and its helpers),
//     the delayed activation, the derived Current*/Next* attributes and
//     the two commands.
//   - DataMgmt.cpp — CommodityTariffAttrsDataMgmt.cpp: the per-attribute
//     validation of a new value (ValidateNewValue, ValidateListEntry).
//   - DataMgmt.h — CommodityTariffAttrsDataMgmt.h: the update state each
//     attribute's storage goes through (assigned, validated, committed).
//   - Consts.h — CommodityTariffConsts.h: the limits.
//
// The C++ storage scaffolding (double-buffered values, the deep copies,
// the containers) is not ported: a [Tariff] is a Go value, copied on the
// way in.

// ClusterIDCommodityTariff is the CommodityTariff cluster id.
const ClusterIDCommodityTariff = ct.ClusterID

// The CommodityTariff datatypes, the generated ones.
type (
	// TariffFeature is a CommodityTariff FeatureMap bit.
	TariffFeature = ct.Feature
	// TariffInformation is the TariffInformationStruct.
	TariffInformation = ct.TariffInformationStruct
	// TariffDayEntry is the DayEntryStruct.
	TariffDayEntry = ct.DayEntryStruct
	// TariffDayPattern is the DayPatternStruct.
	TariffDayPattern = ct.DayPatternStruct
	// TariffComponent is the TariffComponentStruct.
	TariffComponent = ct.TariffComponentStruct
	// TariffPeriod is the TariffPeriodStruct.
	TariffPeriod = ct.TariffPeriodStruct
	// TariffDay is the DayStruct.
	TariffDay = ct.DayStruct
	// TariffCalendarPeriod is the CalendarPeriodStruct.
	TariffCalendarPeriod = ct.CalendarPeriodStruct
	// TariffUnit is the Globals TariffUnitEnum as CommodityTariff carries
	// it.
	TariffUnit = ct.TariffUnitEnum
	// DayEntryRandomizationType is the DayEntryRandomizationTypeEnum.
	DayEntryRandomizationType = ct.DayEntryRandomizationTypeEnum
)

// CommodityTariff features.
const (
	TariffFeaturePricing        = ct.FeaturePricing        // PRICE
	TariffFeatureFriendlyCredit = ct.FeatureFriendlyCredit // FCRED
	TariffFeatureAuxiliaryLoad  = ct.FeatureAuxiliaryLoad  // AUXLD
	TariffFeaturePeakPeriod     = ct.FeaturePeakPeriod     // PEAKP
	TariffFeaturePowerThreshold = ct.FeaturePowerThreshold // PWRTHLD
	TariffFeatureRandomization  = ct.FeatureRandomization  // RNDM
)

// The limits (Consts.h:24-46).
const (
	tariffMaxString                 = 128 // kDefaultStringValuesMaxBufLength
	tariffMaxListLength             = 672 // kDefaultListAttrMaxLength
	tariffMaxCurrencyValue          = 999
	tariffMaxDayPatterns            = 28
	tariffMaxIndividualDays         = 50
	tariffMaxCalendarPatternIDs     = 7
	tariffMaxDayEntryIDs            = 96
	tariffMaxPeriodItemIDs          = 20
	tariffDayEntryDurationLimit     = 1500
	tariffFullWeekMask              = 0x7f
	tariffSecondsPerDay             = 86400
	tariffDefaultStringValuesMaxBuf = tariffMaxString
)

// The ways a tariff update fails, after the CHIP_ERRORs chip's validation
// returns.
var (
	// ErrTariffInvalid is CHIP_ERROR_INVALID_ARGUMENT: a value a rule
	// refuses.
	ErrTariffInvalid = errors.New("energy: CommodityTariff: invalid tariff value")
	// ErrTariffListLength is CHIP_ERROR_INVALID_LIST_LENGTH: an empty
	// list, or one longer than its limit.
	ErrTariffListLength = errors.New("energy: CommodityTariff: invalid list length")
	// ErrTariffDuplicateID is CHIP_ERROR_DUPLICATE_KEY_ID.
	ErrTariffDuplicateID = errors.New("energy: CommodityTariff: duplicate id")
	// ErrTariffUnknownID is CHIP_ERROR_KEY_NOT_FOUND: a reference to an id
	// the tariff does not carry.
	ErrTariffUnknownID = errors.New("energy: CommodityTariff: reference to an unknown id")
	// ErrTariffNoDays is CHIP_ERROR_INVALID_DATA_LIST: an update with
	// neither IndividualDays nor CalendarPeriods.
	ErrTariffNoDays = errors.New("energy: CommodityTariff: neither IndividualDays nor CalendarPeriods")
	// ErrTariffMissing is a cross-validated attribute the update does not
	// carry (Server.cpp:143-146).
	ErrTariffMissing = errors.New("energy: CommodityTariff: TariffInfo, DayEntries, TariffComponents and TariffPeriods are required")
	// ErrTariffUpdatePending is CHIP_ERROR_INCORRECT_STATE: a new value
	// while a delayed update waits for its StartDate (DataMgmt.h:563-566,
	// :612-615).
	ErrTariffUpdatePending = errors.New("energy: CommodityTariff: a delayed update is pending")
)

// Tariff is the tariff data a host hands to [CommodityTariffServer.UpdateTariff]:
// the eleven attributes chip's delegate manages (Server.cpp:1243-1273). A
// nil field is not part of the update — the attribute keeps its value, as
// a chip data-management object with no new value attached does
// (DataMgmt.h:770-776, :811-818); a non-nil empty list is refused, as
// chip's CreateNewListValue refuses a list of size 0
// (DataMgmt.h:555-588).
type Tariff struct {
	TariffUnit                 *TariffUnit
	StartDate                  *uint32
	DefaultRandomizationOffset *int16
	DefaultRandomizationType   *DayEntryRandomizationType
	TariffInfo                 *TariffInformation
	DayEntries                 []TariffDayEntry
	DayPatterns                []TariffDayPattern
	TariffComponents           []TariffComponent
	TariffPeriods              []TariffPeriod
	IndividualDays             []TariffDay
	CalendarPeriods            []TariffCalendarPeriod
}

// tariffAttrs is the attribute of each Tariff field, in chip's
// CommodityTariffAttrTypeEnum order (commodity-tariff-server.h:103-116),
// the order the update validates and commits them in.
var tariffAttrs = [...]uint32{
	ct.AttrTariffUnit, ct.AttrStartDate, ct.AttrDefaultRandomizationOffset, ct.AttrDefaultRandomizationType,
	ct.AttrTariffInfo, ct.AttrDayEntries, ct.AttrDayPatterns, ct.AttrTariffComponents, ct.AttrTariffPeriods,
	ct.AttrIndividualDays, ct.AttrCalendarPeriods,
}

// values returns each field's attribute value in the generated server's
// stored form (an enum as its uint8): nil for a nil field.
func (t *Tariff) values() [len(tariffAttrs)]any {
	var v [len(tariffAttrs)]any
	if t.TariffUnit != nil {
		v[0] = uint8(*t.TariffUnit)
	}
	if t.StartDate != nil {
		v[1] = *t.StartDate
	}
	if t.DefaultRandomizationOffset != nil {
		v[2] = *t.DefaultRandomizationOffset
	}
	if t.DefaultRandomizationType != nil {
		v[3] = uint8(*t.DefaultRandomizationType)
	}
	if t.TariffInfo != nil {
		v[4] = *t.TariffInfo
	}
	if t.DayEntries != nil {
		v[5] = slices.Clone(t.DayEntries)
	}
	if t.DayPatterns != nil {
		v[6] = slices.Clone(t.DayPatterns)
	}
	if t.TariffComponents != nil {
		v[7] = slices.Clone(t.TariffComponents)
	}
	if t.TariffPeriods != nil {
		v[8] = slices.Clone(t.TariffPeriods)
	}
	if t.IndividualDays != nil {
		v[9] = slices.Clone(t.IndividualDays)
	}
	if t.CalendarPeriods != nil {
		v[10] = slices.Clone(t.CalendarPeriods)
	}
	return v
}

// CommodityTariffConfig carries the construction parameters.
type CommodityTariffConfig struct {
	Features TariffFeature
	// Clock is the clock the tariff's current day and day entry are read
	// from. Set, it wins over Now; neither set is [clock.System].
	Clock clock.Clock
	// Now is the clock the tariff's current day and day entry are read
	// from.
	//
	// Deprecated: set Clock; Now may be removed in v0.10.0.
	Now func() time.Time
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// CommodityTariffServer implements [contract.ClusterServer] for
// CommodityTariff. Reads, the data version and the change notifications
// are the generated server's; the update validation, the derived
// attributes and the two commands are chip's.
type CommodityTariffServer struct {
	*spec.Instance

	srv *spec.Server
	now func() time.Time

	// mu guards active, assigned and pending.
	mu sync.Mutex
	// active is a tariff the current attributes are derived from —
	// mServerTariffAttrsCtx.mTariffProvider != nullptr (Server.cpp:896-904).
	active bool
	// pending is a validated update waiting for its StartDate
	// (DelayedTariffUpdateIsActive, Server.cpp:64-68).
	pending *[len(tariffAttrs)]any
	// assigned marks the fields the pending update carries.
	assigned [len(tariffAttrs)]bool
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*CommodityTariffServer)(nil)
	_ contract.ClusterDataVersion            = (*CommodityTariffServer)(nil)
	_ contract.ClusterAttributeLister        = (*CommodityTariffServer)(nil)
	_ contract.ClusterCommandLister          = (*CommodityTariffServer)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*CommodityTariffServer)(nil)
	_ contract.AttributeChangeNotifier       = (*CommodityTariffServer)(nil)
)

// NewCommodityTariff builds a CommodityTariff server. Every attribute
// starts null: chip's delegate holds no tariff until the first update
// (the data-management objects start without a value, DataMgmt.h:538,
// and the current attributes are reset, Server.cpp:575-586).
func NewCommodityTariff(cfg CommodityTariffConfig) (*CommodityTariffServer, error) {
	initial := map[uint32]any{}
	for _, id := range tariffAttrs {
		initial[id] = nil
	}
	for _, id := range []uint32{
		ct.AttrCurrentDay, ct.AttrNextDay, ct.AttrCurrentDayEntry, ct.AttrCurrentDayEntryDate,
		ct.AttrNextDayEntry, ct.AttrNextDayEntryDate, ct.AttrCurrentTariffComponents, ct.AttrNextTariffComponents,
	} {
		initial[id] = nil
	}
	if cfg.Features&ct.FeatureRandomization == 0 {
		delete(initial, ct.AttrDefaultRandomizationOffset)
		delete(initial, ct.AttrDefaultRandomizationType)
	}
	s := &CommodityTariffServer{now: cfg.Now}
	handlers := map[uint32]spec.CommandHandler{
		ct.CmdGetTariffComponent: s.getTariffComponent,
		ct.CmdGetDayEntry:        s.getDayEntry,
	}
	srv, err := spec.NewServer(ct.Definition, spec.Options{Features: uint32(cfg.Features)},
		spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion, Initial: initial})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	s.Instance, s.srv = srv.Instance, srv
	switch {
	case cfg.Clock != nil:
		s.now = cfg.Clock.Now
	case s.now == nil:
		s.now = clock.System{}.Now
	}
	return s, nil
}

func (s *CommodityTariffServer) nowEpoch() uint32 { return matterEpochS(s.now()) }

// value reads a stored attribute.
func (s *CommodityTariffServer) value(attrID uint32) any {
	v, _ := s.srv.Value(attrID)
	return v
}

// --- the update -------------------------------------------------------------

// tariffUpdateCtx is chip's TariffUpdateCtx (commodity-tariff-server.h):
// what one attribute's validation leaves for the next and for the cross
// validation.
type tariffUpdateCtx struct {
	features    TariffFeature
	blockMode   ct.BlockModeEnum
	tariffStart *uint32
	updateTS    uint32

	dayEntryRefsFromDays    map[uint32]bool
	dayEntryRefsFromPeriods map[uint32]bool
	componentRefsByPeriods  map[uint32]bool
	patternRefsFromCalendar map[uint32]bool
	// componentFeatures maps each component id to the features its
	// fields use (TariffComponentKeyIDsFeatureMap).
	componentFeatures map[uint32]TariffFeature
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTariffInvalid, fmt.Sprintf(format, args...))
}

// addIDs is HasDuplicateIDs (DataMgmt.cpp:76-88): every id joins seen; one
// already there is a duplicate.
func addIDs(ids []uint32, seen map[uint32]bool, what string) error {
	for _, id := range ids {
		if seen[id] {
			return fmt.Errorf("%w: %s %d", ErrTariffDuplicateID, what, id)
		}
		seen[id] = true
	}
	return nil
}

// UpdateTariff is Delegate::TariffDataUpdate (Server.cpp:43-71) at the
// server's clock: every attribute the update carries is validated in
// chip's order (TariffDataUpd_Init, :73-82), then the update as a whole
// (TariffDataUpd_CrossValidator, :138-288). A refused update changes
// nothing and returns why. An accepted one whose StartDate lies ahead
// waits for it ([CommodityTariffServer.SyncTime] activates it,
// TryToActivateDelayedTariff, :316-328); any other is committed now
// (TariffDataUpd_Finish, :290-314): the changed attributes are reported
// and the current day, day entry and tariff components derived again
// (Instance::TariffDataUpdatedCb, :588-610).
func (s *CommodityTariffServer) UpdateTariff(t Tariff) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil {
		return ErrTariffUpdatePending
	}
	now := s.nowEpoch()
	ctx := &tariffUpdateCtx{
		features: TariffFeature(s.FeatureMap()), blockMode: ct.BlockModeNoBlock,
		tariffStart: t.StartDate, updateTS: now,
		dayEntryRefsFromDays: map[uint32]bool{}, dayEntryRefsFromPeriods: map[uint32]bool{},
		componentRefsByPeriods: map[uint32]bool{}, patternRefsFromCalendar: map[uint32]bool{},
		componentFeatures: map[uint32]TariffFeature{},
	}
	if err := validateTariff(&t, ctx); err != nil {
		return err
	}
	if err := crossValidateTariff(&t, ctx); err != nil {
		return err
	}
	values := t.values()
	var assigned [len(tariffAttrs)]bool
	for i, v := range values {
		assigned[i] = v != nil
	}
	if t.StartDate != nil && *t.StartDate > now { // :64-68
		s.pending, s.assigned = &values, assigned
		return nil
	}
	return s.finishLocked(values, assigned)
}

// finishLocked is TariffDataUpd_Finish(true) (Server.cpp:290-314): every
// carried attribute whose value changed is committed and reported, and a
// change runs TariffDataUpdatedCb.
func (s *CommodityTariffServer) finishLocked(values [len(tariffAttrs)]any, assigned [len(tariffAttrs)]bool) error {
	changed := map[uint32]any{}
	for i, id := range tariffAttrs {
		if !assigned[i] || !s.Serves(id) {
			continue
		}
		if !reflect.DeepEqual(s.value(id), values[i]) { // UpdateFinish's HasChanged, DataMgmt.h:820
			changed[id] = values[i]
		}
	}
	if len(changed) == 0 {
		return nil
	}
	if err := s.srv.SetAttributes(changed); err != nil {
		return err
	}
	return s.dataUpdatedLocked(false)
}

// dataUpdatedLocked is Instance::TariffDataUpdatedCb (Server.cpp:588-610)
// after the attributes are reported: the derived attributes are reset
// (DeinitCurrentAttrs, :1093-1097); unless the update erased the tariff
// and while TariffUnit, StartDate, TariffInfo, DayEntries, TariffPeriods
// and TariffComponents all carry a value, they are derived again
// (InitCurrentAttrs, :906-914, whose failure chip only logs).
func (s *CommodityTariffServer) dataUpdatedLocked(erased bool) error {
	s.active = false
	if err := s.srv.SetAttributes(map[uint32]any{ // ResetCurrentAttributes, :575-586
		ct.AttrCurrentDay: nil, ct.AttrNextDay: nil, ct.AttrCurrentDayEntry: nil, ct.AttrNextDayEntry: nil,
		ct.AttrCurrentDayEntryDate: nil, ct.AttrNextDayEntryDate: nil,
		ct.AttrCurrentTariffComponents: nil, ct.AttrNextTariffComponents: nil,
	}); err != nil {
		return err
	}
	for _, id := range []uint32{ct.AttrTariffUnit, ct.AttrStartDate, ct.AttrTariffInfo, ct.AttrDayEntries, ct.AttrTariffPeriods, ct.AttrTariffComponents} {
		if s.value(id) == nil { // :599-604
			return nil
		}
	}
	if erased {
		return nil
	}
	s.active = true
	_ = s.updateCurrentLocked(s.nowEpoch()) // InitCurrentAttrs logs a failure, :909-913
	return nil
}

// ClearTariff is Delegate::CleanupTariffData (Server.cpp:330-357): every
// tariff attribute becomes null, the changed ones are reported, and the
// derived attributes are reset. A pending delayed update is dropped with
// it.
func (s *CommodityTariffServer) ClearTariff() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = nil
	changed := map[uint32]any{}
	for _, id := range tariffAttrs {
		if s.Serves(id) && s.value(id) != nil {
			changed[id] = nil
		}
	}
	if len(changed) == 0 {
		return nil
	}
	if err := s.srv.SetAttributes(changed); err != nil {
		return err
	}
	return s.dataUpdatedLocked(true)
}

// SyncTime is the app's poll (CommodityTariffInstance::TariffTimeUpdCb,
// examples/energy-gateway-app/commodity-tariff/src/
// CommodityTariffInstance.cpp:66-77, every kTimerPollIntervalInSec = 30 s,
// CommodityTariffInstance.h:27): a delayed update whose StartDate has
// come is committed (TryToActivateDelayedTariff, Server.cpp:316-328), then
// the current day, day entry and tariff components are derived again at
// the server's clock (TariffTimeAttrsSync, :916-923). It returns what
// chip logs; no tariff is not an error.
func (s *CommodityTariffServer) SyncTime() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.nowEpoch()
	if s.pending != nil {
		start, _ := s.pending[1].(uint32)
		if now >= start {
			values, assigned := *s.pending, s.assigned
			s.pending = nil
			if err := s.finishLocked(values, assigned); err != nil {
				return err
			}
		}
	}
	if !s.active { // UpdateCurrentAttrs answers NOT_FOUND, which :919 ignores
		return nil
	}
	return s.updateCurrentLocked(now)
}

// validateTariff is the per-attribute ValidateNewValue in chip's order
// (DataMgmt.cpp:611-1012), each for a field the update carries.
func validateTariff(t *Tariff, ctx *tariffUpdateCtx) error { //nolint:gocyclo,cyclop,funlen // one rule after the other, as chip states them
	if (t.DayEntries != nil && len(t.DayEntries) == 0) || (t.DayPatterns != nil && len(t.DayPatterns) == 0) ||
		(t.TariffComponents != nil && len(t.TariffComponents) == 0) || (t.TariffPeriods != nil && len(t.TariffPeriods) == 0) ||
		(t.IndividualDays != nil && len(t.IndividualDays) == 0) || (t.CalendarPeriods != nil && len(t.CalendarPeriods) == 0) {
		return fmt.Errorf("%w: an empty list", ErrTariffListLength) // CreateNewListValue, DataMgmt.h:582-584
	}
	if t.TariffUnit != nil && *t.TariffUnit > ct.TariffUnitKVAh { // :611-617
		return invalid("TariffUnit %d", *t.TariffUnit)
	}
	if t.StartDate != nil && *t.StartDate != 0 && ctx.updateTS > *t.StartDate { // :633-648
		return invalid("StartDate %d before the update at %d", *t.StartDate, ctx.updateTS)
	}
	if t.DefaultRandomizationType != nil && *t.DefaultRandomizationType > ct.DayEntryRandomizationTypeRandomNegative { // :650-656
		return invalid("DefaultRandomizationType %d", *t.DefaultRandomizationType)
	}
	if t.TariffInfo != nil {
		if err := validateTariffInfo(t.TariffInfo, ctx); err != nil {
			return err
		}
	}
	if t.DayEntries != nil {
		if err := validateDayEntries(t.DayEntries, ctx); err != nil {
			return err
		}
	}
	if t.DayPatterns != nil {
		if err := validateDayPatterns(t.DayPatterns, ctx); err != nil {
			return err
		}
	}
	if t.TariffComponents != nil {
		if len(t.TariffComponents) > tariffMaxListLength { // :822-826
			return fmt.Errorf("%w: TariffComponents", ErrTariffListLength)
		}
		for _, c := range t.TariffComponents {
			if err := validateTariffComponent(c, ctx); err != nil {
				return err
			}
		}
	}
	if t.TariffPeriods != nil {
		if len(t.TariffPeriods) > tariffMaxListLength { // :853-857
			return fmt.Errorf("%w: TariffPeriods", ErrTariffListLength)
		}
		for _, p := range t.TariffPeriods {
			if err := validateTariffPeriod(p, ctx); err != nil {
				return err
			}
		}
	}
	if t.IndividualDays != nil {
		if err := validateIndividualDays(t.IndividualDays, ctx); err != nil {
			return err
		}
	}
	if t.CalendarPeriods != nil {
		if err := validateCalendarPeriods(t.CalendarPeriods, ctx); err != nil {
			return err
		}
	}
	return nil
}

// validateTariffInfo is DataMgmt.cpp:658-711: the label and the provider
// name at most 128 characters; a non-null BlockMode in the enum, and the
// mode the components' thresholds are held to (NoBlock when null); with
// PRICE the Currency present and, when not null, below 999; without
// PRICE no Currency.
func validateTariffInfo(info *TariffInformation, ctx *tariffUpdateCtx) error {
	if !info.TariffLabel.Null && len(info.TariffLabel.Value) > tariffMaxString { // :670-671
		return invalid("TariffLabel too long")
	}
	if !info.ProviderName.Null && len(info.ProviderName.Value) > tariffMaxString { // :673-674
		return invalid("ProviderName too long")
	}
	ctx.blockMode = ct.BlockModeNoBlock // :683-686
	if !info.BlockMode.Null {           // :677-682
		if info.BlockMode.Value > ct.BlockModeIndividual {
			return invalid("BlockMode %d", info.BlockMode.Value)
		}
		ctx.blockMode = info.BlockMode.Value
	}
	if ctx.features&ct.FeaturePricing != 0 { // :689-703
		if info.Currency == nil {
			return invalid("Currency missing with PRICE")
		}
		if !info.Currency.Null && info.Currency.Value.Currency >= tariffMaxCurrencyValue {
			return invalid("Currency %d", info.Currency.Value.Currency)
		}
	} else if info.Currency != nil { // :704-708
		return invalid("Currency without PRICE")
	}
	return nil
}

// validateDayEntries is DataMgmt.cpp:713-754 with ValidateListEntry for a
// DayEntryStruct (:421-444): 1 to 672 entries, ids unique, StartTime
// below 1500; with RNDM both randomization fields present and the type
// in the enum.
func validateDayEntries(entries []TariffDayEntry, ctx *tariffUpdateCtx) error {
	if len(entries) > tariffMaxListLength { // :729-733
		return fmt.Errorf("%w: DayEntries", ErrTariffListLength)
	}
	seen := map[uint32]bool{}
	for _, e := range entries {
		if seen[e.DayEntryId] { // :739-743
			return fmt.Errorf("%w: DayEntryID %d", ErrTariffDuplicateID, e.DayEntryId)
		}
		seen[e.DayEntryId] = true
		if e.StartTime >= tariffDayEntryDurationLimit { // :425-426
			return invalid("DayEntry %d StartTime %d", e.DayEntryId, e.StartTime)
		}
		if ctx.features&ct.FeatureRandomization != 0 { // :428-441
			if e.RandomizationOffset == nil || e.RandomizationType == nil {
				return invalid("DayEntry %d without its randomization", e.DayEntryId)
			}
			if *e.RandomizationType > ct.DayEntryRandomizationTypeRandomNegative {
				return invalid("DayEntry %d RandomizationType %d", e.DayEntryId, *e.RandomizationType)
			}
		}
	}
	return nil
}

// validateDayPatterns is DataMgmt.cpp:756-807 with ValidateListEntry for
// a DayPatternStruct (:403-419): 1 to 28 patterns, ids unique, 1 to 96
// DayEntryIDs each, no day entry in two patterns (the ids join the update's
// day references), and the days of week either none at all (a single
// rotating day) or the full week.
func validateDayPatterns(patterns []TariffDayPattern, ctx *tariffUpdateCtx) error {
	if len(patterns) > tariffMaxDayPatterns { // :772-776
		return fmt.Errorf("%w: DayPatterns", ErrTariffListLength)
	}
	seen := map[uint32]bool{}
	var mask ct.DayPatternDayOfWeekBitmap
	for _, p := range patterns {
		if seen[p.DayPatternId] { // :781-785
			return fmt.Errorf("%w: DayPatternID %d", ErrTariffDuplicateID, p.DayPatternId)
		}
		seen[p.DayPatternId] = true
		mask |= p.DaysOfWeek
		if len(p.DayEntryIds) == 0 || len(p.DayEntryIds) > tariffMaxDayEntryIDs { // :407-408
			return invalid("DayPattern %d with %d DayEntryIDs", p.DayPatternId, len(p.DayEntryIds))
		}
		if err := addIDs(p.DayEntryIds, ctx.dayEntryRefsFromDays, "DayEntryID in DayPatterns"); err != nil { // :412-416
			return err
		}
	}
	if mask != 0 && mask != tariffFullWeekMask { // :797-804
		return invalid("DayPatterns cover days of week 0x%02X", uint8(mask))
	}
	return nil
}

// validateTariffComponent is ValidateListEntry for a TariffComponentStruct
// (DataMgmt.cpp:446-557): an id above 0; a Threshold exactly when
// BlockMode is not NoBlock; a Label of at most 128; each feature field
// only with its feature, its enum in range and its amounts positive; the
// id unique, recorded with the features its fields use.
func validateTariffComponent(c TariffComponent, ctx *tariffUpdateCtx) error { //nolint:gocyclo,cyclop // one rule per field, as chip states them
	if c.TariffComponentId == 0 { // :452
		return invalid("TariffComponentID 0")
	}
	if (ctx.blockMode == ct.BlockModeNoBlock) == !c.Threshold.Null { // :454-457
		return invalid("TariffComponent %d Threshold against BlockMode %d", c.TariffComponentId, ctx.blockMode)
	}
	if c.Label != nil && !c.Label.Null && len(c.Label.Value) > tariffMaxString { // :460-464
		return invalid("TariffComponent %d Label too long", c.TariffComponentId)
	}
	var features TariffFeature
	need := func(f TariffFeature, field string) error {
		if ctx.features&f == 0 {
			return invalid("TariffComponent %d %s without its feature", c.TariffComponentId, field)
		}
		features |= f
		return nil
	}
	if c.Price != nil && !c.Price.Null { // :467-477
		if err := need(ct.FeaturePricing, "Price"); err != nil {
			return err
		}
		if c.Price.Value.PriceType > ct.TariffPriceTypeIncentiveSignal {
			return invalid("TariffComponent %d PriceType %d", c.TariffComponentId, c.Price.Value.PriceType)
		}
	}
	if c.FriendlyCredit != nil { // :480-487
		if err := need(ct.FeatureFriendlyCredit, "FriendlyCredit"); err != nil {
			return err
		}
	}
	if c.AuxiliaryLoad != nil { // :490-500
		if err := need(ct.FeatureAuxiliaryLoad, "AuxiliaryLoad"); err != nil {
			return err
		}
		if c.AuxiliaryLoad.RequiredState > ct.AuxiliaryLoadSettingNone {
			return invalid("TariffComponent %d RequiredState %d", c.TariffComponentId, c.AuxiliaryLoad.RequiredState)
		}
	}
	if c.PeakPeriod != nil { // :503-514
		if err := need(ct.FeaturePeakPeriod, "PeakPeriod"); err != nil {
			return err
		}
		if c.PeakPeriod.Severity > ct.PeakPeriodSeverityHigh || c.PeakPeriod.PeakPeriod == 0 {
			return invalid("TariffComponent %d PeakPeriod", c.TariffComponentId)
		}
	}
	if pt := c.PowerThreshold; pt != nil { // :517-541
		if err := need(ct.FeaturePowerThreshold, "PowerThreshold"); err != nil {
			return err
		}
		if (!pt.PowerThresholdSource.Null && pt.PowerThresholdSource.Value > ct.PowerThresholdSourceEquipment) ||
			(pt.PowerThreshold != nil && *pt.PowerThreshold <= 0) ||
			(pt.ApparentPowerThreshold != nil && *pt.ApparentPowerThreshold <= 0) {
			return invalid("TariffComponent %d PowerThreshold", c.TariffComponentId)
		}
	}
	if _, dup := ctx.componentFeatures[c.TariffComponentId]; dup { // :550-554
		return fmt.Errorf("%w: TariffComponentID %d", ErrTariffDuplicateID, c.TariffComponentId)
	}
	ctx.componentFeatures[c.TariffComponentId] = features
	return nil
}

// validateTariffPeriod is ValidateListEntry for a TariffPeriodStruct
// (DataMgmt.cpp:559-599): a non-null Label of 1 to 128 characters; 1 to 20
// DayEntryIDs, none in another period; 1 to 20 TariffComponentIDs, unique
// within the period.
func validateTariffPeriod(p TariffPeriod, ctx *tariffUpdateCtx) error {
	if !p.Label.Null && (len(p.Label.Value) > tariffMaxString || p.Label.Value == "") { // :564-576
		return invalid("TariffPeriod Label %q", p.Label.Value)
	}
	if len(p.DayEntryIds) == 0 || len(p.DayEntryIds) > tariffMaxPeriodItemIDs { // :578-579
		return invalid("TariffPeriod with %d DayEntryIDs", len(p.DayEntryIds))
	}
	if len(p.TariffComponentIds) == 0 || len(p.TariffComponentIds) > tariffMaxPeriodItemIDs { // :581-582
		return invalid("TariffPeriod with %d TariffComponentIDs", len(p.TariffComponentIds))
	}
	if err := addIDs(p.DayEntryIds, ctx.dayEntryRefsFromPeriods, "DayEntryID in TariffPeriods"); err != nil { // :585-589
		return err
	}
	if err := addIDs(p.TariffComponentIds, map[uint32]bool{}, "TariffComponentID in a TariffPeriod"); err != nil { // :591-594
		return err
	}
	for _, id := range p.TariffComponentIds { // :596
		ctx.componentRefsByPeriods[id] = true
	}
	return nil
}

// validateIndividualDays is DataMgmt.cpp:873-926: 1 to 50 days, dates
// strictly increasing (and so above 0), DayType in the enum, 1 to 96
// DayEntryIDs each, none already referenced by a day pattern or another
// day.
func validateIndividualDays(days []TariffDay, ctx *tariffUpdateCtx) error {
	if len(days) > tariffMaxIndividualDays { // :887-891
		return fmt.Errorf("%w: IndividualDays", ErrTariffListLength)
	}
	var previous uint32
	for _, d := range days {
		if d.Date <= previous { // :897-901
			return invalid("IndividualDays date %d not after %d", d.Date, previous)
		}
		if d.DayType > ct.DayTypeEvent { // :904-907
			return invalid("IndividualDays DayType %d", d.DayType)
		}
		if len(d.DayEntryIds) == 0 || len(d.DayEntryIds) > tariffMaxDayEntryIDs { // :910-913
			return invalid("IndividualDays day with %d DayEntryIDs", len(d.DayEntryIds))
		}
		if err := addIDs(d.DayEntryIds, ctx.dayEntryRefsFromDays, "DayEntryID in IndividualDays"); err != nil { // :916-920
			return err
		}
		previous = d.Date
	}
	return nil
}

// validateCalendarPeriods is DataMgmt.cpp:928-1013: the update carries a
// StartDate; 1 to 7 DayPatternIDs per period, none in another period; the
// first period's StartDate unset (null or 0) exactly when the tariff's
// StartDate is 0; every later one set and after the one before.
func validateCalendarPeriods(periods []TariffCalendarPeriod, ctx *tariffUpdateCtx) error {
	if ctx.tariffStart == nil { // :947-951
		return invalid("CalendarPeriods without the tariff's StartDate")
	}
	start := *ctx.tariffStart
	var previous spec.Nullable[uint32]
	for i, p := range periods {
		if len(p.DayPatternIds) == 0 || len(p.DayPatternIds) > tariffMaxCalendarPatternIDs { // :956-961
			return invalid("CalendarPeriod with %d DayPatternIDs", len(p.DayPatternIds))
		}
		if err := addIDs(p.DayPatternIds, ctx.patternRefsFromCalendar, "DayPatternID in CalendarPeriods"); err != nil { // :964-968
			return err
		}
		set := !p.StartDate.Null && p.StartDate.Value > 0
		if i == 0 { // :971-993
			if start == 0 && set {
				return invalid("the first CalendarPeriod has a StartDate while the tariff's is 0")
			}
			if start > 0 && !set {
				return invalid("the first CalendarPeriod has no StartDate while the tariff's is %d", start)
			}
			previous = p.StartDate
			continue
		}
		if p.StartDate.Null { // :996-1000
			return invalid("CalendarPeriod %d without StartDate", i)
		}
		if !previous.Null && p.StartDate.Value <= previous.Value { // :1003-1007
			return invalid("CalendarPeriod %d StartDate not after the one before", i)
		}
		previous = p.StartDate
	}
	return nil
}

// crossValidateTariff is TariffDataUpd_CrossValidator (Server.cpp:138-288).
func crossValidateTariff(t *Tariff, ctx *tariffUpdateCtx) error { //nolint:gocyclo,cyclop // chip's checks in chip's order
	if t.TariffInfo == nil || t.DayEntries == nil || t.TariffComponents == nil || t.TariffPeriods == nil { // :143-146
		return ErrTariffMissing
	}
	dayEntries := map[uint32]*TariffDayEntry{}
	for i := range t.DayEntries {
		dayEntries[t.DayEntries[i].DayEntryId] = &t.DayEntries[i]
	}
	components := map[uint32]*TariffComponent{}
	for i := range t.TariffComponents {
		components[t.TariffComponents[i].TariffComponentId] = &t.TariffComponents[i]
	}
	patterns := map[uint32]bool{}
	for _, p := range t.DayPatterns {
		patterns[p.DayPatternId] = true
	}
	known := func(refs map[uint32]bool, has func(uint32) bool, what string) error { // :168-174
		for id := range refs {
			if !has(id) {
				return fmt.Errorf("%w: %s %d", ErrTariffUnknownID, what, id)
			}
		}
		return nil
	}
	hasEntry := func(id uint32) bool { return dayEntries[id] != nil }
	if err := known(ctx.dayEntryRefsFromPeriods, hasEntry, "DayEntryID"); err != nil { // :177
		return err
	}
	if err := known(ctx.componentRefsByPeriods, func(id uint32) bool { return components[id] != nil }, "TariffComponentID"); err != nil { // :178
		return err
	}
	if t.DayPatterns != nil || t.IndividualDays != nil { // :181-192
		if err := known(ctx.dayEntryRefsFromDays, hasEntry, "DayEntryID"); err != nil {
			return err
		}
	}
	if t.CalendarPeriods != nil { // :195-200
		if err := known(ctx.patternRefsFromCalendar, func(id uint32) bool { return patterns[id] }, "DayPatternID"); err != nil {
			return err
		}
	}
	if t.IndividualDays == nil && t.CalendarPeriods == nil { // :202
		return ErrTariffNoDays
	}
	type startDuration struct{ start, duration uint16 }
	type threshold struct {
		value    int64
		features TariffFeature
	}
	for _, p := range t.TariffPeriods { // :232-285
		seen := map[startDuration]bool{}
		for _, id := range p.DayEntryIds {
			e := dayEntries[id]
			pair := startDuration{e.StartTime, tariffDayEntryDurationLimit} // :250-253
			if e.Duration != nil {
				pair.duration = *e.Duration
			}
			if seen[pair] { // :255
				return fmt.Errorf("%w: two day entries of a period at %d for %d", ErrTariffDuplicateID, pair.start, pair.duration)
			}
			seen[pair] = true
		}
		var thresholds []threshold // ThresholdsPerFeatureValidationContext, :84-123
		for _, id := range p.TariffComponentIds {
			c := components[id]
			features := ctx.componentFeatures[id]
			if c.Threshold.Null || (c.Predicted != nil && *c.Predicted) || features == 0 { // :277-280
				continue
			}
			for _, th := range thresholds { // haveJointFeatures, :96-104
				if th.value == c.Threshold.Value && th.features&features != 0 {
					return fmt.Errorf("%w: two components of a period share threshold %d and a feature", ErrTariffDuplicateID, th.value)
				}
			}
			thresholds = append(thresholds, threshold{c.Threshold.Value, features})
		}
	}
	return nil
}

// --- the derived attributes ---------------------------------------------------

// updateCurrentLocked is Instance::UpdateCurrentAttrs (Server.cpp:925-949).
func (s *CommodityTariffServer) updateCurrentLocked(now uint32) error {
	if err := s.updateDayLocked(now); err != nil {
		return err
	}
	return s.updateDayEntryLocked(now)
}

func (s *CommodityTariffServer) dayEntries() []TariffDayEntry {
	l, _ := s.value(ct.AttrDayEntries).([]TariffDayEntry)
	return l
}

func (s *CommodityTariffServer) tariffComponents() []TariffComponent {
	l, _ := s.value(ct.AttrTariffComponents).([]TariffComponent)
	return l
}

func (s *CommodityTariffServer) tariffPeriods() []TariffPeriod {
	l, _ := s.value(ct.AttrTariffPeriods).([]TariffPeriod)
	return l
}

func (s *CommodityTariffServer) dayEntryByID(id uint32) *TariffDayEntry {
	for _, e := range s.dayEntries() {
		if e.DayEntryId == id {
			return &e
		}
	}
	return nil
}

func (s *CommodityTariffServer) componentByID(id uint32) *TariffComponent {
	for _, c := range s.tariffComponents() {
		if c.TariffComponentId == id {
			return &c
		}
	}
	return nil
}

// dayOfWeek is GetDayOfWeek (Server.cpp:658-667): the UTC weekday of a
// Matter epoch-s time as a DayPatternDayOfWeekBitmap bit.
func dayOfWeek(epochS uint32) ct.DayPatternDayOfWeekBitmap {
	wd := time.Unix(int64(epochS)+matterEpochOffset, 0).UTC().Weekday()
	return ct.DayPatternDayOfWeekBitmap(1) << uint(wd)
}

// findDay is FindDay (Server.cpp:669-730): the individual day whose date
// falls on the day of ts; otherwise the calendar period starting that day
// — or, while the tariff's StartDate is 0, the first period when its own
// StartDate is unset — and in it the first day pattern covering the
// weekday, as a Standard day. ok is false when neither yields a day
// (DayIsValid, :732-739).
func (s *CommodityTariffServer) findDay(ts uint32) (TariffDay, bool) {
	dayStart := ts - ts%tariffSecondsPerDay
	if days, _ := s.value(ct.AttrIndividualDays).([]TariffDay); days != nil { // :677-686
		for _, d := range days {
			if d.Date >= dayStart && d.Date < dayStart+tariffSecondsPerDay {
				return d, d.Date != 0
			}
		}
	}
	calendar, _ := s.value(ct.AttrCalendarPeriods).([]TariffCalendarPeriod)
	if calendar == nil {
		return TariffDay{}, false
	}
	start, _ := s.value(ct.AttrStartDate).(uint32)
	var period *TariffCalendarPeriod
	first := true
	for i := range calendar { // :693-710
		entry := &calendar[i]
		if first && start == 0 && (entry.StartDate.Null || entry.StartDate.Value == 0) {
			period, first = entry, false
			continue
		}
		if entry.StartDate.Null {
			continue
		}
		if entry.StartDate.Value >= dayStart && entry.StartDate.Value < dayStart+tariffSecondsPerDay {
			period = entry
			break
		}
	}
	if period == nil {
		return TariffDay{}, false
	}
	patterns, _ := s.value(ct.AttrDayPatterns).([]TariffDayPattern)
	weekday := dayOfWeek(ts)
	for _, id := range period.DayPatternIds { // :714-725
		for _, p := range patterns {
			if p.DayPatternId == id && p.DaysOfWeek&weekday != 0 {
				return TariffDay{Date: dayStart, DayType: ct.DayTypeStandard, DayEntryIds: slices.Clone(p.DayEntryIds)}, dayStart != 0
			}
		}
	}
	return TariffDay{}, false
}

// updateDayLocked is Instance::UpdateDayInformation (Server.cpp:951-989):
// CurrentDay is the day of now — no such day is an error and changes
// nothing; it is replaced when its date differs. NextDay is the day just
// after midnight, replaced when it exists and its date differs.
func (s *CommodityTariffServer) updateDayLocked(now uint32) error {
	current, ok := s.findDay(now)
	if !ok { // :959-963
		return fmt.Errorf("energy: CommodityTariff: no current day at %d", now)
	}
	differs := func(day TariffDay, attr uint32) bool {
		old, isDay := s.value(attr).(TariffDay)
		return !isDay || old.Date != day.Date
	}
	if differs(current, ct.AttrCurrentDay) { // :973-977
		if err := s.srv.Set(ct.AttrCurrentDay, current); err != nil {
			return err
		}
	}
	next, ok := s.findDay(now + (tariffSecondsPerDay - now%tariffSecondsPerDay) + 1) // :979-980
	if ok && differs(next, ct.AttrNextDay) {                                         // :982-986
		return s.srv.Set(ct.AttrNextDay, next)
	}
	return nil
}

// findDayEntry is FindDayEntry (Server.cpp:741-798): walking the day's
// entries in order, an entry's duration is its own, or — without one —
// up to the next entry's start (wrapping midnight), or to the end of the
// day; the first entry covering minutes is current and the entry after it
// next, with the minutes it has left. No covering entry leaves the last
// one walked as current and no next, as chip's loop does.
func (s *CommodityTariffServer) findDayEntry(ids []uint32, minutes uint16) (current, next *TariffDayEntry, remain uint16) {
	for i, id := range ids {
		next = nil
		current = s.dayEntryByID(id)
		if current == nil { // :753-756
			continue
		}
		if i+1 < len(ids) {
			next = s.dayEntryByID(ids[i+1])
		}
		duration := tariffDayEntryDurationLimit - current.StartTime // :769
		switch {
		case current.Duration != nil:
			duration = *current.Duration
		case next != nil && next.StartTime < tariffDayEntryDurationLimit:
			if next.StartTime <= current.StartTime { // :777-781
				duration = tariffDayEntryDurationLimit - current.StartTime + next.StartTime
			} else {
				duration = next.StartTime - current.StartTime
			}
		}
		if current.StartTime <= minutes && current.StartTime+duration > minutes { // :790-794
			return current, next, duration - (minutes - current.StartTime)
		}
	}
	return current, next, 0
}

// updateDayEntryLocked is Instance::UpdateDayEntryInformation
// (Server.cpp:991-1091).
func (s *CommodityTariffServer) updateDayEntryLocked(now uint32) error {
	day, ok := s.value(ct.AttrCurrentDay).(TariffDay)
	if !ok { // :993-996
		return errors.New("energy: CommodityTariff: no current day")
	}
	minutes := uint16((now % tariffSecondsPerDay) / 60) //nolint:gosec // below 1440
	current, next, remain := s.findDayEntry(day.DayEntryIds, minutes)

	var entry, date any // null unless found, :1005-1039
	if current != nil {
		entry, date = *current, day.Date+uint32(current.StartTime)*60
	}
	if !reflect.DeepEqual(s.value(ct.AttrCurrentDayEntry), entry) || !reflect.DeepEqual(s.value(ct.AttrCurrentDayEntryDate), date) { // :1041
		if current != nil {
			if err := s.setPeriodComponentsLocked(current.DayEntryId, ct.AttrCurrentTariffComponents); err != nil {
				return err
			}
		}
		if err := s.srv.SetAttributes(map[uint32]any{ct.AttrCurrentDayEntry: entry, ct.AttrCurrentDayEntryDate: date}); err != nil {
			return err
		}
	}
	entry, date = nil, nil // :1058-1071
	if next != nil {
		entry = *next
		if d, ok := s.value(ct.AttrCurrentDayEntryDate).(uint32); ok {
			date = d + uint32(remain)*60
		}
	}
	if !reflect.DeepEqual(s.value(ct.AttrNextDayEntry), entry) || !reflect.DeepEqual(s.value(ct.AttrNextDayEntryDate), date) { // :1073
		if next != nil {
			if err := s.setPeriodComponentsLocked(next.DayEntryId, ct.AttrNextTariffComponents); err != nil {
				return err
			}
		}
		return s.srv.SetAttributes(map[uint32]any{ct.AttrNextDayEntry: entry, ct.AttrNextDayEntryDate: date})
	}
	return nil
}

// setPeriodComponentsLocked is UpdateTariffComponentAttrsDayEntryById
// (Server.cpp:835-891): the components of the first tariff period
// referencing the day entry — 1 to 20, each one the tariff carries — go
// to attr.
func (s *CommodityTariffServer) setPeriodComponentsLocked(dayEntryID, attr uint32) error {
	var period *TariffPeriod
	for _, p := range s.tariffPeriods() { // FindTariffPeriodByDayEntryId, :800-814
		if slices.Contains(p.DayEntryIds, dayEntryID) {
			period = &p
			break
		}
	}
	if period == nil { // :844-847
		return fmt.Errorf("%w: no tariff period references day entry %d", ErrTariffUnknownID, dayEntryID)
	}
	if len(period.TariffComponentIds) == 0 || len(period.TariffComponentIds) > tariffMaxPeriodItemIDs { // :852
		return fmt.Errorf("%w: period components", ErrTariffListLength)
	}
	out := make([]TariffComponent, 0, len(period.TariffComponentIds))
	for _, id := range period.TariffComponentIds {
		c := s.componentByID(id)
		if c == nil { // :862
			return fmt.Errorf("%w: TariffComponentID %d", ErrTariffUnknownID, id)
		}
		out = append(out, *c)
	}
	return s.srv.Set(attr, out)
}

// --- commands -------------------------------------------------------------

// getTariffComponent is HandleGetTariffComponent (Server.cpp:1099-1210):
// FAILURE without an active tariff, NOT_FOUND for an unknown component;
// otherwise the component, the labels of the periods referencing it
// joined with "; " up to 128 characters (null when none), and their day
// entry ids, sorted.
func (s *CommodityTariffServer) getTariffComponent(_ context.Context, fields any) (any, error) {
	var id uint32
	switch r := fields.(type) {
	case ct.GetTariffComponentRequest:
		id = r.TariffComponentId
	case *ct.GetTariffComponentRequest:
		if r == nil {
			return nil, statusError{im.StatusInvalidCommand, "energy: GetTariffComponent without fields"}
		}
		id = r.TariffComponentId
	default:
		return nil, statusError{im.StatusInvalidCommand, fmt.Sprintf("energy: GetTariffComponent fields %T", fields)}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active { // :1104-1107
		return nil, statusError{im.StatusFailure, "energy: GetTariffComponent: the tariff is not available"}
	}
	component := s.componentByID(id)
	if component == nil { // :1110-1114
		return nil, statusError{im.StatusNotFound, fmt.Sprintf("energy: GetTariffComponent: no component %d", id)}
	}
	var label []byte
	ids := []uint32{}
	first := true
	for _, p := range s.tariffPeriods() { // FindTariffPeriodsByTariffComponentId, :816-833; :1128-1181
		if !slices.Contains(p.TariffComponentIds, id) {
			continue
		}
		if !p.Label.Null {
			if !first {
				if len(label)+2 >= tariffDefaultStringValuesMaxBuf { // :1138-1147
					break
				}
				label = append(label, ';', ' ')
			}
			n := min(len(p.Label.Value), tariffDefaultStringValuesMaxBuf-len(label)) // :1151-1157
			label = append(label, p.Label.Value[:n]...)
			first = false
			if len(label) >= tariffDefaultStringValuesMaxBuf { // :1161-1165
				break
			}
		}
		for _, de := range p.DayEntryIds { // :1168-1179
			if len(ids) >= tariffMaxListLength {
				break
			}
			ids = append(ids, de)
		}
	}
	slices.Sort(ids) // :1183
	resp := ct.GetTariffComponentResponse{Label: spec.NullOf[string](), DayEntryIds: ids, TariffComponent: *component}
	if len(label) > 0 { // :1188-1191
		resp.Label = spec.ValueOf(string(label))
	}
	return resp, nil
}

// getDayEntry is HandleGetDayEntry (Server.cpp:1212-1241): FAILURE
// without an active tariff, NOT_FOUND for an unknown day entry.
func (s *CommodityTariffServer) getDayEntry(_ context.Context, fields any) (any, error) {
	var id uint32
	switch r := fields.(type) {
	case ct.GetDayEntryRequest:
		id = r.DayEntryId
	case *ct.GetDayEntryRequest:
		if r == nil {
			return nil, statusError{im.StatusInvalidCommand, "energy: GetDayEntry without fields"}
		}
		id = r.DayEntryId
	default:
		return nil, statusError{im.StatusInvalidCommand, fmt.Sprintf("energy: GetDayEntry fields %T", fields)}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active { // :1217-1220
		return nil, statusError{im.StatusFailure, "energy: GetDayEntry: the tariff is not available"}
	}
	entry := s.dayEntryByID(id)
	if entry == nil { // :1223-1232
		return nil, statusError{im.StatusNotFound, fmt.Sprintf("energy: GetDayEntry: no day entry %d", id)}
	}
	return ct.GetDayEntryResponse{DayEntry: *entry}, nil
}

// --- plumbing -------------------------------------------------------------

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *CommodityTariffServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (s *CommodityTariffServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute; a list as its generated list
// (commodityWireValue).
func (s *CommodityTariffServer) MatterRead(attrID uint32) (any, bool) {
	return commodityRead(s.srv.MatterRead(attrID))
}

// MatterWrite answers every write: the cluster has no writable attribute.
func (s *CommodityTariffServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches GetTariffComponent and GetDayEntry.
func (s *CommodityTariffServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}
