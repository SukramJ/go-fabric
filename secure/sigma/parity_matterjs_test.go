// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Mirrors matter.js packages/protocol/test/session/secure/CasePairingTest.ts.
// Each test case loads a vector from testdata/matterjs-vectors.json and
// asserts that go-fabric's CASE Sigma primitives produce byte-identical
// output to the matter.js HEAD reference values.
//
// Test-vector provenance:
//   - sigma2_*         — CasePairingTest.ts:14-63 (Sigma2 key schedule + AES-CCM)
//   - sigma3_*         — CasePairingTest.ts:75-129 (Sigma3 key schedule + AES-CCM)
//   - session_keys_*   — CasePairingTest.ts:117-128 (final session-key derivation)
//   - TestCasePairingSignature — CasePairingTest.ts:65-73 at 862ad75f
//     ("generates the right signature"), inline
//
// If any assertion fails that is a REAL drift against matter.js — stop,
// report, do not paper over.

package sigma

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/secure/aesccm"
)

//go:embed testdata/matterjs-vectors.json
var matterJSVectorsJSON []byte

// matterJSSigmaVector is the on-disk shape of one sigma test vector.
type matterJSSigmaVector struct {
	Label       string            `json:"label"`
	Description string            `json:"description"`
	Input       map[string]string `json:"input"`
	Expected    map[string]string `json:"expected"`
}

// loadMatterJSSigmaVectors parses and returns all vectors from the embedded JSON.
func loadMatterJSSigmaVectors(t *testing.T) []matterJSSigmaVector {
	t.Helper()
	var vecs []matterJSSigmaVector
	if err := json.Unmarshal(matterJSVectorsJSON, &vecs); err != nil {
		t.Fatalf("loadMatterJSSigmaVectors: %v", err)
	}
	return vecs
}

// mustHexSigma decodes a hex string; fatal on error.
func mustHexSigma(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("mustHex %q: %v", s, err)
	}
	return b
}

// mustNonce builds a 13-byte AES-CCM nonce from an ASCII label (zero-padded).
func mustNonce(t *testing.T, ascii string) []byte {
	t.Helper()
	n := make([]byte, aesccm.NonceSize)
	if len(ascii) > aesccm.NonceSize {
		t.Fatalf("nonce label %q too long (max %d)", ascii, aesccm.NonceSize)
	}
	copy(n, ascii)
	return n
}

// TestParityMatterJS_Sigma_Vectors runs all matter.js CASE Sigma parity cases.
func TestParityMatterJS_Sigma_Vectors(t *testing.T) {
	t.Parallel()
	vecs := loadMatterJSSigmaVectors(t)

	for _, v := range vecs {
		t.Run(v.Label, func(t *testing.T) {
			t.Parallel()
			switch v.Label {
			case "sigma2_salt_derivation":
				testSigma2Salt(t, v)
			case "sigma2_key_hkdf":
				testSigma2KeyHKDF(t, v)
			case "sigma2_aesccm_encryption":
				testSigma2AESCCMEncryption(t, v)
			case "sigma3_salt_derivation":
				testSigma3Salt(t, v)
			case "sigma3_key_hkdf":
				testSigma3KeyHKDF(t, v)
			case "sigma3_aesccm_decryption":
				testSigma3AESCCMDecryption(t, v)
			case "session_keys_derivation":
				testSessionKeysDerivation(t, v)
			default:
				t.Skipf("unknown vector label %q", v.Label)
			}
		})
	}
}

// testSigma2Salt verifies sigma2Salt() = IPK || respRandom || respEphPub || SHA256(sigma1).
// Mirrors matter.js CasePairingTest.ts:24-28.
func testSigma2Salt(t *testing.T, v matterJSSigmaVector) {
	t.Helper()
	ipk := mustHexSigma(t, v.Input["ipk"])
	respRandom := mustHexSigma(t, v.Input["responder_random"])
	respEphPub := mustHexSigma(t, v.Input["responder_eph_pub"])
	sigma1Bytes := mustHexSigma(t, v.Input["sigma1_bytes"])
	want := mustHexSigma(t, v.Expected["sigma2_salt"])

	got := sigma2Salt(ipk, respRandom, respEphPub, sigma1Bytes)

	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("sigma2Salt mismatch\n got=%x\nwant=%x", got, want)
	}
}

// testSigma2KeyHKDF verifies S2K = HKDF(sharedSecret, sigma2Salt, "Sigma2", 16).
// Mirrors matter.js CasePairingTest.ts:30-32.
func testSigma2KeyHKDF(t *testing.T, v matterJSSigmaVector) {
	t.Helper()
	sharedSecret := mustHexSigma(t, v.Input["shared_secret"])
	salt := mustHexSigma(t, v.Input["sigma2_salt"])
	info := v.Input["hkdf_info"]
	want := mustHexSigma(t, v.Expected["sigma2_key"])

	got, err := hkdfDerive(sharedSecret, salt, info, 16)
	if err != nil {
		t.Fatalf("hkdfDerive: %v", err)
	}

	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("sigma2Key mismatch\n got=%x\nwant=%x", got, want)
	}
}

// testSigma2AESCCMEncryption verifies AES-CCM Seal matches matter.js
// crypto.encrypt(sigma2Key, tbe2Plaintext, "NCASE_Sigma2N").
// Mirrors matter.js CasePairingTest.ts:58-62.
func testSigma2AESCCMEncryption(t *testing.T, v matterJSSigmaVector) {
	t.Helper()
	key := mustHexSigma(t, v.Input["sigma2_key"])
	nonce := mustNonce(t, v.Input["nonce_ascii"])
	plaintext := mustHexSigma(t, v.Input["plaintext_hex"])
	want := mustHexSigma(t, v.Expected["ciphertext_hex"])

	c, err := aesccm.New(key)
	if err != nil {
		t.Fatalf("aesccm.New: %v", err)
	}
	got, err := c.Seal(nil, nonce, plaintext, nil)
	if err != nil {
		t.Fatalf("aesccm.Seal: %v", err)
	}

	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("AES-CCM Sigma2 ciphertext mismatch\n got=%x\nwant=%x", got, want)
	}
}

// testSigma3Salt verifies sigma3Salt() = IPK || SHA256(sigma1 || sigma2).
// Mirrors matter.js CasePairingTest.ts:85-92.
func testSigma3Salt(t *testing.T, v matterJSSigmaVector) {
	t.Helper()
	ipk := mustHexSigma(t, v.Input["ipk"])
	sigma1Bytes := mustHexSigma(t, v.Input["sigma1_bytes"])
	sigma2Bytes := mustHexSigma(t, v.Input["sigma2_bytes"])
	want := mustHexSigma(t, v.Expected["sigma3_salt"])

	got := sigma3Salt(ipk, sigma1Bytes, sigma2Bytes)

	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("sigma3Salt mismatch\n got=%x\nwant=%x", got, want)
	}
}

// testSigma3KeyHKDF verifies S3K = HKDF(sharedSecret, sigma3Salt, "Sigma3", 16).
// Mirrors matter.js CasePairingTest.ts:94-96.
func testSigma3KeyHKDF(t *testing.T, v matterJSSigmaVector) {
	t.Helper()
	sharedSecret := mustHexSigma(t, v.Input["shared_secret"])
	salt := mustHexSigma(t, v.Input["sigma3_salt"])
	info := v.Input["hkdf_info"]
	want := mustHexSigma(t, v.Expected["sigma3_key"])

	got, err := hkdfDerive(sharedSecret, salt, info, 16)
	if err != nil {
		t.Fatalf("hkdfDerive: %v", err)
	}

	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("sigma3Key mismatch\n got=%x\nwant=%x", got, want)
	}
}

// testSigma3AESCCMDecryption verifies AES-CCM Open of the peer's
// encrypted3 payload matches the known TBE3 plaintext.
// Mirrors matter.js CasePairingTest.ts:98-102.
func testSigma3AESCCMDecryption(t *testing.T, v matterJSSigmaVector) {
	t.Helper()
	key := mustHexSigma(t, v.Input["sigma3_key"])
	nonce := mustNonce(t, v.Input["nonce_ascii"])
	ciphertext := mustHexSigma(t, v.Input["ciphertext_hex"])
	want := mustHexSigma(t, v.Expected["plaintext_hex"])

	c, err := aesccm.New(key)
	if err != nil {
		t.Fatalf("aesccm.New: %v", err)
	}
	got, err := c.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("aesccm.Open: %v", err)
	}

	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("AES-CCM Sigma3 plaintext mismatch\n got=%x\nwant=%x", got, want)
	}
}

// testSessionKeysDerivation verifies the final session decryptKey =
// HKDF(sharedSecret, IPK||SHA256(sigma1||sigma2||sigma3), "SessionKeys", 16).
// Mirrors matter.js CasePairingTest.ts:117-128.
//
// The matter.js test requests only 16 bytes (one direction key). In
// production go-fabric derives 48 bytes and splits into I2RKey /
// R2IKey / AttestationChallenge. The first 16 bytes must match the
// matter.js reference.
func testSessionKeysDerivation(t *testing.T, v matterJSSigmaVector) {
	t.Helper()
	sharedSecret := mustHexSigma(t, v.Input["shared_secret"])
	ipk := mustHexSigma(t, v.Input["ipk"])
	sigma1Bytes := mustHexSigma(t, v.Input["sigma1_bytes"])
	sigma2Bytes := mustHexSigma(t, v.Input["sigma2_bytes"])
	sigma3Bytes := mustHexSigma(t, v.Input["sigma3_bytes"])
	want := mustHexSigma(t, v.Expected["decrypt_key"])

	salt := sessionKeysSalt(ipk, sigma1Bytes, sigma2Bytes, sigma3Bytes)

	// Request 48 bytes (full production key material) — matter.js
	// CasePairingTest.ts:121-126 requests 16 and labels the result
	// "decryptKey". The spec labels it I2RKey; the matter.js test
	// uses the same HKDF parameters with L=16. Verify first 16 bytes.
	fullKeys, err := hkdfDerive(sharedSecret, salt, v.Input["hkdf_info"], FinalKeyMaterialSize)
	if err != nil {
		t.Fatalf("hkdfDerive: %v", err)
	}

	got := fullKeys[0:16]
	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Errorf("sessionKeys[0:16] mismatch\n got=%x\nwant=%x", got, want)
	}
}

// TestCasePairingSignature ports "generates the right signature"
// (CasePairingTest.ts:65-73): the fabric key's raw r||s signature over the
// signed-data bytes verifies, through the unpacking verifyTranscript uses.
func TestCasePairingSignature(t *testing.T) {
	t.Parallel()
	t.Run("generates the right signature", func(t *testing.T) {
		t.Parallel()
		fabricPubKey := mustHexSigma(t, "049bfa105c3d209ff226c31da689dafb297b73499ec1844bba89c60ce65938b722300dd0abbb201e9451c6ab284ec99b8d90c5dd892388c59fde30c299c64af8c4")
		signatureData := mustHexSigma(t, "153001f1153001010124020137032414001826048012542826058015203b370624150124115d18240701240801300941049bfa105c3d209ff226c31da689dafb297b73499ec1844bba89c60ce65938b722300dd0abbb201e9451c6ab284ec99b8d90c5dd892388c59fde30c299c64af8c4370a3501280118240201360304020401183004142c3494bc756f4b58a73bde64a3141285a3efd5c9300514e766069362d7e35b79687161644d222bdde93a6818300b406772b5445ef466a669d9e5e5663238b817511e73ce992937ddd975690abda8b86b0a79f2fd49bae78c653fad9bd3d53463d4abd7f996964988a7644c4cc1d0321830020030034104473bd04e2a9c4e6a12b9008739c64a16d0113295822faf17e3d2ffcb77b0cae437701b0f0525ddcc6139da5a56dfda2af2b86a2836ef6b03f8f5c231dbaf950b3004410441838848a7e58ab46a1a71a539c474780002bf22adccbeaa43ee07f5176c61aaa3d718102333fc856595ea3a6a5bfd37d2890049acb82c49440e1f490cd970e018")
		signature := mustHexSigma(t, "75e35c22a5da60805d65772b3d4decc8c6eabe30bd2925608524ea12b729efd00a12faeb5757cdfc65aaefddd01c57be9f14d37e2c0beca43434f8ebdd81d635")
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), fabricPubKey)
		if err != nil {
			t.Fatal(err)
		}
		r, sv, err := unpackECDSASig(signature)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(signatureData)
		if !ecdsa.Verify(pub, digest[:], r, sv) {
			t.Fatal("the signature does not verify over the signed data")
		}
	})
}
