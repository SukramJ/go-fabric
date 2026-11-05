// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestAttributeValueReader_ContainerWithoutDecoder writes a container to
// every attribute of every registered cluster definition — the read-only
// lists among them have no generated decoder — and expects the reader to
// return, never panic: TC-ACE-2.2 writes every attribute of every
// cluster, and OperationalCredentials.TrustedRootCertificates (0x003E:4,
// a read-only list[octstr]) dereferenced a nil decoder in CI run
// 41963301696, which left the WriteRequest unanswered (WriteClient.cpp
// Timeout) instead of UNSUPPORTED_WRITE.
func TestAttributeValueReader_ContainerWithoutDecoder(t *testing.T) {
	t.Parallel()
	containers := map[string]func(*tlv.Encoder){
		"array":  func(enc *tlv.Encoder) { enc.StartArray(tlv.AnonymousTag()); _ = enc.EndContainer() },
		"struct": func(enc *tlv.Encoder) { enc.StartStruct(tlv.AnonymousTag()); _ = enc.EndContainer() },
	}
	read := func(t *testing.T, cluster, attr uint32, body func(*tlv.Encoder)) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("cluster 0x%04X attribute 0x%04X: panic %v", cluster, attr, r)
			}
		}()
		enc := tlv.NewEncoder()
		body(enc)
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		dec := tlv.NewDecoder(b)
		path := im.ConcreteAttributePath{HasCluster: true, HasAttribute: true, Cluster: cluster, Attribute: attr}
		_, _ = attributeValueReader(path, advanceToContent(t, dec), dec)
	}
	// The CI case first, by name.
	for _, body := range containers {
		read(t, opcredsdef.ClusterID, opcredsdef.AttrTrustedRootCertificates, body)
	}
	clusters, attrs := 0, 0
	for id := range uint32(0x2000) {
		def := spec.Lookup(id)
		if def == nil {
			continue
		}
		clusters++
		for _, a := range def.Attributes {
			attrs++
			for _, body := range containers {
				read(t, id, a.ID, body)
			}
		}
	}
	if clusters == 0 || attrs == 0 {
		t.Fatalf("no registered cluster definitions reached the test (%d clusters, %d attributes)", clusters, attrs)
	}
	t.Logf("%d clusters, %d attributes, no panic", clusters, attrs)
}
