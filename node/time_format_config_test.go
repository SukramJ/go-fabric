// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// tflRead reads one attribute of the root's TimeFormatLocalization as a
// controller does: its status and, on success, the value formatted.
func tflRead(t *testing.T, n *node.Node, attr uint32) (im.StatusCode, string) {
	t.Helper()
	res := endpoint.NewTopologyDispatcher(n.Bridge().Topology()).Read(t.Context(), attrPath(0, tfldef.ClusterID, attr))
	if len(res) != 1 {
		t.Fatalf("read 0x%04x: %d results", attr, len(res))
	}
	if res[0].Status != im.StatusSuccess {
		return res[0].Status, ""
	}
	return res[0].Status, fmt.Sprint(res[0].Value.Value)
}

// enumString formats enum values as a read of them is formatted.
func enumString[T ~uint8](v ...T) string {
	if len(v) == 1 {
		return fmt.Sprint(uint64(v[0]))
	}
	out := make([]uint64, len(v))
	for i, e := range v {
		out[i] = uint64(e)
	}
	return fmt.Sprint(out)
}

// TestTimeFormatConfig: Config.TimeFormat is the root
// TimeFormatLocalization's start (decision 360, O-3). nil keeps the
// node's start before the option, 24-hour and [Gregorian]; a stated
// start is what a controller reads; NoFeatures serves the server without
// CalendarFormat; a start the server refuses fails New with
// ErrLocalizationConfig, the node adding no default of its own.
func TestTimeFormatConfig(t *testing.T) {
	t.Parallel()
	served := map[string]struct {
		cfg                 *node.TimeFormatConfig
		hour                tfldef.HourFormatEnum
		features            uint32
		active, supported   string
		noCalendarAttribute bool
	}{
		"nil starts at 24-hour and Gregorian": {
			cfg: nil, hour: tfldef.HourFormatV24Hr, features: uint32(tfldef.FeatureCalendarFormat),
			active:    enumString(tfldef.CalendarTypeGregorian),
			supported: fmt.Sprint([]uint64{uint64(tfldef.CalendarTypeGregorian)}),
		},
		"a stated hour format, list and calendar are served": {
			cfg: &node.TimeFormatConfig{
				HourFormat:             new(tfldef.HourFormatV12Hr),
				ActiveCalendarType:     new(tfldef.CalendarTypeBuddhist),
				SupportedCalendarTypes: []tfldef.CalendarTypeEnum{tfldef.CalendarTypeGregorian, tfldef.CalendarTypeBuddhist, tfldef.CalendarTypeJapanese},
			},
			hour: tfldef.HourFormatV12Hr, features: uint32(tfldef.FeatureCalendarFormat),
			active:    enumString(tfldef.CalendarTypeBuddhist),
			supported: enumString(tfldef.CalendarTypeGregorian, tfldef.CalendarTypeBuddhist, tfldef.CalendarTypeJapanese),
		},
		"an unset calendar starts at the list's first": {
			cfg: &node.TimeFormatConfig{
				HourFormat:             new(tfldef.HourFormatV24Hr),
				SupportedCalendarTypes: []tfldef.CalendarTypeEnum{tfldef.CalendarTypeHebrew, tfldef.CalendarTypeGregorian},
			},
			hour: tfldef.HourFormatV24Hr, features: uint32(tfldef.FeatureCalendarFormat),
			active:    enumString(tfldef.CalendarTypeHebrew),
			supported: enumString(tfldef.CalendarTypeHebrew, tfldef.CalendarTypeGregorian),
		},
		"NoFeatures drops CalendarFormat": {
			cfg:  &node.TimeFormatConfig{HourFormat: new(tfldef.HourFormatV12Hr), NoFeatures: true},
			hour: tfldef.HourFormatV12Hr, features: 0, noCalendarAttribute: true,
		},
	}
	for name, tc := range served {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
			cfg.TimeFormat = tc.cfg
			n := newNode(t, cfg)
			if err := n.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, got := tflRead(t, n, tfldef.AttrHourFormat); got != enumString(tc.hour) {
				t.Errorf("HourFormat %s, want %s", got, enumString(tc.hour))
			}
			if _, got := tflRead(t, n, 0xFFFC); got != fmt.Sprint(tc.features) {
				t.Errorf("FeatureMap %s, want %d", got, tc.features)
			}
			if tc.noCalendarAttribute {
				for _, attr := range []uint32{tfldef.AttrActiveCalendarType, tfldef.AttrSupportedCalendarTypes} {
					if st, v := tflRead(t, n, attr); st != im.StatusUnsupportedAttribute {
						t.Errorf("attribute 0x%04x: status %v value %q, want UNSUPPORTED_ATTRIBUTE", attr, st, v)
					}
				}
				return
			}
			if _, got := tflRead(t, n, tfldef.AttrActiveCalendarType); got != tc.active {
				t.Errorf("ActiveCalendarType %s, want %s", got, tc.active)
			}
			if _, got := tflRead(t, n, tfldef.AttrSupportedCalendarTypes); got != tc.supported {
				t.Errorf("SupportedCalendarTypes %s, want %s", got, tc.supported)
			}
		})
	}

	refused := map[string]*node.TimeFormatConfig{
		"no hour format":            {ActiveCalendarType: new(tfldef.CalendarTypeGregorian)},
		"neither calendar nor list": {HourFormat: new(tfldef.HourFormatV24Hr)},
		"a calendar the list lacks": {
			HourFormat: new(tfldef.HourFormatV24Hr), ActiveCalendarType: new(tfldef.CalendarTypeKorean),
			SupportedCalendarTypes: []tfldef.CalendarTypeEnum{tfldef.CalendarTypeGregorian},
		},
		"a calendar beside NoFeatures": {
			HourFormat: new(tfldef.HourFormatV24Hr), ActiveCalendarType: new(tfldef.CalendarTypeGregorian), NoFeatures: true,
		},
	}
	for name, tf := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
			cfg.TimeFormat = tf
			n, err := node.New(t.Context(), cfg)
			if err == nil {
				_ = n.Stop(t.Context())
				t.Fatal("New accepted the start, want ErrLocalizationConfig")
			}
			if !errors.Is(err, mattercore.ErrLocalizationConfig) {
				t.Errorf("New: %v, want ErrLocalizationConfig", err)
			}
		})
	}
}

// TestTimeFormatConfigStoredParityMatterJS ports the second stored-value
// case of matter.js's describe "TimeFormatLocalizationServer"
// (packages/node/test/behaviors/time-format-localization/TimeFormatLocalizationServerTest.ts
// :262-279 at b25ed960), which needs a configured calendar list: the
// first run lists [Gregorian, Buddhist] and a controller writes Buddhist;
// the second lists [Chinese, Coptic] and starts at Chinese. matter.js
// takes the list's first because neither the stored nor its detected
// Gregorian is listed (TimeFormatLocalizationServer.ts:34-41); here an
// unset ActiveCalendarType starts at the list's first and the node's
// restore refuses the stored Buddhist the list lacks (node/nonvolatile.go
// restore), with one warning.
func TestTimeFormatConfigStoredParityMatterJS(t *testing.T) {
	t.Parallel()

	t.Run("falls back to the first supported calendar when the stored and the detected calendar are not listed", func(t *testing.T) {
		t.Parallel()
		st, eps := &settingsRecorder{Port: memstore.New()}, epmem.New()
		run := func(list []tfldef.CalendarTypeEnum, log *restoreWarnings) *node.Node {
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: st, Endpoints: eps}
			cfg.TimeFormat = &node.TimeFormatConfig{HourFormat: new(tfldef.HourFormatV24Hr), SupportedCalendarTypes: list}
			if log != nil {
				cfg.Logger = slog.New(log)
			}
			n, _ := nvRun(t, cfg, nvCase{})
			return n
		}
		first := run([]tfldef.CalendarTypeEnum{tfldef.CalendarTypeGregorian, tfldef.CalendarTypeBuddhist}, nil)
		if got := dispatchWrite(t, first, attrPath(0, tfldef.ClusterID, tfldef.AttrActiveCalendarType), uint64(tfldef.CalendarTypeBuddhist)); got != im.StatusSuccess {
			t.Fatalf("write of Buddhist: %v", got)
		}
		if err := first.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		key := st.keyEndingIn(t, fmt.Sprintf(".%04x.%04x", tfldef.ClusterID, tfldef.AttrActiveCalendarType))
		log := &restoreWarnings{}
		again := run([]tfldef.CalendarTypeEnum{tfldef.CalendarTypeChinese, tfldef.CalendarTypeCoptic}, log)
		if got := dispatchRead(t, again, attrPath(0, tfldef.ClusterID, tfldef.AttrActiveCalendarType)); got != enumString(tfldef.CalendarTypeChinese) {
			t.Errorf("ActiveCalendarType %s, want Chinese", got)
		}
		log.expectOneReset(t, key)
	})
}
