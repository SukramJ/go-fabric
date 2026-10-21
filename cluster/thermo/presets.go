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
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The Presets feature of the Thermostat server: the port of matter.js
// packages/node/src/behaviors/thermostat/ThermostatServer.ts at 862ad75f —
// the seed and the active-handle check of initialize (:134-152),
// #validatePresetWriteRequest (:1356-1484), #normalizeAndValidatePresetCommit
// (:1508-1595), handleSetActivePresetRequest / setActivePresetRequest
// (:281-316) and the Presets accessor over persistedPresets (:1660-1722).

// Preset is the PresetStruct of the generated definition.
type Preset = tdef.PresetStruct

// PresetType is the PresetTypeStruct of the generated definition.
type PresetType = tdef.PresetTypeStruct

// ErrPresets reports a ThermostatConfig whose presets the server refuses:
// a NumberOfPresets or PresetTypes outside their constraints, configured
// presets the write rules refuse, or a [PresetStore] that fails to load.
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

// asStatus is the IM status an error carries, if any.
func asStatus(err error) (im.StatusCode, bool) {
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return sc.MatterStatusCode(), true
	}
	return 0, false
}

// clonePresets deep-copies presets: a write normalises the presets it is
// given, and the caller's slice is the caller's (matter.js deepCopy,
// ThermostatServer.ts:49-51).
func clonePresets(in []Preset) []Preset {
	if in == nil {
		return nil
	}
	out := make([]Preset, len(in))
	for i, p := range in {
		out[i] = p
		if !p.PresetHandle.Null {
			out[i].PresetHandle.Value = bytes.Clone(p.PresetHandle.Value)
		}
		if p.Name != nil {
			n := *p.Name
			out[i].Name = &n
		}
		if p.CoolingSetpoint != nil {
			c := *p.CoolingSetpoint
			out[i].CoolingSetpoint = &c
		}
		if p.HeatingSetpoint != nil {
			h := *p.HeatingSetpoint
			out[i].HeatingSetpoint = &h
		}
	}
	return out
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
// one value (ThermostatServer.ts:1387-1389).
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
// (ThermostatServer.ts:1356-1484): presets a client wants to store, against
// the presets they replace (seeding — old undefined — admits the handles
// the application states). issued are the handles this write's
// normalisation issued.
func (r presetRules) validatePresetWrite(presets, old []Preset, seeding bool, issued map[string]bool) error {
	if len(presets) > r.numberOfPresets {
		return spec.Errorf(im.StatusResourceExhausted, "Number of presets (%d) exceeds NumberOfPresets (%d)", len(presets), r.numberOfPresets)
	}
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
	for _, p := range presets {
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
		// The specification counts null — "no name" — as a value that may
		// not repeat within a scenario.
		if slices.ContainsFunc(names[p.PresetScenario], func(n *string) bool { return equalName(n, name) }) {
			return spec.Errorf(im.StatusConstraintError, "Duplicate preset name %s for scenario %s", nameJSON(name), scenario)
		}
		if name != nil && t.PresetTypeFeatures&tdef.PresetTypeFeaturesSupportsNames == 0 {
			return spec.Errorf(im.StatusConstraintError, "Preset names are not supported for scenario %s", scenario)
		}
		names[p.PresetScenario] = append(names[p.PresetScenario], name)
		if counts[p.PresetScenario] == int(t.NumberOfPresets) {
			return spec.Errorf(im.StatusResourceExhausted, "Number of presets (%d) for scenario %s exceeds allowed number (%d)", counts[p.PresetScenario], scenario, t.NumberOfPresets)
		}
		counts[p.PresetScenario]++

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

// normalizePresetCommit is matter.js #normalizeAndValidatePresetCommit
// (ThermostatServer.ts:1508-1595): it issues a handle to every preset
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
	// handle that named no preset to begin with (:1582-1594).
	if active != nil && !handles[handleKey(active)] && slices.ContainsFunc(old, func(o Preset) bool {
		return !o.PresetHandle.Null && bytes.Equal(o.PresetHandle.Value, active)
	}) {
		return spec.Errorf(im.StatusInvalidInState, "Cannot remove preset %s while it is the active preset", handleKey(active))
	}
	return nil
}

// savePresets hands presets and the active handle to the PresetStore; a
// server without one keeps them in memory only.
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
// #handlePersistedPresetsChanging, ThermostatServer.ts:1296-1313, then the
// commit). The caller holds s.op; presets are the caller's and are
// normalised in place.
func (s *ThermostatServer) applyPresets(ctx context.Context, presets []Preset) error {
	s.mu.RLock()
	old := s.presets
	active := bytes.Clone(s.activePresetHandle)
	rules := s.presetRules()
	s.mu.RUnlock()
	issued := map[string]bool{}
	if err := rules.validatePresetWrite(presets, old, false, issued); err != nil {
		return err
	}
	if err := normalizePresetCommit(presets, old, false, issued, active); err != nil {
		return err
	}
	if err := s.savePresets(ctx, presets, active); err != nil {
		return err
	}
	s.mu.Lock()
	s.presets = presets
	s.mu.Unlock()
	// Presets is computed from persistedPresets, whose change announces it
	// (#handlePersistedPresetsChanged, ThermostatServer.ts:1497-1503).
	s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrPresets)
	return nil
}

// commitPresets applies the presets an atomic write staged: validated as
// staged against the stored presets ($AtomicChanging, :1289-1291),
// normalised with the commit checks ($AtomicChanged, :1489-1495), then
// stored. The caller holds s.op.
func (s *ThermostatServer) commitPresets(ctx context.Context, staged []Preset) error {
	return s.applyPresets(ctx, clonePresets(staged))
}

// SetPresets is a local write of the Presets attribute — matter.js
// `state.presets = …` with a local actor, which stores at once
// (ThermostatServer.ts:1702-1706, hasLocalActor): the presets are
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
	return s.applyPresets(ctx, clonePresets(presets))
}

// Presets returns a copy of the stored presets (matter.js
// persistedPresets).
func (s *ThermostatServer) Presets() []Preset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePresets(s.presets)
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
	return s.storeActiveHandle(ctx, bytes.Clone(handle))
}

// storeActiveHandle saves and stores the active handle; the caller holds
// s.op.
func (s *ThermostatServer) storeActiveHandle(ctx context.Context, handle []byte) error {
	s.mu.RLock()
	presets := s.presets
	same := bytes.Equal(s.activePresetHandle, handle) && (s.activePresetHandle == nil) == (handle == nil)
	s.mu.RUnlock()
	if same {
		return nil
	}
	if err := s.savePresets(ctx, presets, handle); err != nil {
		return err
	}
	s.mu.Lock()
	s.activePresetHandle = handle
	s.mu.Unlock()
	s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrActivePresetHandle)
	return nil
}

// clearActivePresetOnDirectWrite clears the active preset after a direct
// (not a command's) setpoint change of the setpoints in effect — matter.js
// #assert(Un)occupied*SetpointChanging (ThermostatServer.ts:682-706,
// :744-768 at 862ad75f): a setpoint someone set by hand is no longer the
// preset's. It takes s.mu only, so the inbound setters may call it while a
// write waits on the Sink.
func (s *ThermostatServer) clearActivePresetOnDirectWrite(ctx context.Context, unoccupied bool) {
	if s.features&ThermostatFeaturePRES == 0 {
		return
	}
	s.mu.Lock()
	if s.activePresetHandle == nil || s.occupiedLocked() == unoccupied {
		s.mu.Unlock()
		return
	}
	s.activePresetHandle = nil
	presets := s.presets
	s.mu.Unlock()
	if err := s.savePresets(ctx, presets, nil); err != nil {
		s.logger().Warn("matter.thermostat.active_preset_not_saved", slog.String("reason", err.Error()))
	}
	s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrActivePresetHandle)
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
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.RLock()
	var preset *Preset
	if !req.PresetHandle.Null {
		for i := range s.presets {
			p := s.presets[i]
			if !p.PresetHandle.Null && bytes.Equal(p.PresetHandle.Value, req.PresetHandle.Value) {
				preset = &p
				break
			}
		}
	}
	st := s.setpointState()
	unoccupied := !s.occupiedLocked()
	s.mu.RUnlock()
	if !req.PresetHandle.Null && preset == nil {
		return spec.Errorf(im.StatusInvalidCommand, "Requested PresetHandle not found")
	}
	var handle []byte
	if !req.PresetHandle.Null {
		handle = bytes.Clone(req.PresetHandle.Value)
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
	return s.storeActiveHandle(ctx, handle)
}

// seedPresets is matter.js initialize's preset part
// (ThermostatServer.ts:134-152) with the validation its commit runs
// (persistedPresets$Changing with no old value, :1296-1313): the stored
// presets when the PresetStore has them, else the configured ones —
// validated as a seed and normalised — saved; then an ActivePresetHandle
// that matches no preset is cleared with a warning.
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
		}
	}
	if !loaded {
		presets = clonePresets(cfg.Presets)
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
