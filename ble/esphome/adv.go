// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/SukramJ/go-fabric/transport/btp"
)

// AD types of the advertising data (Bluetooth Core Supplement, Part A, §1).
const (
	adServiceData16 = 0x16
	// matterServiceUUID16 is the Matter service's 16-bit UUID
	// (btp.ServiceUUIDShort).
	matterServiceUUID16 = 0xfff6
	// matterServiceDataLength is the length of the Matter service data a
	// commissionable peripheral advertises; matter.js ignores any other
	// (packages/nodejs-ble/src/NobleBleClient.ts:189-195), as the BlueZ
	// platform does.
	matterServiceDataLength = 8
)

// matterServiceData finds the Matter service data in raw advertising data:
// the AD structures of BluetoothLERawAdvertisement.data (api.proto:1897-1904;
// the proxy forwards them unparsed, ADV_IND and the scan response merged),
// each a length byte, a type byte and the data. It returns the service data
// of the 16-bit UUID 0xFFF6 when that is the 8-byte Matter advertisement
// btp.DecodeAdvertisementServiceData accepts; ok is false for every other
// advertisement.
func matterServiceData(ad []byte) (data []byte, ok bool) {
	for len(ad) > 0 {
		l := int(ad[0])
		if l == 0 {
			// A zero length ends the significant part.
			return nil, false
		}
		if l > len(ad)-1 {
			return nil, false
		}
		s := ad[1 : 1+l]
		ad = ad[1+l:]
		if s[0] != adServiceData16 || len(s) < 3 || binary.LittleEndian.Uint16(s[1:3]) != matterServiceUUID16 {
			continue
		}
		if d := s[3:]; isMatterServiceData(d) {
			return append([]byte(nil), d...), true
		}
	}
	return nil, false
}

// isMatterServiceData is the check the BlueZ platform makes on the
// service data of the Matter UUID.
func isMatterServiceData(d []byte) bool {
	if len(d) != matterServiceDataLength {
		return false
	}
	_, err := btp.DecodeAdvertisementServiceData(d)
	return err == nil
}

// macString formats the API's 48-bit address (an integer, most significant
// octet first; api.proto:1899) as the colon-separated MAC BlueZ reports.
func macString(a uint64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], a)
	s := strings.ToUpper(hex.EncodeToString(b[2:]))
	return s[0:2] + ":" + s[2:4] + ":" + s[4:6] + ":" + s[6:8] + ":" + s[8:10] + ":" + s[10:12]
}

// parseMAC is macString's inverse.
func parseMAC(s string) (uint64, error) {
	h := strings.ReplaceAll(s, ":", "")
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 6 {
		return 0, fmt.Errorf("esphome: %q is not a MAC address", s)
	}
	var w [8]byte
	copy(w[2:], b)
	return binary.BigEndian.Uint64(w[:]), nil
}

// uuidWords parses a 128-bit UUID string into the API's two words, high
// first (aioesphomeapi/model.py:1853-1873).
func uuidWords(s string) [2]uint64 {
	b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(b) != 16 {
		panic("esphome: bad UUID constant " + s)
	}
	return [2]uint64{binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:])}
}

// The UUIDs the GATT database is searched for.
var (
	serviceWords = uuidWords(btp.ServiceUUID)
	c1Words      = uuidWords(btp.C1CharacteristicUUID)
	c2Words      = uuidWords(btp.C2CharacteristicUUID)
	c3Words      = uuidWords(btp.C3CharacteristicUUID)
	// cccdWords is the Client Characteristic Configuration descriptor,
	// 0x2902 on the Bluetooth base UUID.
	cccdWords = uuidWords("00002902-0000-1000-8000-00805f9b34fb")
)

// shortWords is a 16- or 32-bit UUID on the Bluetooth base UUID
// (aioesphomeapi/model.py:1876-1878).
func shortWords(short uint32) [2]uint64 {
	return [2]uint64{uint64(short)<<32 | 0x1000, 0x800000805f9b34fb}
}
