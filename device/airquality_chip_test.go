// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

// The host-value check of AirQuality (brief 27-B §6), ported by meaning
// from connectedhomeip's src/app/clusters/air-quality-server/tests/
// TestAirQualityCluster.cpp at 6170af84 against the kit's AirQuality
// server: chip's SetAirQuality (AirQualityCluster.cpp:63-90) refuses Fair,
// Moderate, VeryPoor and ExtremelyPoor with ConstraintError when their
// feature is off, accepts Unknown, Good and Poor always, and leaves the
// value as it was on a refusal. The kit's server holds the same check in
// its definition (the enum members' conformance, spec.Instance
// EnumSupported); a refused value is never served.

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	aqdef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
)

const allAirQualityFeatures = aqdef.FeatureFair | aqdef.FeatureModerate | aqdef.FeatureVeryPoor | aqdef.FeatureExtremelyPoor

func airQualitySensor(t *testing.T, features aqdef.Feature) *device.AirQualitySensor {
	t.Helper()
	d, err := device.NewAirQualitySensor(device.Options{StableKey: endpoint.StringKey("aq"), Name: "aq"}, aqdef.Declare{Features: features})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func setAirQuality(d *device.AirQualitySensor, v aqdef.AirQualityEnum) error {
	return d.AirQuality().Set(aqdef.Patch{AirQuality: &v})
}

func airQualityStatus(err error) im.StatusCode {
	var s interface{ MatterStatusCode() im.StatusCode }
	if errors.As(err, &s) {
		return s.MatterStatusCode()
	}
	if err == nil {
		return im.StatusSuccess
	}
	return im.StatusFailure
}

// AttributeList, ReadAttributes (the revision and the FeatureMap) and
// HasFeature / HasFeatureReturnsFalse / FeatureMapReadsZero: the FeatureMap
// is the selection, the attribute list AirQuality alone.
func TestChipAirQualityAttributesAndFeatureMap(t *testing.T) {
	t.Parallel()
	for _, f := range []aqdef.Feature{allAirQualityFeatures, 0} {
		d := airQualitySensor(t, f)
		srv := d.Server(aqdef.ClusterID)
		if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(f) {
			t.Errorf("FeatureMap %v, want 0x%X", fm, uint32(f))
		}
		if rev, _ := srv.MatterRead(cluster.AttrGlobalClusterRevision); rev != aqdef.Revision {
			t.Errorf("ClusterRevision %v, want %d", rev, aqdef.Revision)
		}
		if l, ok := srv.(interface{ MatterAttributes() []uint32 }); !ok || len(l.MatterAttributes()) != 1 || l.MatterAttributes()[0] != aqdef.AttrAirQuality {
			t.Errorf("attributes %v", l)
		}
	}
}

// SetAndGetAirQuality, SetSameValueNoChange and ReadAttributeAfterSet:
// with every feature each member is stored and read back, the same value
// twice included.
func TestChipAirQualitySetAndGet(t *testing.T) {
	t.Parallel()
	d := airQualitySensor(t, allAirQualityFeatures)
	for _, v := range []aqdef.AirQualityEnum{
		aqdef.AirQualityGood, aqdef.AirQualityGood, aqdef.AirQualityFair, aqdef.AirQualityModerate, aqdef.AirQualityPoor,
		aqdef.AirQualityVeryPoor, aqdef.AirQualityExtremelyPoor, aqdef.AirQualityUnknown,
	} {
		if err := setAirQuality(d, v); err != nil {
			t.Fatalf("set %d: %v", v, err)
		}
		if got := d.AirQuality().Get().AirQuality; got != v {
			t.Fatalf("read %d after setting %d", got, v)
		}
	}
}

// InvalidValueRejected: a value outside the enum is refused and the value
// stays. chip answers InvalidValue (:86); the kit's definition answers
// ConstraintError, as it does for any value outside an attribute's type.
func TestChipAirQualityInvalidValueRejected(t *testing.T) {
	t.Parallel()
	d := airQualitySensor(t, allAirQualityFeatures)
	if err := setAirQuality(d, aqdef.AirQualityPoor); err != nil {
		t.Fatal(err)
	}
	if err := setAirQuality(d, 0xFF); airQualityStatus(err) != im.StatusConstraintError {
		t.Fatalf("0xFF: %v, want ConstraintError", err)
	}
	if got := d.AirQuality().Get().AirQuality; got != aqdef.AirQualityPoor {
		t.Errorf("read %d after the refusal, want Poor", got)
	}
}

// FeatureConstrainedValuesRejected and PartialFeaturesFairOnly: a member
// whose feature is off is ConstraintError and never served; the members
// without a feature are always accepted.
func TestChipAirQualityFeatureConstrainedValues(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		features aqdef.Feature
		accepted []aqdef.AirQualityEnum
		refused  []aqdef.AirQualityEnum
	}{
		"no features": {0,
			[]aqdef.AirQualityEnum{aqdef.AirQualityUnknown, aqdef.AirQualityGood, aqdef.AirQualityPoor},
			[]aqdef.AirQualityEnum{aqdef.AirQualityFair, aqdef.AirQualityModerate, aqdef.AirQualityVeryPoor, aqdef.AirQualityExtremelyPoor}},
		"fair only": {aqdef.FeatureFair,
			[]aqdef.AirQualityEnum{aqdef.AirQualityFair, aqdef.AirQualityGood, aqdef.AirQualityPoor, aqdef.AirQualityUnknown},
			[]aqdef.AirQualityEnum{aqdef.AirQualityModerate, aqdef.AirQualityVeryPoor, aqdef.AirQualityExtremelyPoor}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := airQualitySensor(t, tc.features)
			for _, v := range tc.accepted {
				if err := setAirQuality(d, v); err != nil {
					t.Errorf("set %d: %v", v, err)
				}
			}
			if err := setAirQuality(d, aqdef.AirQualityGood); err != nil {
				t.Fatal(err)
			}
			for _, v := range tc.refused {
				if err := setAirQuality(d, v); airQualityStatus(err) != im.StatusConstraintError {
					t.Errorf("set %d: %v, want ConstraintError", v, err)
				}
				if got := d.AirQuality().Get().AirQuality; got != aqdef.AirQualityGood {
					t.Errorf("served %v after refusing %d, want Good", got, v)
				}
			}
		})
	}
}
