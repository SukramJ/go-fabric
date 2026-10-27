// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestEncodingKnownAnswers pins the encoders against bytes derived by hand
// from the proto3 rules and api.proto's field numbers.
func TestEncodingKnownAnswers(t *testing.T) {
	cases := []struct {
		name string
		m    Message
		want string
	}{
		// client_info "go-fabric" (1), major 1 (2), minor 19 (3).
		{"HelloRequest", &HelloRequest{ClientInfo: "go-fabric", APIVersionMajor: 1, APIVersionMinor: 19}, "0a09676f2d666162726963" + "1001" + "1813"},
		// flags 1 (1).
		{"Subscribe", &SubscribeBluetoothLEAdvertisementsRequest{Flags: SubscriptionFlagRawAdvertisements}, "0801"},
		// address (1) as a varint, CONNECT_V3_WITHOUT_CACHE (2), has_address_type (3), address_type 1 (4).
		{"DeviceRequest", &BluetoothDeviceRequest{Address: 0xC0FFEE000B1E, RequestType: DeviceRequestConnectV3WithoutCache, HasAddressType: true, AddressType: 1}, "089e9680f0fe9f30" + "1005" + "1801" + "2001"},
		// address 1 (1), handle 14 (2), response (3), data (4).
		{"WriteRequest", &AddressHandle{MessageType: TypeBluetoothGATTWriteRequest, Address: 1, Handle: 0x0e, Flag: true, Data: []byte{1, 2}}, "0801" + "100e" + "1801" + "22020102"},
		// the CCCD: address, handle, data 02 00 (3).
		{"WriteDescriptor", &AddressHandle{MessageType: TypeBluetoothGATTWriteDescriptorRequest, Address: 1, Handle: 0x11, Data: []byte{2, 0}}, "0801" + "1011" + "1a020200"},
		// enable (3).
		{"NotifyRequest", &AddressHandle{MessageType: TypeBluetoothGATTNotifyRequest, Address: 1, Handle: 0x10, Flag: true}, "0801" + "1010" + "1801"},
		// int32 -1 is ten bytes.
		{"ConnectionResponse", &BluetoothDeviceConnectionResponse{Address: 1, Error: -1}, "0801" + "20ffffffffffffffffff01"},
		// rssi -60 as sint32 is 119; the address and the data are forced.
		{"RawAdvertisement", &BluetoothLERawAdvertisementsResponse{Advertisements: []RawAdvertisement{{RSSI: -60, Data: []byte{0xaa}}}}, "0a07" + "0800" + "1077" + "2201aa"},
		{"Empty", Empty(TypePingRequest), ""},
	}
	for _, c := range cases {
		if got := hex.EncodeToString(c.m.Marshal()); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// The plaintext frame: 0x00, length 15, type 1.
	if got := hex.EncodeToString(AppendPlaintext(nil, cases[0].m)); got != "000f01"+cases[0].want {
		t.Errorf("plaintext frame %s", got)
	}
}

// TestDecodeFirmwareShapes decodes the shapes the firmware writes: the uuid
// words one by one, a short UUID, the allocated addresses, and a packed
// repeated uint64 as protobuf may write it.
func TestDecodeFirmwareShapes(t *testing.T) {
	c1 := UUID{Words: [2]uint64{0x18EE2EF5263D4559, 0x959F4F9C429F9D11}}
	svc := GATTService{UUID: UUID{Short: 0xfff6}, Handle: 0x0c, Characteristics: []GATTCharacteristic{
		{UUID: c1, Handle: 0x0e, Properties: 0x08},
		{UUID: UUID{Words: [2]uint64{1, 2}}, Handle: 0x10, Properties: 0x20, Descriptors: []GATTDescriptor{{UUID: UUID{Short: 0x2902}, Handle: 0x11}}},
	}}
	in := &BluetoothGATTGetServicesResponse{Address: 7, Services: []GATTService{svc}}
	var out BluetoothGATTGetServicesResponse
	if err := out.Unmarshal(in.Marshal()); err != nil {
		t.Fatal(err)
	}
	got := out.Services[0]
	if out.Address != 7 || got.UUID.Short != 0xfff6 || got.UUID.Is128() {
		t.Fatalf("service %+v", got)
	}
	if got.Characteristics[0].UUID != c1 || got.Characteristics[1].Descriptors[0].UUID.Short != 0x2902 || got.Characteristics[1].Descriptors[0].Handle != 0x11 {
		t.Fatalf("characteristics %+v", got.Characteristics)
	}
	// A packed uuid (field 1, wire type 2).
	var d GATTDescriptor
	packed := append([]byte{0x0a, 0x02, 0x05, 0x06}, 0x10, 0x21)
	if err := d.unmarshal(packed); err != nil || d.UUID.Words != [2]uint64{5, 6} || d.Handle != 0x21 {
		t.Fatalf("packed uuid %+v %v", d, err)
	}
	free := &BluetoothConnectionsFreeResponse{Free: 2, Limit: 3, Allocated: []uint64{0xC0FFEE000B1E}}
	var f BluetoothConnectionsFreeResponse
	if err := f.Unmarshal(free.Marshal()); err != nil || f.Free != 2 || f.Limit != 3 || len(f.Allocated) != 1 || f.Allocated[0] != 0xC0FFEE000B1E {
		t.Fatalf("free %+v %v", f, err)
	}
	var e AddressHandle
	if err := e.Unmarshal(TypeBluetoothGATTErrorResponse, (&AddressHandle{MessageType: TypeBluetoothGATTErrorResponse, Address: 1, Handle: 2, Error: -3}).Marshal()); err != nil || e.Error != -3 {
		t.Fatalf("error response %+v %v", e, err)
	}
	if err := fields([]byte{0x08}, func(field) error { return nil }); !errors.Is(err, ErrMalformed) {
		t.Fatalf("truncated varint: %v", err)
	}
	if err := fields([]byte{0x0a, 0x05, 0x00}, func(field) error { return nil }); !errors.Is(err, ErrMalformed) {
		t.Fatalf("truncated bytes: %v", err)
	}
}

func TestRoundTrips(t *testing.T) {
	hello := &HelloResponse{APIVersionMajor: 1, APIVersionMinor: 14, ServerInfo: "esphome", Name: "proxy"}
	var h HelloResponse
	if err := h.Unmarshal(hello.Marshal()); err != nil || h != *hello {
		t.Fatalf("hello %+v %v", h, err)
	}
	info := &DeviceInfoResponse{Name: "proxy", MACAddress: "AA", BluetoothProxyFeatureFlags: 0x7f, BluetoothMACAddress: "BB"}
	var i DeviceInfoResponse
	if err := i.Unmarshal(info.Marshal()); err != nil || i != *info {
		t.Fatalf("info %+v %v", i, err)
	}
	caps := &DeviceCapabilitiesResponse{BluetoothProxyFeatureFlags: 0x27, BluetoothMACAddress: "CC"}
	var c DeviceCapabilitiesResponse
	if err := c.Unmarshal(caps.Marshal()); err != nil || c != *caps {
		t.Fatalf("capabilities %+v %v", c, err)
	}
	legacy := &BluetoothLEAdvertisementResponse{Address: 9, Name: []byte("n"), RSSI: -70, AddressType: 1, ServiceData: []ServiceData{{UUID: "0000fff6-0000-1000-8000-00805f9b34fb", Data: []byte{0, 1}}}}
	var l BluetoothLEAdvertisementResponse
	if err := l.Unmarshal(legacy.Marshal()); err != nil || l.RSSI != -70 || l.AddressType != 1 || l.ServiceData[0].UUID != legacy.ServiceData[0].UUID || !bytes.Equal(l.ServiceData[0].Data, []byte{0, 1}) {
		t.Fatalf("legacy %+v %v", l, err)
	}
	raw := &BluetoothLERawAdvertisementsResponse{Advertisements: []RawAdvertisement{{Address: 1, RSSI: 5, AddressType: 1, Data: []byte{1}}, {Address: 2, RSSI: -100, Data: []byte{2}}}}
	var r BluetoothLERawAdvertisementsResponse
	if err := r.Unmarshal(raw.Marshal()); err != nil || len(r.Advertisements) != 2 || r.Advertisements[1].RSSI != -100 || r.Advertisements[0].AddressType != 1 {
		t.Fatalf("raw %+v %v", r, err)
	}
	var dr BluetoothDeviceRequest
	req := &BluetoothDeviceRequest{Address: 3, RequestType: DeviceRequestDisconnect}
	if err := dr.Unmarshal(req.Marshal()); err != nil || dr != *req {
		t.Fatalf("device request %+v %v", dr, err)
	}
	var cr BluetoothDeviceConnectionResponse
	resp := &BluetoothDeviceConnectionResponse{Address: 3, Connected: true, MTU: 247, Error: 0}
	if err := cr.Unmarshal(resp.Marshal()); err != nil || cr != *resp {
		t.Fatalf("connection response %+v %v", cr, err)
	}
	var hr HelloRequest
	if err := hr.Unmarshal((&HelloRequest{ClientInfo: "x", APIVersionMajor: 1, APIVersionMinor: 19}).Marshal()); err != nil || hr.APIVersionMinor != 19 {
		t.Fatalf("hello request %+v %v", hr, err)
	}
	var sub SubscribeBluetoothLEAdvertisementsRequest
	if err := sub.Unmarshal([]byte{0x08, 0x01}); err != nil || sub.Flags != 1 {
		t.Fatal(sub, err)
	}
	if (&DisconnectRequest{}).Type() != TypeDisconnectRequest || len((&DisconnectRequest{Reason: 1}).Marshal()) != 2 {
		t.Fatal("disconnect request")
	}
	var w AddressHandle
	if err := w.Unmarshal(TypeBluetoothGATTWriteRequest, (&AddressHandle{MessageType: TypeBluetoothGATTWriteRequest, Address: 1, Handle: 2, Flag: true, Data: []byte{9}}).Marshal()); err != nil || !w.Flag || !bytes.Equal(w.Data, []byte{9}) {
		t.Fatalf("write %+v %v", w, err)
	}
}

func TestPlaintextFraming(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlaintext(&buf)
	big := &AddressHandle{MessageType: TypeBluetoothGATTNotifyDataResponse, Address: 1, Handle: 2, Data: make([]byte, 300)}
	for _, m := range []Message{Empty(TypePingRequest), big} {
		if err := p.WriteMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	if typ, payload, err := p.ReadMessage(); err != nil || typ != TypePingRequest || len(payload) != 0 {
		t.Fatalf("ping %d %v %v", typ, payload, err)
	}
	typ, payload, err := p.ReadMessage()
	if err != nil || typ != TypeBluetoothGATTNotifyDataResponse || !bytes.Equal(payload, big.Marshal()) {
		t.Fatalf("notify data %d %v", typ, err)
	}
	for in, want := range map[string]error{
		"01":           ErrRequiresEncryption,
		"02":           ErrProtocol,
		"00ffffffff7f": ErrProtocol, // a five-byte varuint
		"00ffff7f01":   ErrProtocol, // a length past 65535
		"0000ffffff7f": ErrProtocol, // a type past 16 bits
		"000501aabb":   io.ErrUnexpectedEOF,
		"":             io.EOF,
		"0080":         io.EOF,
	} {
		_, _, err := NewPlaintext(bytes.NewBuffer(unhex(t, in))).ReadMessage()
		if !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", in, err, want)
		}
	}
}

// recorder records what is written through it.
type recorder struct {
	net.Conn
	mu  sync.Mutex
	out [][]byte
}

func (r *recorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	r.out = append(r.out, append([]byte(nil), b...))
	r.mu.Unlock()
	return r.Conn.Write(b)
}

func (r *recorder) writes() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.out...)
}

// hkdf is the Noise HKDF (Noise spec §4.3) over HMAC-SHA256.
func hkdf(ck, ikm []byte, n int) [][]byte {
	mac := func(k, d []byte) []byte { h := hmac.New(sha256.New, k); h.Write(d); return h.Sum(nil) }
	tk := mac(ck, ikm)
	var out [][]byte
	prev := []byte{}
	for i := 1; i <= n; i++ {
		prev = mac(tk, append(append([]byte(nil), prev...), byte(i)))
		out = append(out, prev)
	}
	return out
}

// aead is ChaChaPoly with the Noise nonce: four zero bytes and the
// counter, little-endian (noise_encryption.py:18).
func aead(t *testing.T, k []byte, n uint64, ad, pt []byte) []byte {
	t.Helper()
	c, err := chacha20poly1305.New(k)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [12]byte
	binary.LittleEndian.PutUint64(nonce[4:], n)
	return c.Seal(nil, nonce[:], pt, ad)
}

// TestNoiseKnownAnswer derives Noise_NNpsk0_25519_ChaChaPoly_SHA256 by hand
// from the Noise specification — the hash chain, the PSK and the two
// ephemeral keys, the split — for fixed keys, and holds both handshake
// messages and the first two transport frames the client sends to it.
func TestNoiseKnownAnswer(t *testing.T) {
	psk := bytes.Repeat([]byte{0x42}, 32)
	ePriv := bytes.Repeat([]byte{0x01}, 32)
	rPriv := bytes.Repeat([]byte{0x02}, 32)
	x := ecdh.X25519()
	ei, _ := x.NewPrivateKey(ePriv)
	er, _ := x.NewPrivateKey(rPriv)
	shared, _ := er.ECDH(ei.PublicKey())

	mixHash := func(h, d []byte) []byte { s := sha256.Sum256(append(append([]byte(nil), h...), d...)); return s[:] }
	name := sha256.Sum256([]byte("Noise_NNpsk0_25519_ChaChaPoly_SHA256"))
	h, ck := name[:], name[:]
	h = mixHash(h, []byte("NoiseAPIInit\x00\x00"))
	// psk: MixKeyAndHash.
	o := hkdf(ck, psk, 3)
	ck, h = o[0], mixHash(h, o[1])
	// -> e: MixHash, and MixKey in a PSK handshake.
	h = mixHash(h, ei.PublicKey().Bytes())
	o = hkdf(ck, ei.PublicKey().Bytes(), 2)
	ck, k := o[0], o[1]
	c1 := aead(t, k, 0, h, nil)
	h = mixHash(h, c1)
	msg1 := append(append([]byte(nil), ei.PublicKey().Bytes()...), c1...)
	// <- e, ee.
	h = mixHash(h, er.PublicKey().Bytes())
	o = hkdf(ck, er.PublicKey().Bytes(), 2)
	ck = o[0]
	o = hkdf(ck, shared, 2)
	ck, k = o[0], o[1]
	c2 := aead(t, k, 0, h, nil)
	msg2 := append(append([]byte(nil), er.PublicKey().Bytes()...), c2...)
	split := hkdf(ck, nil, 2)

	a, b := net.Pipe()
	ca, cb := &recorder{Conn: a}, &recorder{Conn: b}
	done := make(chan error, 1)
	var server *Noise
	go func() {
		var err error
		server, err = ServerHandshake(cb, psk, ServerHello{Name: "proxy", MAC: "aabbccddeeff"}, bytes.NewReader(rPriv))
		done <- err
	}()
	client, hello, err := ClientHandshake(ca, psk, bytes.NewReader(ePriv))
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if hello.Name != "proxy" || hello.MAC != "aabbccddeeff" {
		t.Fatalf("server hello %+v", hello)
	}
	first := ca.writes()[0]
	wantFirst := append([]byte{0x01, 0x00, 0x00, 0x01, 0x00, 0x31, 0x00}, msg1...)
	if !bytes.Equal(first, wantFirst) {
		t.Fatalf("client hello and msg1\n got %x\nwant %x", first, wantFirst)
	}
	sw := cb.writes()
	if wantHello := append([]byte{0x01, 0x00, 0x14, 0x01}, "proxy\x00aabbccddeeff\x00"...); !bytes.Equal(sw[0], wantHello) {
		t.Fatalf("server hello frame %x", sw[0])
	}
	if want := append([]byte{0x01, 0x00, 0x31, 0x00}, msg2...); !bytes.Equal(sw[1], want) {
		t.Fatalf("msg2\n got %x\nwant %x", sw[1], want)
	}
	// Two transport frames: the inner header, nonces 0 and 1 under k1.
	go func() {
		for range 2 {
			if _, _, err := server.ReadMessage(); err != nil {
				done <- err
				return
			}
		}
		done <- server.WriteMessage(Empty(TypePingResponse))
	}()
	m := &HelloRequest{ClientInfo: "go-fabric", APIVersionMajor: 1, APIVersionMinor: 19}
	for i := range 2 {
		if err := client.WriteMessage(m); err != nil {
			t.Fatal(err)
		}
		inner := append([]byte{0x00, 0x01, 0x00, byte(len(m.Marshal()))}, m.Marshal()...)
		ct := aead(t, split[0], uint64(i), nil, inner) //nolint:gosec // 0 or 1
		want := append([]byte{0x01, 0x00, byte(len(ct))}, ct...)
		if got := ca.writes()[1+i]; !bytes.Equal(got, want) {
			t.Fatalf("transport frame %d\n got %x\nwant %x", i, got, want)
		}
	}
	typ, _, err := client.ReadMessage()
	if err != nil || typ != TypePingResponse {
		t.Fatalf("server frame %d %v", typ, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNoiseRefusals(t *testing.T) {
	psk := bytes.Repeat([]byte{1}, 32)
	wrong := bytes.Repeat([]byte{2}, 32)
	// A wrong key: the device rejects with "Handshake MAC failure".
	a, b := net.Pipe()
	go func() { _, _ = ServerHandshake(b, psk, ServerHello{Name: "p"}, nil); _ = b.Close() }()
	if _, _, err := ClientHandshake(a, wrong, nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("wrong key: %v", err)
	}
	// A plaintext client at an encrypted device reads "requires encryption".
	pa, pb := net.Pipe()
	go func() { _, _ = ServerHandshake(pb, psk, ServerHello{}, nil); _ = pb.Close() }()
	go func() { _ = NewPlaintext(pa).WriteMessage(&HelloRequest{ClientInfo: "x"}) }()
	if _, _, err := NewPlaintext(pa).ReadMessage(); !errors.Is(err, ErrRequiresEncryption) {
		t.Fatalf("plaintext at a Noise device: %v", err)
	}
	// A Noise client at a plaintext device reads "plaintext protocol".
	na, nb := net.Pipe()
	go func() {
		_, _ = io.ReadFull(nb, make([]byte, 3+3+1+48))
		_ = NewPlaintext(nb).WriteMessage(Empty(TypeDisconnectRequest))
		_ = nb.Close()
	}()
	if _, _, err := ClientHandshake(na, psk, nil); !errors.Is(err, ErrPlaintextDevice) {
		t.Fatalf("Noise at a plaintext device: %v", err)
	}
	// Malformed server answers.
	for _, frames := range [][]byte{
		{0x01, 0x00, 0x00},                         // an empty server hello
		{0x01, 0x00, 0x01, 0x02},                   // an unknown protocol
		{0x01, 0x00, 0x01, 0x01, 0x01, 0x00, 0x00}, // an empty handshake frame
		append([]byte{0x01, 0x00, 0x01, 0x01, 0x01, 0x00, 0x04, 0x01}, "bad"...),
		{0x01, 0x00, 0x01, 0x01, 0x01, 0x00, 0x02, 0x00, 0x01}, // a message that does not authenticate
		{0x07, 0x00, 0x00},
	} {
		a, b := net.Pipe()
		go func() {
			_, _ = io.ReadFull(b, make([]byte, 3+3+1+48))
			_, _ = b.Write(frames)
			_ = b.Close()
		}()
		if _, _, err := ClientHandshake(a, psk, nil); err == nil {
			t.Errorf("%x: no error", frames)
		}
	}
	if _, err := DecodeKey("c2hvcnQ="); !errors.Is(err, ErrMalformedKey) {
		t.Fatal(err)
	}
	if k, err := DecodeKey("AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\n"); err != nil || !bytes.Equal(k, psk) {
		t.Fatal(k, err)
	}
}
