// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/SukramJ/go-fabric/internal/deepcopy"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The Presets feature of the Thermostat server: the port of matter.js
// packages/node/src/behaviors/thermostat/ThermostatServer.ts at db021da0 —
// the seed and the active-handle check of initialize (:134-152),
// #validatePresetWriteRequest (:1367-1493), #normalizePresets (:1516-1537)
// and #validatePresetCommit (:1544-1580), handleSetActivePresetRequest /
// setActivePresetRequest (:281-316) and the Presets accessor over
// persistedPresets (:1660-1703).

// Preset is the PresetStruct of the generated definition.
type Preset = tdef.PresetStruct

// PresetType is the PresetTypeStruct of the generated definition.
type PresetType = tdef.PresetTypeStruct

// ErrPresets reports a ThermostatConfig whose presets the server refuses:
// a NumberOfPresets or PresetTypes outside their constraints, configured
// presets the write rules refuse, or a [PresetStore] that fails to load —
// with TSUGGEST also a [SuggestionStore] that fails to load or to save the
// stored list pruned at the start.
var ErrPresets = errors.New("thermo: preset configuration refused")

// presetHandleLength is the length of a handle the server issues —
// matter.js `entropy.randomBytes(16)` (ThermostatServer.ts:1526).
const presetHandleLength = 16

// PresetStore persists one thermostat endpoint's presets — matter.js's
// PersistedPresets, quality N (ThermostatServer.ts:64-74), and the
// ActivePresetHandle, "X N" in thermostat-cluster.element.ts. The presets travel as the TLV encoding of the
// Presets list; the active handle is nil for null. Load reports ok=false
// when nothing was saved, and the server then seeds from
// [ThermostatConfig.Presets]. store.ThermostatPresetStore implements it on
// the module's store.
//
// SavePresets runs while the server holds the presets' save in turn: every
// change of Presets or ActivePresetHandle — a write, a command, the
// host's direct setpoint change that ends the active preset — waits for
// it, so each save writes the pair current when it runs
// (R2-P6-store-1501). SavePresets must therefore not call back into the
// server's setters, which would wait for itself, and should return in
// bounded time: the wait takes no context.
type PresetStore = tdef.PresetStore

// EncodePresets is the TLV encoding of a Presets list a [PresetStore]
// keeps.
func EncodePresets(presets []Preset) []byte {
	enc := tlv.NewEncoder()
	spec.List[Preset](presets).EncodeTLV(enc, tlv.AnonymousTag())
	b, _ := enc.Bytes() // an encoder of a well-formed list does not fail
	return b
}

// DecodePresets decodes what [EncodePresets] encoded.
func DecodePresets(b []byte) ([]Preset, error) {
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, fmt.Errorf("thermo: decode presets: %w", err)
	}
	v, err := tdef.Definition.Attribute(tdef.AttrPresets).Decode(n)
	if err != nil {
		return nil, fmt.Errorf("thermo: decode presets: %w", err)
	}
	list, _ := v.(spec.List[Preset])
	return []Preset(list), nil
}

// asStatus is the IM status an error carries, if any ([spec.CarriesStatus]:
// a host's contract.StatusError or an im.StatusCodeError).
func asStatus(err error) (im.StatusCode, bool) {
	if spec.CarriesStatus(err) {
		return spec.StatusOf(err), true
	}
	return 0, false
}

// presetsValue is a Presets value as the server handles it: the bridge
// decodes a write as spec.List, a host may pass a plain slice.
func presetsValue(v any) ([]Preset, error) {
	switch x := v.(type) {
	case spec.List[Preset]:
		return []Preset(x), nil
	case []Preset:
		return x, nil
	}
	return nil, spec.Errorf(im.StatusConstraintError, "thermo: Presets value %T is not a list of PresetStruct", v)
}

// handleKey is a handle's map key (matter.js Bytes.toHex).
func handleKey(h []byte) string { return hex.EncodeToString(h) }

// presetName is a preset's name, nil for "no name": absent and null are
// one value (ThermostatServer.ts:1396-1398).
func presetName(p Preset) *string {
	if p.Name == nil || p.Name.Null {
		return nil
	}
	return &p.Name.Value
}

// presetScenarioName is the PresetScenarioEnum name of a scenario, for
// the messages.
func presetScenarioName(sc tdef.PresetScenarioEnum) string {
	values := tdef.PresetScenarioEnumDef.Values
	for i := range values {
		if values[i].Value == uint64(sc) {
			return values[i].Name
		}
	}
	return fmt.Sprintf("%d", sc)
}

// presetRules is what the preset validation reads besides the presets:
// the feature selection, the effective setpoint limits, the preset types
// and the count.
type presetRules struct {
	heat, cool       bool
	heatMin, heatMax int
	coolMin, coolMax int
	numberOfPresets  int
	types            []PresetType
}

// presetRules reads the rules from the server's state; the caller holds
// s.mu.
func (s *ThermostatServer) presetRules() presetRules {
	st := s.setpointState()
	r := presetRules{heat: st.heat, cool: st.cool, numberOfPresets: int(s.numberOfPresets), types: s.presetTypes}
	if st.heat {
		r.heatMin, r.heatMax = st.effectiveLimits(true)
	}
	if st.cool {
		r.coolMin, r.coolMax = st.effectiveLimits(false)
	}
	return r
}

// validatePresetWrite is matter.js #validatePresetWriteRequest
// (ThermostatServer.ts:1850-1975 at b25ed960): presets a client wants to
// store, against the presets they replace (seeding — old undefined —
// admits the handles the application states). issued are the handles this
// write's normalisation issued. Each appended preset is checked in order:
// its handle, scenario and name support, then the total and per-scenario
// counts, then the duplicate name (:1913-1943 at b25ed960), so an unknown
// handle in an over-long list is NOT_FOUND and one more unnamed preset for
// a full scenario RESOURCE_EXHAUSTED.
func (r presetRules) validatePresetWrite(presets, old []Preset, seeding bool, issued map[string]bool) error {
	oldByHandle := map[string]Preset{}
	for _, p := range old {
		if !p.PresetHandle.Null {
			oldByHandle[handleKey(p.PresetHandle.Value)] = p
		}
	}
	types := map[tdef.PresetScenarioEnum]PresetType{}
	for _, t := range r.types {
		types[t.PresetScenario] = t
	}
	names := map[tdef.PresetScenarioEnum][]*string{}
	counts := map[tdef.PresetScenarioEnum]int{}
	seen := map[string]bool{}
	for index, p := range presets {
		name := presetName(p)
		builtIn := !p.BuiltIn.Null && p.BuiltIn.Value
		scenario := presetScenarioName(p.PresetScenario)
		if !p.PresetHandle.Null {
			key := handleKey(p.PresetHandle.Value)
			if seen[key] {
				return spec.Errorf(im.StatusConstraintError, "Duplicate presetHandle %s in new Presets", key)
			}
			oldPreset, known := oldByHandle[key]
			switch {
			case known:
				if !p.BuiltIn.Null && oldPreset.BuiltIn != p.BuiltIn {
					return spec.Errorf(im.StatusConstraintError, "Cannot change built-in status of preset with presetHandle %s", key)
				}
			case seeding:
				// Initial seeding, where the application states the presets
				// the device ships with.
			case !issued[key]:
				return spec.Errorf(im.StatusNotFound, "Preset with presetHandle %s does not exist in old Presets, cannot add new Presets with non-null presetHandle", key)
			case builtIn:
				// The handle is one this write asked for, so the preset is
				// an addition whatever it now carries.
				return spec.Errorf(im.StatusConstraintError, "Can not add a new built-in preset")
			}
			seen[key] = true
		} else if builtIn {
			return spec.Errorf(im.StatusConstraintError, "Can not add a new built-in preset")
		}

		t, ok := types[p.PresetScenario]
		if !ok {
			return spec.Errorf(im.StatusConstraintError, "No PresetType defined for scenario %s", scenario)
		}
		if name != nil && t.PresetTypeFeatures&tdef.PresetTypeFeaturesSupportsNames == 0 {
			return spec.Errorf(im.StatusConstraintError, "Preset names are not supported for scenario %s", scenario)
		}
		// Mirrors matter.js ThermostatServer.ts:1919-1932 at b25ed960
		// (#validatePresetWriteRequest): the total and the per-scenario
		// count, for this appended preset.
		if index+1 > r.numberOfPresets {
			return spec.Errorf(im.StatusResourceExhausted, "Number of presets (%d) exceeds NumberOfPresets (%d)", len(presets), r.numberOfPresets)
		}
		if count := counts[p.PresetScenario]; count == int(t.NumberOfPresets) {
			return spec.Errorf(im.StatusResourceExhausted, "Number of presets (%d) for scenario %s exceeds allowed number (%d)", count+1, scenario, t.NumberOfPresets)
		}
		counts[p.PresetScenario]++
		// The specification counts null — "no name" — as a value that may
		// not repeat within a scenario (:1934-1943 at b25ed960).
		if slices.ContainsFunc(names[p.PresetScenario], func(n *string) bool { return equalName(n, name) }) {
			return spec.Errorf(im.StatusConstraintError, "Duplicate preset name %s for scenario %s", nameJSON(name), scenario)
		}
		names[p.PresetScenario] = append(names[p.PresetScenario], name)

		if r.cool {
			if p.CoolingSetpoint == nil {
				return spec.Errorf(im.StatusConstraintError, "Preset for scenario %s is missing required coolingSetpoint", scenario)
			}
			if c := int(*p.CoolingSetpoint); c < r.coolMin || c > r.coolMax {
				return spec.Errorf(im.StatusConstraintError, "Preset coolingSetpoint (%d) for scenario %s is out of bounds [%d, %d]", c, scenario, r.coolMin, r.coolMax)
			}
		}
		if r.heat {
			if p.HeatingSetpoint == nil {
				return spec.Errorf(im.StatusConstraintError, "Preset for scenario %s is missing required heatingSetpoint", scenario)
			}
			if h := int(*p.HeatingSetpoint); h < r.heatMin || h > r.heatMax {
				return spec.Errorf(im.StatusConstraintError, "Preset heatingSetpoint (%d) for scenario %s is out of bounds [%d, %d]", h, scenario, r.heatMin, r.heatMax)
			}
		}
	}
	return nil
}

func equalName(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// nameJSON is a name as matter.js's JSON.stringify prints it.
func nameJSON(n *string) string {
	if n == nil {
		return "null"
	}
	return fmt.Sprintf("%q", *n)
}

// normalizePresetCommit is matter.js #normalizePresets and
// #validatePresetCommit (ThermostatServer.ts:1516-1537, :1544-1580), which
// the stored write runs one after the other (:1310-1311): it issues a
// handle to every preset
// lacking one (recorded in issued), resolves a null BuiltIn — false for a
// seed or a new preset, the stored preset's value otherwise — and refuses
// the removal of a built-in preset and of the active preset. presets is
// normalised in place.
func normalizePresetCommit(presets, old []Preset, seeding bool, issued map[string]bool, active []byte) error {
	handles := map[string]bool{}
	for i := range presets {
		p := &presets[i]
		if p.PresetHandle.Null {
			h := make([]byte, presetHandleLength)
			if _, err := rand.Read(h); err != nil {
				return fmt.Errorf("thermo: issue a preset handle: %w", err)
			}
			p.PresetHandle = spec.ValueOf(h)
			issued[handleKey(h)] = true
		}
		handles[handleKey(p.PresetHandle.Value)] = true
		if !p.BuiltIn.Null {
			continue
		}
		p.BuiltIn = spec.ValueOf(false)
		if seeding {
			continue
		}
		for _, o := range old {
			if !o.PresetHandle.Null && bytes.Equal(o.PresetHandle.Value, p.PresetHandle.Value) {
				p.BuiltIn = o.BuiltIn
				break
			}
		}
	}
	newBuiltIn := map[string]bool{}
	for _, p := range presets {
		if !p.BuiltIn.Null && p.BuiltIn.Value {
			newBuiltIn[handleKey(p.PresetHandle.Value)] = true
		}
	}
	for _, o := range old {
		if !o.BuiltIn.Null && o.BuiltIn.Value && !o.PresetHandle.Null && !newBuiltIn[handleKey(o.PresetHandle.Value)] {
			return spec.Errorf(im.StatusConstraintError, "Cannot remove built-in preset with presetHandle %s", handleKey(o.PresetHandle.Value))
		}
	}
	// The specification refuses the removal of the active preset, not a
	// handle that named no preset to begin with (:1567-1579).
	if active != nil && !handles[handleKey(active)] && slices.ContainsFunc(old, func(o Preset) bool {
		return !o.PresetHandle.Null && bytes.Equal(o.PresetHandle.Value, active)
	}) {
		return spec.Errorf(im.StatusInvalidInState, "Cannot remove preset %s while it is the active preset", handleKey(active))
	}
	return nil
}

// lockPersist takes [ThermostatServer.persist]; unlockPersist releases it.
func (s *ThermostatServer) lockPersist()   { s.persist <- struct{}{} }
func (s *ThermostatServer) unlockPersist() { <-s.persist }

// savePresets hands presets and the active handle to the PresetStore; a
// server without one keeps them in memory only. After construction the
// caller holds persist, read the pair under it and stores what it saved
// before it releases it (R2-P6-store-1501); see [PresetStore] for what
// that asks of the store.
func (s *ThermostatServer) savePresets(ctx context.Context, presets []Preset, active []byte) error {
	if s.presetStore == nil {
		return nil
	}
	if err := s.presetStore.SavePresets(ctx, EncodePresets(presets), bytes.Clone(active)); err != nil {
		return fmt.Errorf("thermo: save presets: %w", err)
	}
	return nil
}

// applyPresets validates, normalises, persists and stores presets — the
// stored write of matter.js (persistedPresets$Changing,
// #handlePersistedPresetsChanging, ThermostatServer.ts:1300-1320, then the
// commit). The caller holds s.op; presets are the caller's and are
// normalised in place. local is a local write: with MSCH, a preset a
// stored schedule names cannot be removed (#validatePresetCommit,
// ThermostatServer.ts:2032-2065 at b25ed960); an atomic commit checked
// that against its own unit's schedules before (atomicChanged).
func (s *ThermostatServer) applyPresets(ctx context.Context, presets []Preset, local bool) error {
	s.lockPersist()
	defer s.unlockPersist()
	s.mu.RLock()
	old := s.presets
	active := bytes.Clone(s.activePresetHandle)
	rules := s.presetRules()
	var inUse error
	if local {
		inUse = s.presetsInUseBySchedules(old, presets, s.schedules)
	}
	s.mu.RUnlock()
	issued := map[string]bool{}
	if err := rules.validatePresetWrite(presets, old, false, issued); err != nil {
		return err
	}
	if err := normalizePresetCommit(presets, old, false, issued, active); err != nil {
		return err
	}
	if inUse != nil {
		return inUse
	}
	if err := s.savePresets(ctx, presets, active); err != nil {
		return err
	}
	s.mu.Lock()
	s.presets = presets
	s.mu.Unlock()
	// Presets is computed from persistedPresets, whose change announces it
	// (#handlePersistedPresetsChanged, ThermostatServer.ts:1508-1510).
	s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrPresets)
	return nil
}

// commitPresets applies the presets an atomic write staged, as matter.js's
// commit does: its checks before the transaction commits ($AtomicChanging,
// ThermostatServer.ts:1289-1291, $AtomicChanged, :1500-1502) are
// precommitAtomic's; the stored write then validates, normalises and
// checks them (:1300-1320) — the commit assigns the staged value, which
// the Presets setter stores for a command (:1687-1690,
// AtomicWriteHandler.ts:299). The caller holds s.op.
func (s *ThermostatServer) commitPresets(ctx context.Context, staged []Preset) error {
	return s.applyPresets(ctx, deepcopy.Each(staged), false)
}

// SetPresets is a local write of the Presets attribute — matter.js
// `state.presets = …` with a local actor, which stores at once
// (ThermostatServer.ts:1687-1690, hasLocalActor): the presets are
// validated as a controller's would be against the stored ones (a handle
// the device has not issued is NOT_FOUND, counts per type, names only with
// SupportsNames, the setpoints within the limits), new presets get a
// handle, a built-in preset and the active preset cannot be removed, and
// the result is saved to the [PresetStore] and announced. The error is the
// refusal, with its IM status. presets is not modified.
func (s *ThermostatServer) SetPresets(ctx context.Context, presets []Preset) error {
	if s.features&ThermostatFeaturePRES == 0 {
		return spec.Errorf(im.StatusUnsupportedAttribute, "thermo: Presets needs the Presets feature")
	}
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.applyPresets(ctx, deepcopy.Each(presets), true); err != nil {
		return err
	}
	s.reevaluateOnPresetsChange(ctx)
	return nil
}

// Presets returns a copy of the stored presets (matter.js
// persistedPresets).
func (s *ThermostatServer) Presets() []Preset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepcopy.Each(s.presets)
}

// ActivePresetHandle returns the ActivePresetHandle, nil for null.
func (s *ThermostatServer) ActivePresetHandle() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return bytes.Clone(s.activePresetHandle)
}

// SetActivePresetHandle sets ActivePresetHandle locally (nil for null) — a
// local state change in matter.js, held to the attribute's "max 16" and
// nothing else; it is saved and announced.
func (s *ThermostatServer) SetActivePresetHandle(ctx context.Context, handle []byte) error {
	if s.features&ThermostatFeaturePRES == 0 {
		return spec.Errorf(im.StatusUnsupportedAttribute, "thermo: ActivePresetHandle needs the Presets feature")
	}
	if handle != nil {
		if _, err := s.inst.CheckValue(tdef.AttrActivePresetHandle, handle, nil); err != nil {
			return err
		}
	}
	s.op.Lock()
	defer s.op.Unlock()
	reevaluate, err := s.storeActiveHandle(ctx, bytes.Clone(handle))
	if reevaluate {
		s.reevaluateSuggestions(ctx)
	}
	return err
}

// ActivatePreset activates the preset handle names (nil: none) with its
// setpoints, as the application's setActivePresetRequest does in
// matter.js: the SetActivePresetRequest command's path, run as the host —
// a handle that names no stored preset is INVALID_COMMAND; otherwise it
// becomes ActivePresetHandle (saved, announced, ActivePresetChange
// emitted) and the setpoints of the occupancy in effect take the preset's,
// each clamped to its limits, and the preset stays active (the moved
// setpoints are the activation's, not a direct write). It is the path a
// followed thermostat suggestion takes too: the moved setpoints reach the
// [Sink] before they are stored, so the Sink must not call back into the
// server. A handle that leaves the current thermostat suggestion's preset
// overrides it. [ThermostatServer.SetActivePresetHandle] sets the handle
// alone. Mirrors matter.js ThermostatServer.ts:551-553 at b25ed960
// (setActivePresetRequest: handleSetActivePresetRequest, :523-538, then
// #applyPresetSetpoints, :556-567); the preset staying active is the
// method's documented contract (:505-521).
func (s *ThermostatServer) ActivatePreset(ctx context.Context, handle []byte) error {
	if s.features&ThermostatFeaturePRES == 0 {
		return spec.Errorf(im.StatusUnsupportedCommand, "thermo: SetActivePresetRequest needs the Presets feature")
	}
	handle = bytes.Clone(handle) // nil stays null, an empty handle empty
	s.op.Lock()
	defer s.op.Unlock()
	return s.activatePreset(contract.WithOrigin(ctx, contract.OriginHostSet), handle)
}

// storeActiveHandle saves and stores the active handle and reports
// whether the change started or ended a thermostat suggestion's override
// (trackOverrideLocked): the caller then re-evaluates the suggestions,
// once it holds no persist. The caller holds s.op.
func (s *ThermostatServer) storeActiveHandle(ctx context.Context, handle []byte) (reevaluate bool, err error) {
	s.lockPersist()
	defer s.unlockPersist()
	s.mu.RLock()
	presets := s.presets
	same := bytes.Equal(s.activePresetHandle, handle) && (s.activePresetHandle == nil) == (handle == nil)
	s.mu.RUnlock()
	if same {
		return false, nil
	}
	if err := s.savePresets(ctx, presets, handle); err != nil {
		return false, err
	}
	s.mu.Lock()
	s.activePresetHandle = handle
	reevaluate = s.trackOverrideLocked(handle, false)
	s.mu.Unlock()
	s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrActivePresetHandle)
	return reevaluate, nil
}

// clearActivePresetOnDirectWrite clears the active preset after a direct
// (not a command's) setpoint change of the setpoints in effect — matter.js
// #assert(Un)occupied*SetpointChanging (ThermostatServer.ts:1150-1166,
// :1203-1220 at b25ed960) through #leavePresetOnSetpointWrite (:764-789):
// a setpoint someone set by hand is no longer the preset's. It takes persist and s.mu, never s.op, so the inbound setters
// may call it while a write waits on the Sink (issue #63); persist keeps
// its save from overtaking a preset write's (R2-P6-store-1501). It
// reports whether the cleared handle started a thermostat suggestion's
// override (trackOverrideLocked): the caller re-evaluates the suggestions
// then — at once when it holds s.op, else scheduled.
func (s *ThermostatServer) clearActivePresetOnDirectWrite(ctx context.Context, unoccupied bool) (reevaluate bool) {
	if s.features&ThermostatFeaturePRES == 0 {
		return false
	}
	s.lockPersist()
	s.mu.Lock()
	if s.activePresetHandle == nil || s.occupiedLocked() == unoccupied {
		s.mu.Unlock()
		s.unlockPersist()
		return false
	}
	previous := s.activePresetHandle
	s.activePresetHandle = nil
	reevaluate = s.trackOverrideLocked(nil, true)
	presets := s.presets
	s.mu.Unlock()
	err := s.savePresets(ctx, presets, nil)
	s.unlockPersist()
	if err != nil {
		s.logger().Warn("matter.thermostat.active_preset_not_saved", slog.String("reason", err.Error()))
	}
	s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrActivePresetHandle)
	// chip clears the active preset after a write of a setpoint in effect
	// through SetActivePreset(null), which emits ActivePresetChange
	// (ThermostatCluster.h:215-224, ThermostatClusterPresets.cpp:499).
	s.emitActivePresetChange(ctx, previous, nil)
	return reevaluate
}

// presetRequest normalises the SetActivePresetRequest payload: the bridge
// decodes it through the generated definition.
func presetRequest(fields any) (tdef.SetActivePresetRequestRequest, error) {
	switch v := fields.(type) {
	case tdef.SetActivePresetRequestRequest:
		return v, nil
	case *tdef.SetActivePresetRequestRequest:
		if v != nil {
			return *v, nil
		}
	}
	return tdef.SetActivePresetRequestRequest{}, spec.Errorf(im.StatusInvalidCommand, "thermo: SetActivePresetRequest carried %T", fields)
}

// handleSetActivePresetRequest is matter.js setActivePresetRequest
// (ThermostatServer.ts:281-316): a handle that names no stored preset is
// INVALID_COMMAND; otherwise the handle becomes the active one (null
// clears it) and — beyond the specification, as matter.js does — the
// setpoints in effect take the preset's, each clamped to its limits. The
// setpoints move in a command's context: no limit assert, the reconcile
// runs, the active handle stays. With a [Sink] the moved setpoints reach
// it before anything is stored, as SetpointRaiseLower's do.
func (s *ThermostatServer) handleSetActivePresetRequest(ctx context.Context, fields any) error {
	req, err := presetRequest(fields)
	if err != nil {
		return err
	}
	var handle []byte
	if !req.PresetHandle.Null {
		handle = bytes.Clone(req.PresetHandle.Value)
		if handle == nil {
			handle = []byte{}
		}
	}
	s.op.Lock()
	defer s.op.Unlock()
	return s.activatePreset(ctx, handle)
}

// activatePreset makes handle the active preset (nil: none) as
// SetActivePresetRequest does — the body of
// [ThermostatServer.handleSetActivePresetRequest], which a followed
// thermostat suggestion runs too (suggestions.go). The caller holds s.op.
func (s *ThermostatServer) activatePreset(ctx context.Context, handle []byte) error {
	s.mu.RLock()
	var preset *Preset
	if handle != nil {
		for i := range s.presets {
			p := s.presets[i]
			if !p.PresetHandle.Null && bytes.Equal(p.PresetHandle.Value, handle) {
				preset = &p
				break
			}
		}
	}
	st := s.setpointState()
	unoccupied := !s.occupiedLocked()
	previous := bytes.Clone(s.activePresetHandle)
	s.mu.RUnlock()
	if handle != nil && preset == nil {
		return spec.Errorf(im.StatusInvalidCommand, "Requested PresetHandle not found")
	}
	s.logger().Info("matter.thermostat.active_preset", slog.String("preset_handle", handleKey(handle)))

	if preset != nil {
		next := st
		var moved []string
		if st.heat && preset.HeatingSetpoint != nil {
			lo, hi := st.effectiveLimits(true)
			next.setSetpoint(true, unoccupied, min(max(int(*preset.HeatingSetpoint), lo), hi))
			moved = append(moved, setpointKey(true, unoccupied))
		}
		if st.cool && preset.CoolingSetpoint != nil {
			lo, hi := st.effectiveLimits(false)
			next.setSetpoint(false, unoccupied, min(max(int(*preset.CoolingSetpoint), lo), hi))
			moved = append(moved, setpointKey(false, unoccupied))
		}
		for _, key := range moved {
			if err := next.reconcile(key); err != nil {
				return err
			}
		}
		if err := s.applyCommandSetpoints(ctx, st, next, "SetActivePresetRequest"); err != nil {
			return err
		}
	}
	reevaluate, err := s.storeActiveHandle(ctx, handle)
	if err != nil {
		return err
	}
	// chip's SetActivePreset emits the event on every accepted request,
	// the unchanged handle included (ThermostatClusterPresets.cpp:461-500).
	s.emitActivePresetChange(ctx, previous, handle)
	if reevaluate {
		s.reevaluateSuggestions(ctx)
	}
	return nil
}

// limitCoveringStored is the value for a limit attribute that the stored
// list does not exceed: a configuration that lowers a limit below what
// clients already stored raises it, with a notice, and the stored entries
// stay. Mirrors matter.js ThermostatServer.ts:393-399 at b25ed960
// (#limitCoveringStored), applied to NumberOfPresets at :299-304.
func (s *ThermostatServer) limitCoveringStored(name string, limit uint8, required int, found string) uint8 {
	if required <= int(limit) {
		return limit
	}
	raised := uint8(min(required, math.MaxUint8)) //nolint:gosec // clamped to a uint8
	s.logger().Info("matter.thermostat.limit_raised",
		slog.String("attribute", name), slog.Int("value", int(raised)),
		slog.String("reason", fmt.Sprintf("%s is %d but %s", name, limit, found)))
	return raised
}

// seedPresets is matter.js initialize's preset part
// (ThermostatServer.ts:294-319 at b25ed960) with the validation its commit
// runs (persistedPresets$Changing with no old value, :1774-1791): the
// stored presets when the PresetStore has them — NumberOfPresets raised to
// cover them — else the configured ones — validated as a seed and
// normalised — saved; then an ActivePresetHandle that matches no preset is
// cleared with a warning.
func (s *ThermostatServer) seedPresets(cfg ThermostatConfig) error {
	ctx := context.Background()
	if v, err := s.inst.CheckValue(tdef.AttrNumberOfPresets, cfg.NumberOfPresets, nil); err != nil {
		return fmt.Errorf("%w: NumberOfPresets: %w", ErrPresets, err)
	} else if n, ok := v.(uint64); ok {
		s.numberOfPresets = uint8(n) //nolint:gosec // a uint8 per CheckValue
	}
	if _, err := s.inst.CheckValue(tdef.AttrPresetTypes, spec.List[PresetType](cfg.PresetTypes), nil); err != nil {
		return fmt.Errorf("%w: PresetTypes: %w", ErrPresets, err)
	}
	s.presetTypes = slices.Clone(cfg.PresetTypes)
	s.presetStore = cfg.PresetStore

	active := bytes.Clone(cfg.ActivePresetHandle)
	var presets []Preset
	loaded := false
	if s.presetStore != nil {
		b, h, ok, err := s.presetStore.LoadPresets(ctx)
		if err != nil {
			return fmt.Errorf("%w: load: %w", ErrPresets, err)
		}
		if ok {
			if presets, err = DecodePresets(b); err != nil {
				return fmt.Errorf("%w: %w", ErrPresets, err)
			}
			active, loaded = bytes.Clone(h), true
			s.numberOfPresets = s.limitCoveringStored("NumberOfPresets", s.numberOfPresets, len(presets), fmt.Sprintf("%d presets are stored", len(presets)))
		}
	}
	if !loaded {
		presets = deepcopy.Each(cfg.Presets)
		if presets == nil {
			presets = []Preset{}
		}
		issued := map[string]bool{}
		if err := s.presetRules().validatePresetWrite(presets, nil, true, issued); err != nil {
			return fmt.Errorf("%w: %w", ErrPresets, err)
		}
		if err := normalizePresetCommit(presets, nil, true, issued, active); err != nil {
			return fmt.Errorf("%w: %w", ErrPresets, err)
		}
	}
	s.presets = presets
	cleared := false
	if active != nil && !slices.ContainsFunc(presets, func(p Preset) bool {
		return !p.PresetHandle.Null && bytes.Equal(p.PresetHandle.Value, active)
	}) {
		s.logger().Warn("matter.thermostat.active_preset_cleared",
			slog.String("active_preset_handle", handleKey(active)), slog.String("reason", "matches no preset, reporting no active preset"))
		active, cleared = nil, true
	}
	s.activePresetHandle = active
	if !loaded || cleared {
		if err := s.savePresets(ctx, presets, active); err != nil {
			return fmt.Errorf("%w: %w", ErrPresets, err)
		}
	}
	return nil
}
