// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

// The fixtures below are matter.js's real-world commissioner
// certificates, copied verbatim from
// packages/protocol/test/certificate/TestCertificates.ts ("Amazon
// Certificates", "SmartThings Certificates"). Each ASN1 value is the
// TBSCertificate DER matter.js's Certificate.asUnsignedDer produces for
// the TLV next to it — the bytes the issuer signed. TBSToDER has to
// reproduce them byte for byte or the ECDSA check fails.
//
// Both roots carry their extensions in the TLV order basic-constraints,
// subject-key-id, key-usage, authority-key-id — not the tag order — and
// matter.js's extensionsToAsn1 (Certificate.ts:208) emits them in that
// arrival order because TlvObject.decodeTlvInternalValue
// (packages/types/src/tlv/TlvObject.ts:205-230) assigns the fields as
// the wire delivers them. Both ICACs carry basic-constraints path-len 0
// explicitly; the signed DER holds the `02 01 00`.
var matterJSCommissionerFixtures = []struct {
	name     string
	rootTLV  string
	rootASN1 string
	icacTLV  string
	nocTLV   string
	nocASN1  string
	// at is a wall time inside every certificate's validity window.
	at time.Time
}{
	{
		name:     "Amazon",
		rootTLV:  "15300111008f7e89d8d67b0aecdeae6d212deaa3b724020137032714d894477fd1553601182604537ac72a2605e378cd5937062714d894477fd15536011824070124080130094104047302d8c1c429049055346a31cf2c7e62d7266a3ddeb60a854810d9a8d3ed50a2e90bb5f1fad51b29b727c153946b64718ae2422ea10dfc9b7db0cbc48164d4370a3501290118300414c6e675219d6a1ebb8e664f86d80fe11c3758b859240261300514c6e675219d6a1ebb8e664f86d80fe11c3758b85918300b40092a586ace30018a5fa614cca2c2b23c58ab6c75dc8e759f95a7ba13db97d84043b532505e0cf61c187d29c00f56f90a4952dfb195163d49bbb1f5402e52ad9818",
		rootASN1: "3082014ca0030201020211008f7e89d8d67b0aecdeae6d212deaa3b7300a06082a8648ce3d04030230223120301e060a2b0601040182a27c01040c1030313336353544313746343739344438301e170d3232303932383231333431315a170d3437303932383232333431315a30223120301e060a2b0601040182a27c01040c10303133363535443137463437393444383059301306072a8648ce3d020106082a8648ce3d03010703420004047302d8c1c429049055346a31cf2c7e62d7266a3ddeb60a854810d9a8d3ed50a2e90bb5f1fad51b29b727c153946b64718ae2422ea10dfc9b7db0cbc48164d4a3633061300f0603551d130101ff040530030101ff301d0603551d0e04160414c6e675219d6a1ebb8e664f86d80fe11c3758b859300e0603551d0f0101ff040403020186301f0603551d23041830168014c6e675219d6a1ebb8e664f86d80fe11c3758b859",
		icacTLV:  "1530011100a95aa1823a32582162f4b4116ea2561e24020137032714d894477fd1553601182604bd1e622d26054d7799493706271359cb602434233d1118240701240801300941044c47c47148c7c049120c92db2edefd7dd86db7ac6425ecaa8aecebf1143c384dcaa83a542e1d0d54487fb6cc7a0ae8c94442fe02d30acc8968808303d1a7092a370a3501290124020018300514c6e675219d6a1ebb8e664f86d80fe11c3758b859300414b1b4a84c6e069ff49099126e2074a554fe55d31e24026118300b4073d3fb9c4cf4ba22fffb4d9d4248be968646374cdfd8a79bf108379b74f7d1cf2569473ffc606e86ad301148ec964453e97c9a8b41f33588e97f5864162b7ad518",
		nocTLV:   "1530011100e2c7907d7c0ee8ca124370ceffb76fab2402013703271359cb602434233d1118260484e0ae2d260594f17a403706271522c119b02919a7002711f0155fc7aaf2f90218240701240801300941042f38b38e61fdd92077517638cfb7419448d418945ea0acc5fe5b09deae368cc1d840b2f383b6383f79c1f5c61d96929325e26c9d7e186267606398c8b2bba243370a3501280118300514b1b4a84c6e069ff49099126e2074a554fe55d31e300414aedfdbe5ae9ea1c1283977a8f11303abafbe67162402013603040204011818300b40c4ef43d01ed60d8a859ca73632aee482a3941a0f321e62590efbc8c7576af3d2e9ae1414de910f5983613cad8eff1a259f01f87af1d339f8962aa40e7f3672fc18",
		nocASN1:  "3082018fa003020102021100e2c7907d7c0ee8ca124370ceffb76fab300a06082a8648ce3d04030230223120301e060a2b0601040182a27c01030c1031313344323333343234363043423539301e170d3234303431343138343432305a170d3334303431323139343432305a30443120301e060a2b0601040182a27c01050c10303041373139323942303139433132323120301e060a2b0601040182a27c01010c10303246394632414143373546313546303059301306072a8648ce3d020106082a8648ce3d030107034200042f38b38e61fdd92077517638cfb7419448d418945ea0acc5fe5b09deae368cc1d840b2f383b6383f79c1f5c61d96929325e26c9d7e186267606398c8b2bba243a38183308180300c0603551d130101ff04023000301f0603551d23041830168014b1b4a84c6e069ff49099126e2074a554fe55d31e301d0603551d0e04160414aedfdbe5ae9ea1c1283977a8f11303abafbe6716300e0603551d0f0101ff04040302078030200603551d250101ff0416301406082b0601050507030206082b06010505070301",
		at:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	},
	{
		name:     "SmartThings",
		rootTLV:  "1530011100e2e22382cf8b0a5d6154721aad16888124020137032714ff5ac811916947d718260414aab32a2605240b1b3437062714ff5ac811916947d7182407012408013009410410d18ee66a35bb151ca6981395bb2b12d0b00705436f565de05636c8aae4a510cc36325f7c5b04286bc914808314f191d19813da78a884a9cca0bf9e4ec040db370a3501290118300414f3776b9803d4cd4c55269ef01bd50beb21152605240261300514f3776b9803d4cd4c55269ef01bd50beb2115260518300b40cf7be64b09641ec320dc30df26a5d873a306f8f75a7ecffe8be5bd55639e0a0f6e8126dc969268d1279ccd71cf112ccc178288c7d81367d0aa28b492a5ca049018",
		rootASN1: "3082014ca003020102021100e2e22382cf8b0a5d6154721aad168881300a06082a8648ce3d04030230223120301e060a2b0601040182a27c01040c1044373437363939313131433835414646301e170d3232303931333230353233365a170d3237303931333231353233365a30223120301e060a2b0601040182a27c01040c10443734373639393131314338354146463059301306072a8648ce3d020106082a8648ce3d0301070342000410d18ee66a35bb151ca6981395bb2b12d0b00705436f565de05636c8aae4a510cc36325f7c5b04286bc914808314f191d19813da78a884a9cca0bf9e4ec040dba3633061300f0603551d130101ff040530030101ff301d0603551d0e04160414f3776b9803d4cd4c55269ef01bd50beb21152605300e0603551d0f0101ff040403020186301f0603551d23041830168014f3776b9803d4cd4c55269ef01bd50beb21152605",
		icacTLV:  "153001110090c76c01f23e1a7cc5cbb128f48b886224020137032714ff5ac811916947d7182604c280812d2605230b1b3437062713022dccf7da9b7da11824070124080130094104fbd2352ba2afe25d154609778b87c4c8fcc6c6cb4a672ba65113596df71d0bac22620e8aa27a40630484f0c62749560dad6d6ba800f6246bdfb1d23e3a391a51370a3501290124020018300514f3776b9803d4cd4c55269ef01bd50beb21152605300414c4744f525b764ea5c696073f21fd9deaae67e4ca24026018300b4065ba9477e323bf7496080ba77fc850855bc4082ea600a9fe30608b44caed98eff335a5ebc010f3311edecc8c63b98d9c704216ad6aa5c73f8ff5e1ee456437ef18",
		nocTLV:   "1530011100f4ba17882d33b60e2451346f61d7760024020137032713022dccf7da9b7da1182604250aaf2d2605b4a4192f3706271179370682516a35082715589b23d28795b7541824070124080130094104256f8cd94f656cc94baca821eaa185b019d7749111927c0ce8711602a782f13e396bc72f7284395b8643d36f7f66b0feeec8f16ca8e89a070cb6495d2d466278370a3501280118300514c4744f525b764ea5c696073f21fd9deaae67e4ca300414b2d4e028daa6113b41ab29b74a1fe27b4c3933612402053603040204011818300b404fc617c32047971feb679b473529110f55cf9f3227da2f9fcc71c29aeded037eabf6800e9a512651b3bb5be3635886dc498ecaa0f955348a85f033b6f52239ac18",
		at:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	},
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

// TestTBSToDER_MatchesMatterJSCommissionerFixtures locks TBSToDER to
// the TBSCertificate bytes matter.js produces for the Amazon and
// SmartThings roots (and the Amazon NOC): the extension order of the
// rebuilt DER is the TLV arrival order, as in Certificate.ts:208
// extensionsToAsn1.
func TestTBSToDER_MatchesMatterJSCommissionerFixtures(t *testing.T) {
	t.Parallel()
	for _, f := range matterJSCommissionerFixtures {
		t.Run(f.name+"/RCAC", func(t *testing.T) {
			t.Parallel()
			c, err := Decode(mustHex(t, f.rootTLV))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			got, err := TBSToDER(c)
			if err != nil {
				t.Fatalf("TBSToDER: %v", err)
			}
			if want := mustHex(t, f.rootASN1); !bytes.Equal(got, want) {
				t.Fatalf("TBS DER differs from matter.js\n got: %x\nwant: %x", got, want)
			}
		})
		if f.nocASN1 == "" {
			continue
		}
		t.Run(f.name+"/NOC", func(t *testing.T) {
			t.Parallel()
			c, err := Decode(mustHex(t, f.nocTLV))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			got, err := TBSToDER(c)
			if err != nil {
				t.Fatalf("TBSToDER: %v", err)
			}
			if want := mustHex(t, f.nocASN1); !bytes.Equal(got, want) {
				t.Fatalf("TBS DER differs from matter.js\n got: %x\nwant: %x", got, want)
			}
		})
	}
}

// TestValidateRCAC_MatterJSCommissionerRoots is the AddTrustedRootCertificate
// path: the Amazon and SmartThings roots self-verify. Before the
// extension order was taken from the TLV, both failed with
// "RCAC self-signature invalid" and commissioning from Alexa stopped at
// AddTrustedRootCertificate.
func TestValidateRCAC_MatterJSCommissionerRoots(t *testing.T) {
	t.Parallel()
	for _, f := range matterJSCommissionerFixtures {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			c, err := Decode(mustHex(t, f.rootTLV))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if err := ValidateRCAC(c); err != nil {
				t.Fatalf("ValidateRCAC: %v", err)
			}
		})
	}
}

// TestVerifier_MatterJSCommissionerChains is the AddNOC / CASE path:
// root → ICAC → NOC of both commissioners verifies and yields the NOC's
// public key.
func TestVerifier_MatterJSCommissionerChains(t *testing.T) {
	t.Parallel()
	for _, f := range matterJSCommissionerFixtures {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			root, err := Decode(mustHex(t, f.rootTLV))
			if err != nil {
				t.Fatalf("decode root: %v", err)
			}
			v, err := NewVerifier(root.PublicKey, FixedTime{T: f.at})
			if err != nil {
				t.Fatalf("NewVerifier: %v", err)
			}
			noc := mustHex(t, f.nocTLV)
			pub, err := v.VerifyAndExtractPubKey(noc, mustHex(t, f.icacTLV))
			if err != nil {
				t.Fatalf("VerifyAndExtractPubKey: %v", err)
			}
			nocCert, err := Decode(noc)
			if err != nil {
				t.Fatalf("decode noc: %v", err)
			}
			want, err := nocCert.PublicKeyECDSA()
			if err != nil {
				t.Fatalf("noc pubkey: %v", err)
			}
			if !pub.Equal(want) {
				t.Fatal("extracted public key is not the NOC's")
			}
		})
	}
}
