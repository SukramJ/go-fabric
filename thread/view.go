// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The sizes of connectedhomeip's dataset view
// (src/lib/support/ThreadOperationalDataset.h:36-42 at 6170af84).
const (
	// MaxDatasetLen is kSizeOperationalDataset, the largest operational
	// dataset (:36).
	MaxDatasetLen = 254
	// MaxNetworkNameLen is kSizeNetworkName (:38).
	MaxNetworkNameLen = 16
	// ExtPANIDLen is kSizeExtendedPanId (:39).
	ExtPANIDLen = 8
	// NetworkKeyLen is kSizeMasterKey (:40).
	NetworkKeyLen = 16
	// MeshLocalPrefixLen is kSizeMeshLocalPrefix (:41).
	MeshLocalPrefixLen = 8
	// PSKcLen is kSizePSKc (:42).
	PSKcLen = 16
)

var (
	// ErrInvalidDataset is a blob [NewDatasetView] refuses: chip's
	// CHIP_ERROR_INVALID_ARGUMENT from OperationalDatasetView::Init
	// (src/lib/support/ThreadOperationalDataset.cpp:166-171 at 6170af84).
	ErrInvalidDataset = errors.New("thread: not a framed operational dataset")
	// ErrTLVNotFound is a sub-TLV the dataset does not carry: chip's
	// CHIP_ERROR_TLV_TAG_NOT_FOUND of the getters (:196, :205, …).
	ErrTLVNotFound = errors.New("thread: dataset sub-TLV not found")
	// ErrTLVInvalid is a sub-TLV of a size chip's getter refuses: its
	// CHIP_ERROR_INVALID_TLV_ELEMENT (:197, :206-208, …).
	ErrTLVInvalid = errors.New("thread: dataset sub-TLV of an invalid size")
)

// ValidDataset is connectedhomeip's OperationalDatasetView::IsValid
// (src/lib/support/ThreadOperationalDataset.cpp:151-164 at 6170af84): at
// most [MaxDatasetLen] bytes, every TLV inside the blob, none in the
// extended-length form (a length byte of 0xFF, ThreadTLV::kLengthEscape,
// :47, "not allowed in a dataset TLV"). The empty blob is valid.
func ValidDataset(blob []byte) bool {
	if len(blob) > MaxDatasetLen {
		return false
	}
	for i := 0; i < len(blob); {
		if i+2 > len(blob) || blob[i+1] == 0xff {
			return false
		}
		i += 2 + int(blob[i+1])
		if i > len(blob) {
			return false
		}
	}
	return true
}

// DatasetView reads an operational dataset the way connectedhomeip's
// OperationalDatasetView does (src/lib/support/ThreadOperationalDataset.cpp
// :149-310 at 6170af84): each getter takes the first sub-TLV of its type
// ([DatasetView.Locate]) and holds it to the size chip demands. It is the
// view a server checks a dataset with — the Thread Network Directory, the
// Thread Border Router Management cluster — where [DecodeDataset], the
// commissioner's codec after matter.js, reads the last occurrence and
// accepts the one-byte Channel real border routers emit. A DatasetView
// shares its bytes with the blob it was made from.
type DatasetView struct{ data []byte }

// NewDatasetView is OperationalDatasetView::Init (:166-171): the view of
// a blob [ValidDataset] accepts, [ErrInvalidDataset] otherwise.
func NewDatasetView(blob []byte) (DatasetView, error) {
	if !ValidDataset(blob) {
		return DatasetView{}, ErrInvalidDataset
	}
	return DatasetView{data: blob}, nil
}

// Bytes is the dataset the view reads.
func (v DatasetView) Bytes() []byte { return v.data }

// Locate is OperationalDatasetView::Locate (:173-186): the value of the
// first sub-TLV of type t; false when there is none.
func (v DatasetView) Locate(t TLVType) ([]byte, bool) {
	for i := 0; i+2 <= len(v.data); {
		n := int(v.data[i+1])
		if TLVType(v.data[i]) == t && i+2+n <= len(v.data) {
			return v.data[i+2 : i+2+n], true
		}
		i += 2 + n
	}
	return nil, false
}

// Has reports whether the dataset carries a sub-TLV of type t
// (OperationalDatasetView::Has, ThreadOperationalDataset.h:239).
func (v DatasetView) Has(t TLVType) bool {
	_, ok := v.Locate(t)
	return ok
}

// IsCommissioned is OperationalDatasetView::IsCommissioned (:188-191):
// the dataset carries the PAN ID, the Network Key, the Extended PAN ID
// and the Channel.
func (v DatasetView) IsCommissioned() bool {
	return v.Has(TLVPANID) && v.Has(TLVNetworkKey) && v.Has(TLVExtPANID) && v.Has(TLVChannel)
}

// sized is the first sub-TLV of type t when ok accepts its length.
func (v DatasetView) sized(t TLVType, ok func(n int) bool) ([]byte, error) {
	value, found := v.Locate(t)
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrTLVNotFound, tlvName(t))
	}
	if !ok(len(value)) {
		return nil, fmt.Errorf("%w: %s of %d bytes", ErrTLVInvalid, tlvName(t), len(value))
	}
	return value, nil
}

func exactly(want int) func(int) bool { return func(n int) bool { return n == want } }

// tlvName is the matter.js name of a getter's TLV type; every getter's
// type has one.
func tlvName(t TLVType) string {
	name, _ := t.Name()
	return name
}

// ActiveTimestamp is GetActiveTimestamp (:193-200): eight bytes,
// big-endian.
func (v DatasetView) ActiveTimestamp() (uint64, error) {
	b, err := v.sized(TLVActiveTimestamp, exactly(8))
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b), nil
}

// Channel is GetChannel (:202-211): the three-byte form on channel page 0
// only.
func (v DatasetView) Channel() (uint16, error) {
	b, err := v.sized(TLVChannel, exactly(3))
	if err != nil {
		return 0, err
	}
	if b[0] != 0 {
		return 0, fmt.Errorf("%w: CHANNEL on page %d, not 0", ErrTLVInvalid, b[0])
	}
	return binary.BigEndian.Uint16(b[1:]), nil
}

// ExtPANID is GetExtendedPanIdAsByteSpan (:230-237): [ExtPANIDLen]
// bytes, sharing the view's memory.
func (v DatasetView) ExtPANID() ([]byte, error) {
	return v.sized(TLVExtPANID, exactly(ExtPANIDLen))
}

// MeshLocalPrefix is GetMeshLocalPrefix (:239-246): [MeshLocalPrefixLen]
// bytes.
func (v DatasetView) MeshLocalPrefix() ([]byte, error) {
	return v.sized(TLVMeshLocalPrefix, exactly(MeshLocalPrefixLen))
}

// NetworkKey is GetMasterKey (:248-255): [NetworkKeyLen] bytes.
func (v DatasetView) NetworkKey() ([]byte, error) {
	return v.sized(TLVNetworkKey, exactly(NetworkKeyLen))
}

// NetworkName is GetNetworkName (:257-265): at most [MaxNetworkNameLen]
// bytes; chip checks no minimum.
func (v DatasetView) NetworkName() (string, error) {
	b, err := v.sized(TLVNetworkName, func(n int) bool { return n <= MaxNetworkNameLen })
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// PANID is GetPanId (:267-274): two bytes, big-endian.
func (v DatasetView) PANID() (uint16, error) {
	b, err := v.sized(TLVPANID, exactly(2))
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

// PSKc is GetPSKc (:276-283): [PSKcLen] bytes.
func (v DatasetView) PSKc() ([]byte, error) {
	return v.sized(TLVPSKc, exactly(PSKcLen))
}

// ChannelMask is GetChannelMask (:285-292): a non-empty value.
func (v DatasetView) ChannelMask() ([]byte, error) {
	return v.sized(TLVChannelMask, func(n int) bool { return n > 0 })
}

// SecurityPolicy is GetSecurityPolicy (:294-301): four bytes, the
// rotation time and the flags.
func (v DatasetView) SecurityPolicy() (SecurityPolicy, error) {
	b, err := v.sized(TLVSecurityPolicy, exactly(4))
	if err != nil {
		return SecurityPolicy{}, err
	}
	return DecodeSecurityPolicy(b)
}

// DelayTimer is GetDelayTimer (:303-310): four bytes, big-endian
// milliseconds.
func (v DatasetView) DelayTimer() (uint32, error) {
	b, err := v.sized(TLVDelayTimer, exactly(4))
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}
