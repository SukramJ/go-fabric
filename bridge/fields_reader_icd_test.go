// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/tlv"
)

func decodeICDTLV(t *testing.T, cmd uint32, v spec.Encodable) (any, error) {
	t.Helper()
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	raw, _ := enc.Bytes()
	dec := tlv.NewDecoder(raw)
	opener, _ := dec.Next()
	return commandFieldsReader(cmdPath(icddef.ClusterID, cmd), dec, opener)
}

// TestICDVerificationKeyLengthIsNotAConstraint holds IcdManagementServer.ts:58-75:
// a VerificationKey of another length than 16 decodes, so the server's
// comparison answers it; the Key keeps its bound.
func TestICDVerificationKeyLengthIsNotAConstraint(t *testing.T) {
	t.Parallel()
	short := []byte{1, 2, 3}
	key := bytes.Repeat([]byte{7}, 16)
	got, err := decodeICDTLV(t, icddef.CmdRegisterClient, icddef.RegisterClientRequest{CheckInNodeId: 1, MonitoredSubject: 1, Key: key, VerificationKey: &short})
	if err != nil {
		t.Fatal(err)
	}
	if req := got.(icddef.RegisterClientRequest); req.VerificationKey == nil || !bytes.Equal(*req.VerificationKey, short) || !bytes.Equal(req.Key, key) {
		t.Fatalf("RegisterClient %+v", req)
	}
	got, err = decodeICDTLV(t, icddef.CmdUnregisterClient, icddef.UnregisterClientRequest{CheckInNodeId: 1, VerificationKey: &short})
	if err != nil {
		t.Fatal(err)
	}
	if req := got.(icddef.UnregisterClientRequest); req.VerificationKey == nil || !bytes.Equal(*req.VerificationKey, short) {
		t.Fatalf("UnregisterClient %+v", req)
	}
	// A 16-byte key decodes as the definition decodes it.
	got, err = decodeICDTLV(t, icddef.CmdUnregisterClient, icddef.UnregisterClientRequest{CheckInNodeId: 1, VerificationKey: &key})
	if err != nil || !bytes.Equal(*got.(icddef.UnregisterClientRequest).VerificationKey, key) {
		t.Fatalf("UnregisterClient with a 16-byte key: %v %v", got, err)
	}
	// The Key itself keeps its bound.
	if _, err := decodeICDTLV(t, icddef.CmdRegisterClient, icddef.RegisterClientRequest{CheckInNodeId: 1, MonitoredSubject: 1, Key: short}); err == nil {
		t.Fatal("a 3-byte Key decoded")
	}
	if _, err := decodeICDTLV(t, icddef.CmdRegisterClient, icddef.RegisterClientRequest{CheckInNodeId: 1, MonitoredSubject: 1, Key: short, VerificationKey: &short}); err == nil {
		t.Fatal("a 3-byte Key with an odd VerificationKey decoded")
	}
}
