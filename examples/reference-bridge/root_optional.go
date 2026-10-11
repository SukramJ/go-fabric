// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	ethdiagdef "github.com/SukramJ/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/store"
)

// rootUserLabelsKey keeps the root's controller-written UserLabel list in
// the store's settings table, as JSON, the way the root NodeLabel is kept
// (persist.go): matter.js keeps labelList nonvolatile.
const rootUserLabelsKey = "user_label.root"

// aggregatorUserLabelsKey is rootUserLabelsKey for the Aggregator
// endpoint's UserLabel list.
const aggregatorUserLabelsKey = "user_label.aggregator"

// buildRootLabelsAndDiagnostics constructs the root's optional label,
// localization and diagnostics clusters: FixedLabel, UserLabel (persisted),
// LocalizationConfiguration, TimeFormatLocalization, UnitLocalization,
// SoftwareDiagnostics and EthernetNetworkDiagnostics. Every one is
// optional on RootNode; the detected values matter.js takes from Intl are
// this host's choice here: en-US (de-DE supported), 24-hour, Gregorian,
// Celsius.
func buildRootLabelsAndDiagnostics(ctx context.Context, st *store.Store) ([]contract.ClusterServer, error) {
	fixedLabel, userLabel, err := buildLabels(ctx, st, rootUserLabelsKey, []fixedlabeldef.LabelStruct{
		{Label: "role", Value: "bridge"},
		{Label: "example", Value: "reference"},
	})
	if err != nil {
		return nil, err
	}

	localization, err := mattercore.NewLocalizationConfiguration(mattercore.LocalizationConfigurationConfig{
		ActiveLocale:     "en-US",
		SupportedLocales: []string{"en-US", "de-DE"},
	})
	if err != nil {
		return nil, err
	}
	timeFormat, err := mattercore.NewTimeFormatLocalization(mattercore.TimeFormatLocalizationConfig{
		HourFormat:         tfldef.HourFormatV24Hr,
		ActiveCalendarType: tfldef.CalendarTypeGregorian,
	})
	if err != nil {
		return nil, err
	}
	units, err := mattercore.NewUnitLocalization(mattercore.UnitLocalizationConfig{})
	if err != nil {
		return nil, err
	}

	// No watermarks: the Go runtime keeps no heap high watermark, so the
	// WTRMRK feature and ResetWatermarks stay off.
	software, err := mattercore.NewSoftwareDiagnostics(mattercore.SoftwareDiagnosticsConfig{Heap: runtimeHeap{}})
	if err != nil {
		return nil, err
	}
	// The daemon binds every interface and knows no single Ethernet link,
	// so PHYRate, FullDuplex and CarrierDetect read null. TimeSinceReset is
	// not served: the model states no unit for it.
	ethernet, err := mattercore.NewEthernetNetworkDiagnostics(mattercore.EthernetNetworkDiagnosticsConfig{
		Reporter:   unknownEthernet{},
		Attributes: []uint32{ethdiagdef.AttrPhyRate, ethdiagdef.AttrFullDuplex, ethdiagdef.AttrCarrierDetect},
	})
	if err != nil {
		return nil, err
	}
	return []contract.ClusterServer{fixedLabel, userLabel, localization, timeFormat, units, software, ethernet}, nil
}

// buildLabels constructs one endpoint's FixedLabel, holding fixed, and
// UserLabel, its list restored from and persisted under key in the
// store's settings table. The root and the Aggregator each get a pair.
func buildLabels(ctx context.Context, st *store.Store, key string, fixed []fixedlabeldef.LabelStruct) (fixedLabel, userLabel contract.ClusterServer, err error) {
	fixedLabel, err = mattercore.NewFixedLabel(fixed)
	if err != nil {
		return nil, nil, err
	}

	var labels []userlabeldef.LabelStruct
	if v, ok, err := st.GetSetting(ctx, key); err == nil && ok {
		if err := json.Unmarshal([]byte(v), &labels); err != nil {
			labels = nil
		}
	}
	userLabel, err = mattercore.NewUserLabel(mattercore.UserLabelConfig{
		Labels: labels,
		OnWrite: func(ctx context.Context, l []userlabeldef.LabelStruct) error {
			b, err := json.Marshal(l)
			if err != nil {
				return fmt.Errorf("user labels: %w", err)
			}
			return st.SetSetting(context.WithoutCancel(ctx), key, string(b))
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return fixedLabel, userLabel, nil
}

// runtimeHeap reports the Go heap from runtime.MemStats: used is HeapAlloc
// ("bytes of allocated heap objects"); free is the heap memory the runtime
// holds from the OS without an object in it — HeapSys minus HeapReleased
// minus HeapAlloc, the sum of the two estimates the MemStats documentation
// gives (HeapIdle - HeapReleased, retained for future growth; HeapInuse -
// HeapAlloc, dedicated to size classes but unused).
type runtimeHeap struct{}

func (runtimeHeap) read() runtime.MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}

// CurrentHeapFree implements mattercore.HeapReporter.
func (h runtimeHeap) CurrentHeapFree() (uint64, bool) {
	m := h.read()
	return m.HeapSys - m.HeapReleased - m.HeapAlloc, true
}

// CurrentHeapUsed implements mattercore.HeapReporter.
func (h runtimeHeap) CurrentHeapUsed() (uint64, bool) { return h.read().HeapAlloc, true }

// unknownEthernet is an Ethernet reporter that cannot tell anything.
type unknownEthernet struct{}

func (unknownEthernet) PHYRate() (ethdiagdef.PHYRateEnum, bool) { return 0, false }
func (unknownEthernet) FullDuplex() (duplex, ok bool)           { return false, false }
func (unknownEthernet) CarrierDetect() (carrier, ok bool)       { return false, false }
func (unknownEthernet) TimeSinceReset() (uint64, bool)          { return 0, false }
