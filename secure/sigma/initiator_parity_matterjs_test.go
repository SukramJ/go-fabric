// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/secure/aesccm"
)

// Byte parity for the initiator's half of CASE, against the wire captures in
// matter.js packages/protocol/test/session/secure/CasePairingTest.ts
// ("generates the right bytes for sigma 3"). In that capture the matter.js
// node is the responder and its peer the initiator, so every value below is
// what an initiator produced — exactly the steps [Initiator.ProcessSigma2Bytes]
// performs: parse Sigma2 as received, salt S3K over the received bytes,
// build TBE3 + its signed data, seal it, frame Sigma3, derive the I2R key.
const (
	pjsSigma3Ecdh        = "04d1dbb16fd8ffda505a00f49c9af98eb5e98573708590575991aa06f09e295463f33cccf49c8df7c5b58f6d02c4f193485d4c9ae41cf4df0b89b223822adf7db5"
	pjsSigma3PeerEcdh    = "041d5a2e34457dcb4184b94bf7cb6f64689ee73d69282daa5fa5ee6fa5810f80c136ec7b8806679878cb045d9b25aa28a1a411c77677f5a1cf6f98c4acfeb9793c"
	pjsSigma3Sigma1      = "15300120dbf3b308b237d4a734ea0926a611706953fb83c80bf93dda569799777cd11fe1250244c23003206283cd5207ae1602ed4b07f27f4166453966c601b3c8a825dc39f18b1fb12a55300441041d5a2e34457dcb4184b94bf7cb6f64689ee73d69282daa5fa5ee6fa5810f80c136ec7b8806679878cb045d9b25aa28a1a411c77677f5a1cf6f98c4acfeb9793c35052501881325022c011818"
	pjsSigma3Sigma2      = "15300120c64d8172a7c8f77fadc6d6a35da29cb880ed54da6fa70d9e922ff96c12b13a4b2502ece930034104d1dbb16fd8ffda505a00f49c9af98eb5e98573708590575991aa06f09e295463f33cccf49c8df7c5b58f6d02c4f193485d4c9ae41cf4df0b89b223822adf7db531045c0186ce241016bdb0efe61b6ad8c57fc7e936773c7889994df8a891ae8cd469b02c87d36719d103a0890d03525ca253c598cabdb48aeafdedf7c4bdb56dbab4977af00d258aeba02d7999d92d47976c987be4aa2e28bc67bc868bb6c7ef1d636f1cb26e2cf41e5733991a6064fdfb1cac762938e60f492760219832830e05d712868343126c80983f9d8bfeb5bdb373368d88eb72e5179c07f75a5f8f622285a0c02b2642ebf96cb4394717188a313cf960ec0a07927cade2edbe901511a77902c440860d2f4a3191aeca6039bb8891bf82798ddeb3944a68f613f8d2fbaa1ca572474c2e99c456f1a86cf0bf9b547f45fe1069c33fb04429916b36cf17e6e65d61fe8b22d767b88218889eddc40c38335093c406c31c58d3a5770ef31899cda4ded48acf1394f1467e124f33925bd7e4d6e3e1d80a0b82783a3ce440178e9295f24a778bb315a084831238bfab5f46727a31f5d797bb7a91d72101fb3635052501881325022c011818"
	pjsSigma3Sigma3      = "1531014c01d3e009b49a46a2492a96fc8391dd90a53ad5de4fadc911a524036bbaf439577bf4ce9bb4b5cab2942c8c6e8d5666b3ce7449e0fb290833291641f40a366a4a9fd5573b5913b932914e23f13d25353d4cad0ad6e2ecbb85163800580dfb7ea88c56bb67565e94542f49c2f0fe4a7de22ee0fecc85a28da31e32508c3ce1d30b4cfe7b7bb84d5c4425f20336ea816f4b9a04c6c0828e4f4b24eb4aacf29a4b6b419d85dcf02d222c9826691aa73864f9cd6b8ebfd94680b0db75f7356018284e2c9b3bcea807f73f5adaff5db005550e441921420ddc65500a2bd4a177875f71be4e293c35c8221867bf206c016d6d5add4ceba754fb3ad375a03061634d3a0b0ae6b0bafe272640c545f07cf7742d960f399a12c09b7bf7833678e048e039ee7b1a66e365817fff0d0d7e28edb7beae205a2974b3273ae23909e9bb482cdc5b05363265d78eb747536336b7ef18"
	pjsSigma3IPK         = "0c677d9b5ac585827b577470bd9bd516"
	pjsSigma3Shared      = "c1466cab85d7809732786f63cb3b014a51cd49d01f494fcd56665ba8e00e5502"
	pjsSigma3Key         = "5ba760e89d08efcfe22dbe5832684415"
	pjsSigma3Encrypted   = "d3e009b49a46a2492a96fc8391dd90a53ad5de4fadc911a524036bbaf439577bf4ce9bb4b5cab2942c8c6e8d5666b3ce7449e0fb290833291641f40a366a4a9fd5573b5913b932914e23f13d25353d4cad0ad6e2ecbb85163800580dfb7ea88c56bb67565e94542f49c2f0fe4a7de22ee0fecc85a28da31e32508c3ce1d30b4cfe7b7bb84d5c4425f20336ea816f4b9a04c6c0828e4f4b24eb4aacf29a4b6b419d85dcf02d222c9826691aa73864f9cd6b8ebfd94680b0db75f7356018284e2c9b3bcea807f73f5adaff5db005550e441921420ddc65500a2bd4a177875f71be4e293c35c8221867bf206c016d6d5add4ceba754fb3ad375a03061634d3a0b0ae6b0bafe272640c545f07cf7742d960f399a12c09b7bf7833678e048e039ee7b1a66e365817fff0d0d7e28edb7beae205a2974b3273ae23909e9bb482cdc5b05363265d78eb747536336b7ef"
	pjsSigma3Plain       = "153001f4153001010124020137032414001826048012542826058015203b3706241501261169b6010018240701240801300941041b5c00110e57c1c9bc0619ada179f31bb8c07c8b95b5b0f94ffb21acb87c4d307678026d858be56e2a2ad146b3a895480fc8a616c9199980bd620503f4a311e7370a3501280118240201360304020401183004144cf0ce8e72e489e884550045a3bc6469b135fa3c300514e766069362d7e35b79687161644d222bdde93a6818300b40fa1b589089ed43e6ea3dc500eb18b4933d735c3cf384246e641fe13701a46cfe5d5e7b8d9e6e6e7d203d24c23c7f8b56a7849b813c0bb8709a47ce089d19a71b18300340b474e61e26d3a4bb243890632e2c12a82263a9433f31e9c0303d7b3ffc61799bce5b219679c90c820eebb9af5fb8066c7d459b69cef49e37ccbb8c66534240f218"
	pjsSigma3SignedData  = "153001f4153001010124020137032414001826048012542826058015203b3706241501261169b6010018240701240801300941041b5c00110e57c1c9bc0619ada179f31bb8c07c8b95b5b0f94ffb21acb87c4d307678026d858be56e2a2ad146b3a895480fc8a616c9199980bd620503f4a311e7370a3501280118240201360304020401183004144cf0ce8e72e489e884550045a3bc6469b135fa3c300514e766069362d7e35b79687161644d222bdde93a6818300b40fa1b589089ed43e6ea3dc500eb18b4933d735c3cf384246e641fe13701a46cfe5d5e7b8d9e6e6e7d203d24c23c7f8b56a7849b813c0bb8709a47ce089d19a71b18300341041d5a2e34457dcb4184b94bf7cb6f64689ee73d69282daa5fa5ee6fa5810f80c136ec7b8806679878cb045d9b25aa28a1a411c77677f5a1cf6f98c4acfeb9793c30044104d1dbb16fd8ffda505a00f49c9af98eb5e98573708590575991aa06f09e295463f33cccf49c8df7c5b58f6d02c4f193485d4c9ae41cf4df0b89b223822adf7db518"
	pjsSigma3I2RKey      = "e3ffee2792dc6d8dee832e248d1df718"
	pjsSigma3Sigma2SID   = 0xe9ec
	pjsSigma3SessionIdle = 5000
)

func pjsHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("fixture hex: %v", err)
	}
	return b
}

// TestParityMatterJS_InitiatorSigma3Bytes walks the initiator's Sigma3
// construction step by step against the matter.js capture.
func TestParityMatterJS_InitiatorSigma3Bytes(t *testing.T) {
	t.Parallel()
	sigma1 := pjsHex(t, pjsSigma3Sigma1)
	sigma2Raw := pjsHex(t, pjsSigma3Sigma2)
	ipk := pjsHex(t, pjsSigma3IPK)

	// Sigma2 as received: the decoder reads matter.js's minimal-width
	// session parameters and the responder session id.
	sigma2, err := UnmarshalSigma2(sigma2Raw)
	if err != nil {
		t.Fatalf("UnmarshalSigma2(matter.js Sigma2): %v", err)
	}
	if sigma2.ResponderSessionID != pjsSigma3Sigma2SID {
		t.Errorf("responder session id = %#x, want %#x", sigma2.ResponderSessionID, pjsSigma3Sigma2SID)
	}
	if !bytes.Equal(sigma2.ResponderEphPubKey, pjsHex(t, pjsSigma3Ecdh)) {
		t.Error("responder ephemeral key differs from the capture")
	}
	if sigma2.SessionParams == nil || sigma2.SessionParams.SessionIdleInterval != pjsSigma3SessionIdle {
		t.Errorf("session params = %+v, want idle %d", sigma2.SessionParams, pjsSigma3SessionIdle)
	}

	// S3K is salted over the Sigma2 bytes as received.
	salt3 := sigma3Salt(ipk, sigma1, sigma2Raw)
	s3k, err := hkdfDerive(pjsHex(t, pjsSigma3Shared), salt3, HKDFInfoSigma3, SessionKeySize)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(s3k); got != pjsSigma3Key {
		t.Fatalf("S3K = %s, want %s", got, pjsSigma3Key)
	}

	// TBE3 plaintext: re-encoding the captured fields reproduces the
	// matter.js TlvEncryptedDataSigma3 bytes.
	plain := pjsHex(t, pjsSigma3Plain)
	tbe3, err := unmarshalTBE3(plain)
	if err != nil {
		t.Fatal(err)
	}
	if got := marshalTBE3(tbe3); !bytes.Equal(got, plain) {
		t.Fatalf("TBE3 plaintext differs from matter.js:\n got %x\nwant %x", got, plain)
	}

	// The initiator signs TlvSignedData(initNOC, initICAC, initEph, respEph).
	signed := signedDataBytes(tbe3.InitiatorNOC, tbe3.InitiatorICAC, pjsHex(t, pjsSigma3PeerEcdh), pjsHex(t, pjsSigma3Ecdh))
	if got := hex.EncodeToString(signed); got != pjsSigma3SignedData {
		t.Fatalf("signed data differs from matter.js:\n got %s\nwant %s", got, pjsSigma3SignedData)
	}

	// Seal TBE3 under S3K with the fixed Sigma3 nonce.
	c, err := aesccm.New(s3k)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal(nil, nonceTBE3, plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(sealed); got != pjsSigma3Encrypted {
		t.Fatalf("encrypted3 differs from matter.js")
	}

	// Frame Sigma3.
	sigma3 := Sigma3{Encrypted3: sealed}.Marshal()
	if got := hex.EncodeToString(sigma3); got != pjsSigma3Sigma3 {
		t.Fatalf("Sigma3 frame differs from matter.js:\n got %s\nwant %s", got, pjsSigma3Sigma3)
	}

	// I2R — the key the initiator encrypts with.
	final, err := hkdfDerive(pjsHex(t, pjsSigma3Shared), sessionKeysSalt(ipk, sigma1, sigma2Raw, sigma3), HKDFInfoSessionKeys, FinalKeyMaterialSize)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(final[:SessionKeySize]); got != pjsSigma3I2RKey {
		t.Fatalf("I2R key = %s, want %s", got, pjsSigma3I2RKey)
	}
}

// TestParityMatterJS_InitiatorParsesMatterJSSigma1 checks the initiator's
// Sigma1 shape against a matter.js-produced Sigma1: the five fields the
// initiator emits (random, session id, destination id, ephemeral key,
// session parameters) decode, and re-encoding them yields a Sigma1 the
// decoder reads back identically.
func TestParityMatterJS_InitiatorParsesMatterJSSigma1(t *testing.T) {
	t.Parallel()
	s1, err := UnmarshalSigma1(pjsHex(t, pjsSigma3Sigma1))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s1.InitiatorEphPubKey, pjsHex(t, pjsSigma3PeerEcdh)) {
		t.Error("ephemeral key differs from the capture")
	}
	if s1.InitiatorSessionParams == nil || s1.InitiatorSessionParams.SessionIdleInterval != 5000 || s1.InitiatorSessionParams.SessionActiveInterval != 300 {
		t.Errorf("session params = %+v", s1.InitiatorSessionParams)
	}
	back, err := UnmarshalSigma1(s1.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if back.InitiatorRandom != s1.InitiatorRandom || back.InitiatorSessionID != s1.InitiatorSessionID ||
		back.DestinationID != s1.DestinationID || *back.InitiatorSessionParams != *s1.InitiatorSessionParams {
		t.Fatal("Sigma1 does not survive a re-encode")
	}
}
