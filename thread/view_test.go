// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/thread"
)

// chipDataset is the dataset of connectedhomeip's
// TestThreadBorderRouterManagementCluster.cpp:303-308 at 6170af84:
// Active Timestamp 0, Channel 11, Channel Mask 0x07fff800, Extended PAN
// ID dead00beef00cafe, Mesh-Local Prefix fd00:0db8:a00:0::/64 …,
// "OpenThread", PAN ID 0xc115, PSKc and Security Policy.
var chipDataset = []byte{
	0x0e, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x0b, 0x35, 0x06,
	0x00, 0x04, 0x00, 0x1f, 0xff, 0xe0, 0x02, 0x08, 0xde, 0xaa, 0x00, 0xbe, 0xef, 0x00, 0xca, 0xef, 0x07,
	0x08, 0xfd, 0xde, 0xad, 0x00, 0xbe, 0xef, 0x00, 0x00, 0x05, 0x10, 0xb7, 0x28, 0x08, 0x04, 0x85, 0xcf,
	0xc5, 0x25, 0x7f, 0x68, 0x4c, 0x54, 0x9d, 0x6a, 0x57, 0x5e, 0x03, 0x0a, 0x4f, 0x70, 0x65, 0x6e, 0x54,
	0x68, 0x72, 0x65, 0x61, 0x64, 0x01, 0x02, 0xc1, 0x15, 0x04, 0x10, 0xcb, 0x13, 0x47, 0xeb, 0x0c, 0xd4,
	0xb3, 0x5c, 0xd1, 0x42, 0xda, 0x5e, 0x6d, 0xf1, 0x8b, 0x88, 0x0c, 0x04, 0x02, 0xa0, 0xf7, 0xf8,
}

// TestDatasetViewReadsChipsDataset reads every getter of chip's view off
// chip's own test dataset (ThreadOperationalDataset.cpp:193-310).
func TestDatasetViewReadsChipsDataset(t *testing.T) {
	t.Parallel()
	v, err := thread.NewDatasetView(chipDataset)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v.Bytes(), chipDataset) || !v.IsCommissioned() {
		t.Fatal("the view does not hold the commissioned dataset")
	}
	ts, err := v.ActiveTimestamp()
	if err != nil || ts != 0 {
		t.Errorf("ActiveTimestamp = %d, %v", ts, err)
	}
	ch, err := v.Channel()
	if err != nil || ch != 11 {
		t.Errorf("Channel = %d, %v", ch, err)
	}
	pan, err := v.PANID()
	if err != nil || pan != 0xc115 {
		t.Errorf("PANID = %#x, %v", pan, err)
	}
	xp, err := v.ExtPANID()
	if err != nil || !bytes.Equal(xp, []byte{0xde, 0xaa, 0x00, 0xbe, 0xef, 0x00, 0xca, 0xef}) {
		t.Errorf("ExtPANID = %x, %v", xp, err)
	}
	name, err := v.NetworkName()
	if err != nil || name != "OpenThread" {
		t.Errorf("NetworkName = %q, %v", name, err)
	}
	for label, get := range map[string]func() ([]byte, error){
		"MeshLocalPrefix": v.MeshLocalPrefix, "NetworkKey": v.NetworkKey, "PSKc": v.PSKc, "ChannelMask": v.ChannelMask,
	} {
		if b, err := get(); err != nil || len(b) == 0 {
			t.Errorf("%s = %x, %v", label, b, err)
		}
	}
	sp, err := v.SecurityPolicy()
	if err != nil || sp.RotationTime != 0x02a0 || sp.Flags != 0xf7f8 {
		t.Errorf("SecurityPolicy = %+v, %v", sp, err)
	}
	if _, err := v.DelayTimer(); !errors.Is(err, thread.ErrTLVNotFound) {
		t.Errorf("DelayTimer = %v, want ErrTLVNotFound", err)
	}
}

// TestDatasetViewIsValid is IsValid (:151-164): the empty blob, a
// truncated header, a value past the end, the extended-length form and a
// blob over 254 bytes.
func TestDatasetViewIsValid(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		blob []byte
		ok   bool
	}{
		{"empty", nil, true},
		{"chip's dataset", chipDataset, true},
		{"truncated header", []byte{0x00}, false},
		{"value past the end", []byte{0x03, 0x05, 'a'}, false},
		{"extended length", []byte{0x03, 0xff, 0x00, 0x01, 'a'}, false},
		{"over 254 bytes", append([]byte{0x7f, 0xfd}, make([]byte, 253)...), false},
	} {
		if got := thread.ValidDataset(c.blob); got != c.ok {
			t.Errorf("%s: ValidDataset = %v, want %v", c.name, got, c.ok)
		}
		if _, err := thread.NewDatasetView(c.blob); (err == nil) != c.ok || (err != nil && !errors.Is(err, thread.ErrInvalidDataset)) {
			t.Errorf("%s: NewDatasetView = %v", c.name, err)
		}
	}
}

// TestDatasetViewFirstOccurrenceAndSizes pins chip's reading where the
// matter.js codec reads differently: Locate takes the first sub-TLV of a
// type (:173-186), the one-byte Channel and a Channel off page 0 are
// invalid (:202-211), a Network Name over 16 bytes is invalid and an
// empty one is not (:257-265), an empty Channel Mask is invalid
// (:285-292), and a missing TLV is not found.
func TestDatasetViewFirstOccurrenceAndSizes(t *testing.T) {
	t.Parallel()
	twoNames := []byte{0x03, 0x01, 'a', 0x03, 0x01, 'b'}
	v, _ := thread.NewDatasetView(twoNames)
	if name, err := v.NetworkName(); err != nil || name != "a" {
		t.Errorf("NetworkName of two = %q, %v, want the first", name, err)
	}
	ds, err := thread.DecodeDataset(twoNames)
	if err != nil || *ds.NetworkName != "b" {
		t.Errorf("the matter.js codec reads %v, %v; it takes the last", ds, err)
	}
	for _, c := range []struct {
		name string
		blob []byte
		get  func(thread.DatasetView) error
		want error
	}{
		{"one-byte Channel", []byte{0x00, 0x01, 0x0f}, func(v thread.DatasetView) error { _, err := v.Channel(); return err }, thread.ErrTLVInvalid},
		{"Channel on page 2", []byte{0x00, 0x03, 0x02, 0x00, 0x0f}, func(v thread.DatasetView) error { _, err := v.Channel(); return err }, thread.ErrTLVInvalid},
		{"17-byte name", append([]byte{0x03, 17}, bytes.Repeat([]byte{'n'}, 17)...), func(v thread.DatasetView) error { _, err := v.NetworkName(); return err }, thread.ErrTLVInvalid},
		{"empty name", []byte{0x03, 0x00}, func(v thread.DatasetView) error { _, err := v.NetworkName(); return err }, nil},
		{"empty Channel Mask", []byte{0x35, 0x00}, func(v thread.DatasetView) error { _, err := v.ChannelMask(); return err }, thread.ErrTLVInvalid},
		{"short PAN ID", []byte{0x01, 0x01, 0x01}, func(v thread.DatasetView) error { _, err := v.PANID(); return err }, thread.ErrTLVInvalid},
		{"short timestamp", []byte{0x0e, 0x01, 0x01}, func(v thread.DatasetView) error { _, err := v.ActiveTimestamp(); return err }, thread.ErrTLVInvalid},
		{"short policy", []byte{0x0c, 0x01, 0x01}, func(v thread.DatasetView) error { _, err := v.SecurityPolicy(); return err }, thread.ErrTLVInvalid},
		{"short delay", []byte{0x34, 0x01, 0x01}, func(v thread.DatasetView) error { _, err := v.DelayTimer(); return err }, thread.ErrTLVInvalid},
		{"delay", []byte{0x34, 0x04, 0, 0, 0x01, 0}, func(v thread.DatasetView) error { _, err := v.DelayTimer(); return err }, nil},
		{"no PAN ID", nil, func(v thread.DatasetView) error { _, err := v.PANID(); return err }, thread.ErrTLVNotFound},
		{"unnamed type", []byte{0xfe, 0x00}, func(v thread.DatasetView) error {
			if !v.Has(0xfe) {
				return errors.New("Has(0xfe) false")
			}
			_, err := v.Locate(0xfd)
			if err {
				return errors.New("Locate(0xfd) found")
			}
			return nil
		}, nil},
	} {
		v, err := thread.NewDatasetView(c.blob)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err := c.get(v); !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if (thread.DatasetView{}).IsCommissioned() {
		t.Error("the empty view is commissioned")
	}
}
