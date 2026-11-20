// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/time-format-localization/TimeFormatLocalizationServerTest.ts
// at b25ed960. matter.js builds the root with
// TimeFormatLocalizationServer.with("CalendarFormat") over the calendars
// Buddhist, Chinese and Coptic, 24-hour format, Coptic active
// (createEndpoint :57-71), and sets activeCalendarType inside an act; the
// Go form is the server with that configuration and a local MatterWrite of
// ActiveCalendarType. matter.js throws a ConstraintError naming the "in
// supportedCalendarTypes" constraint; the Go assertion is the IM status
// CONSTRAINT_ERROR the runtime answers.
//
// The start-up, write and restart cases (#4901) run DetectingServer,
// whose detected hour format is 24Hr and detected calendar Gregorian
// (:20-29), with its state set on the root (rootWith :31-33), and write as
// a controller (writeAttribute :46-55). The Go server has no detection of
// its own: the host passes the detected values as HourFormat and
// ActiveCalendarType, so DetectingServer's start is a Go start with 24Hr
// and Gregorian. A write is a MatterWrite on the built server; a restart
// is the host passing back the values OnWrite handed it.
package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	"github.com/SukramJ/go-fabric/im"
)

// TestTimeFormatLocalizationServerParityMatterJS ports the describe
// "TimeFormatLocalizationServer" (TimeFormatLocalizationServerTest.ts:73-97
// at b25ed960), the createEndpoint cases.
func TestTimeFormatLocalizationServerParityMatterJS(t *testing.T) {
	t.Parallel()
	create := func(t *testing.T) interface {
		MatterRead(uint32) (any, bool)
		MatterWrite(context.Context, uint32, any) error
	} {
		t.Helper()
		srv, err := core.NewTimeFormatLocalization(core.TimeFormatLocalizationConfig{
			HourFormat:         tfldef.HourFormatV24Hr,
			ActiveCalendarType: tfldef.CalendarTypeCoptic,
			SupportedCalendarTypes: []tfldef.CalendarTypeEnum{
				tfldef.CalendarTypeBuddhist, tfldef.CalendarTypeChinese, tfldef.CalendarTypeCoptic,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}

	// :80-85.
	t.Run("accepts valid date formats", func(t *testing.T) {
		t.Parallel()
		srv := create(t)
		if err := srv.MatterWrite(context.Background(), tfldef.AttrActiveCalendarType, uint8(tfldef.CalendarTypeChinese)); err != nil {
			t.Fatalf("ActiveCalendarType Chinese: %v", err)
		}
		if v, _ := srv.MatterRead(tfldef.AttrActiveCalendarType); v != tfldef.CalendarTypeChinese && v != uint8(tfldef.CalendarTypeChinese) {
			t.Errorf("ActiveCalendarType %v, want Chinese", v)
		}
	})

	// :87-97.
	t.Run("rejects invalid date formats", func(t *testing.T) {
		t.Parallel()
		srv := create(t)
		err := srv.MatterWrite(context.Background(), tfldef.AttrActiveCalendarType, uint8(tfldef.CalendarTypeGregorian))
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusConstraintError {
			t.Fatalf("ActiveCalendarType Gregorian: %v, want CONSTRAINT_ERROR", err)
		}
		if v, _ := srv.MatterRead(tfldef.AttrActiveCalendarType); v != tfldef.CalendarTypeCoptic && v != uint8(tfldef.CalendarTypeCoptic) {
			t.Errorf("ActiveCalendarType %v after the refusal, want Coptic", v)
		}
	})
}

// TestTimeFormatLocalizationServerStartParityMatterJS ports the start-up,
// write and restart cases of the describe "TimeFormatLocalizationServer"
// (TimeFormatLocalizationServerTest.ts:99-316 at b25ed960) where they have
// a Go subject. Left: the replacement of a configured or stored calendar
// the list lacks (:117-123, :146-158, :241-260, :262-279) and the warning
// that goes with it (:160-168) — ActiveCalendarType is always configured
// here, and one the list lacks is refused
// (BD-Matter-Localization-ConfiguredStartValueRefused); the count of
// detections (:125-137) and the type-level override (:281-293),
// TypeScript mechanisms; the server without CalendarFormat (:74-78), which
// the Go constructor does not build.
func TestTimeFormatLocalizationServerStartParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	build := func(t *testing.T, cfg core.TimeFormatLocalizationConfig) *spec.Server {
		t.Helper()
		srv, err := core.NewTimeFormatLocalization(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	// detecting is DetectingServer's start: 24Hr, Gregorian.
	detecting := func(supported ...tfldef.CalendarTypeEnum) core.TimeFormatLocalizationConfig {
		return core.TimeFormatLocalizationConfig{HourFormat: tfldef.HourFormatV24Hr, ActiveCalendarType: tfldef.CalendarTypeGregorian, SupportedCalendarTypes: supported}
	}
	read := func(t *testing.T, srv *spec.Server, attr uint32) uint8 {
		t.Helper()
		v, ok := srv.MatterRead(attr)
		if !ok {
			t.Fatalf("no attribute 0x%04X", attr)
		}
		return v.(uint8)
	}
	checkSupported := func(t *testing.T, srv *spec.Server, want ...tfldef.CalendarTypeEnum) {
		t.Helper()
		v, _ := srv.MatterRead(tfldef.AttrSupportedCalendarTypes)
		if got, w := enum8ListTLV(t, v), wantEnum8List(want...); !slices.Equal(got, w) {
			t.Errorf("SupportedCalendarTypes % X, want % X", got, w)
		}
	}

	// :99-105.
	t.Run("defaults to the detected hour format and calendar, listing the calendar as the only supported type", func(t *testing.T) {
		t.Parallel()
		srv := build(t, detecting())
		if got := read(t, srv, tfldef.AttrHourFormat); got != uint8(tfldef.HourFormatV24Hr) {
			t.Errorf("HourFormat %d, want 24Hr", got)
		}
		if got := read(t, srv, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeGregorian) {
			t.Errorf("ActiveCalendarType %d, want Gregorian", got)
		}
		checkSupported(t, srv, tfldef.CalendarTypeGregorian)
	})

	// :107-112.
	t.Run("treats an explicitly undefined SupportedCalendarTypes as unset", func(t *testing.T) {
		t.Parallel()
		cfg := detecting()
		cfg.SupportedCalendarTypes = nil
		srv := build(t, cfg)
		if got := read(t, srv, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeGregorian) {
			t.Errorf("ActiveCalendarType %d, want Gregorian", got)
		}
		checkSupported(t, srv, tfldef.CalendarTypeGregorian)
	})

	// :139-144.
	t.Run("builds SupportedCalendarTypes from a configured ActiveCalendarType", func(t *testing.T) {
		t.Parallel()
		cfg := detecting()
		cfg.ActiveCalendarType = tfldef.CalendarTypeBuddhist
		srv := build(t, cfg)
		if got := read(t, srv, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeBuddhist) {
			t.Errorf("ActiveCalendarType %d, want Buddhist", got)
		}
		checkSupported(t, srv, tfldef.CalendarTypeBuddhist)
	})

	// :170-176.
	t.Run("prefers the detected calendar when it is in the configured list", func(t *testing.T) {
		t.Parallel()
		srv := build(t, detecting(tfldef.CalendarTypeBuddhist, tfldef.CalendarTypeGregorian))
		if got := read(t, srv, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeGregorian) {
			t.Errorf("ActiveCalendarType %d, want Gregorian", got)
		}
	})

	// :178-188.
	t.Run("rejects a controller write of a calendar outside the supported list with CONSTRAINT_ERROR", func(t *testing.T) {
		t.Parallel()
		srv := build(t, detecting(tfldef.CalendarTypeBuddhist, tfldef.CalendarTypeGregorian))
		if err := srv.MatterWrite(ctx, tfldef.AttrActiveCalendarType, uint64(tfldef.CalendarTypeHebrew)); statusOf(err) != im.StatusConstraintError {
			t.Fatalf("write of Hebrew: %v, want CONSTRAINT_ERROR", err)
		}
		if got := read(t, srv, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeGregorian) {
			t.Errorf("ActiveCalendarType %d, want Gregorian", got)
		}
	})

	// :190-200.
	t.Run("accepts a controller write of a calendar in the supported list", func(t *testing.T) {
		t.Parallel()
		srv := build(t, detecting(tfldef.CalendarTypeBuddhist, tfldef.CalendarTypeGregorian))
		if err := srv.MatterWrite(ctx, tfldef.AttrActiveCalendarType, uint64(tfldef.CalendarTypeBuddhist)); statusOf(err) != im.StatusSuccess {
			t.Fatalf("write of Buddhist: %v, want SUCCESS", err)
		}
		if got := read(t, srv, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeBuddhist) {
			t.Errorf("ActiveCalendarType %d, want Buddhist", got)
		}
	})

	// :202-208.
	t.Run("stores HourFormat UseActiveLocale as written", func(t *testing.T) {
		t.Parallel()
		srv := build(t, detecting())
		if err := srv.MatterWrite(ctx, tfldef.AttrHourFormat, uint64(tfldef.HourFormatUseActiveLocale)); statusOf(err) != im.StatusSuccess {
			t.Fatalf("write of UseActiveLocale: %v, want SUCCESS", err)
		}
		if got := read(t, srv, tfldef.AttrHourFormat); got != uint8(tfldef.HourFormatUseActiveLocale) {
			t.Errorf("HourFormat %d, want UseActiveLocale", got)
		}
	})

	// :210-218.
	t.Run("rejects ActiveCalendarType UseActiveLocale while it is not a supported calendar", func(t *testing.T) {
		t.Parallel()
		srv := build(t, detecting())
		if err := srv.MatterWrite(ctx, tfldef.AttrActiveCalendarType, uint64(tfldef.CalendarTypeUseActiveLocale)); statusOf(err) != im.StatusConstraintError {
			t.Fatalf("write of UseActiveLocale: %v, want CONSTRAINT_ERROR", err)
		}
	})

	// :220-239.
	t.Run("keeps written HourFormat and ActiveCalendarType across a restart", func(t *testing.T) {
		t.Parallel()
		cfg := detecting(tfldef.CalendarTypeGregorian, tfldef.CalendarTypeBuddhist)
		stored := cfg
		cfg.OnWrite = func(_ context.Context, attr uint32, v uint8) error {
			switch attr {
			case tfldef.AttrHourFormat:
				stored.HourFormat = tfldef.HourFormatEnum(v)
			case tfldef.AttrActiveCalendarType:
				stored.ActiveCalendarType = tfldef.CalendarTypeEnum(v)
			}
			return nil
		}
		first := build(t, cfg)
		if err := first.MatterWrite(ctx, tfldef.AttrHourFormat, uint64(tfldef.HourFormatV12Hr)); statusOf(err) != im.StatusSuccess {
			t.Fatalf("write of 12Hr: %v, want SUCCESS", err)
		}
		if err := first.MatterWrite(ctx, tfldef.AttrActiveCalendarType, uint64(tfldef.CalendarTypeBuddhist)); statusOf(err) != im.StatusSuccess {
			t.Fatalf("write of Buddhist: %v, want SUCCESS", err)
		}
		second := build(t, stored)
		if got := read(t, second, tfldef.AttrHourFormat); got != uint8(tfldef.HourFormatV12Hr) {
			t.Errorf("HourFormat after the restart %d, want 12Hr", got)
		}
		if got := read(t, second, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeBuddhist) {
			t.Errorf("ActiveCalendarType after the restart %d, want Buddhist", got)
		}
	})

	// :295-316. The overridden detected formats arrive as the host's
	// configuration.
	t.Run("uses the overridden detected formats of a derived type", func(t *testing.T) {
		t.Parallel()
		srv := build(t, core.TimeFormatLocalizationConfig{
			HourFormat: tfldef.HourFormatV12Hr, ActiveCalendarType: tfldef.CalendarTypeBuddhist,
			SupportedCalendarTypes: []tfldef.CalendarTypeEnum{tfldef.CalendarTypeBuddhist},
		})
		if got := read(t, srv, tfldef.AttrHourFormat); got != uint8(tfldef.HourFormatV12Hr) {
			t.Errorf("HourFormat %d, want 12Hr", got)
		}
		if got := read(t, srv, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeBuddhist) {
			t.Errorf("ActiveCalendarType %d, want Buddhist", got)
		}
	})

	// Go's own: an empty, non-nil list is no configuration, as matter.js's
	// `!supportedCalendarTypes?.length` (TimeFormatLocalizationServer.ts:24
	// at b25ed960). Refused with ErrLocalizationConfig before (decision 296).
	t.Run("seeds an empty SupportedCalendarTypes with the ActiveCalendarType", func(t *testing.T) {
		t.Parallel()
		cfg := detecting()
		cfg.SupportedCalendarTypes = []tfldef.CalendarTypeEnum{}
		srv := build(t, cfg)
		if got := read(t, srv, tfldef.AttrActiveCalendarType); got != uint8(tfldef.CalendarTypeGregorian) {
			t.Errorf("ActiveCalendarType %d, want Gregorian", got)
		}
		checkSupported(t, srv, tfldef.CalendarTypeGregorian)
	})

	// Go's own (BD-Matter-Localization-ConfiguredStartValueRefused): a
	// configured calendar the configured list lacks is refused, where
	// matter.js replaces it (:146-158).
	t.Run("refuses a configured ActiveCalendarType that is not in the configured list", func(t *testing.T) {
		t.Parallel()
		cfg := detecting(tfldef.CalendarTypeBuddhist, tfldef.CalendarTypeGregorian)
		cfg.ActiveCalendarType = tfldef.CalendarTypeHebrew
		if _, err := core.NewTimeFormatLocalization(cfg); !errors.Is(err, core.ErrLocalizationConfig) {
			t.Fatalf("Hebrew of [Buddhist Gregorian]: %v, want ErrLocalizationConfig", err)
		}
	})
}
