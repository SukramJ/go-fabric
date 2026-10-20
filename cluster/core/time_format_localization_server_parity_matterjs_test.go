// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/time-format-localization/TimeFormatLocalizationServerTest.ts
// at 862ad75f. matter.js builds the root with
// TimeFormatLocalizationServer.with("CalendarFormat") over the calendars
// Buddhist, Chinese and Coptic, 24-hour format, Coptic active
// (createEndpoint :13-27), and sets activeCalendarType inside an act; the
// Go form is the server with that configuration and a local MatterWrite of
// ActiveCalendarType. matter.js throws a ConstraintError naming the "in
// supportedCalendarTypes" constraint; the Go assertion is the IM status
// CONSTRAINT_ERROR the runtime answers.
package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	"github.com/SukramJ/go-fabric/im"
)

// TestTimeFormatLocalizationServerParityMatterJS ports the describe
// "TimeFormatLocalizationServer" (TimeFormatLocalizationServerTest.ts:29-48).
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

	// :30-35.
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

	// :37-47.
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
