// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	dldef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	icddef "thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	dsdef "thingfabric.dev/go-fabric/cluster/spec/jointfabricdatastore"
	onoffdef "thingfabric.dev/go-fabric/cluster/spec/onoff"
	"thingfabric.dev/go-fabric/im"
)

// The two value logs of an attribute read — the read's per-path report
// (logReadAttributeReport) and a subscription's priming report
// (primingSource.log) — never carry a value the model marks secret. They
// mirror the attribute read log of
// matter.js packages/protocol/src/action/server/AttributeReadResponse.ts:303-306 at db91886c,
// which passes the value through SecretRedaction.redact; this module redacts
// always (no Logger.redactSecrets switch).

// redactLogSite is one of the two value logs, writing r to a logger.
type redactLogSite struct {
	name string
	log  func(b *Bridge, r im.AttributeReport)
}

var redactLogSites = []redactLogSite{
	{"read_report", func(b *Bridge, r im.AttributeReport) { b.logReadAttributeReport(0, r) }},
	{"initial_report", func(b *Bridge, r im.AttributeReport) { (&primingSource{b: b}).log(r) }},
}

// logged runs site with r against a text and a JSON handler and returns
// both outputs.
func logged(site redactLogSite, r im.AttributeReport) []string {
	outs := make([]string, 0, 2)
	for _, mk := range []func(*bytes.Buffer) slog.Handler{
		func(w *bytes.Buffer) slog.Handler {
			return slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})
		},
		func(w *bytes.Buffer) slog.Handler {
			return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})
		},
	} {
		var buf bytes.Buffer
		site.log(&Bridge{logger: slog.New(mk(&buf))}, r)
		outs = append(outs, buf.String())
	}
	return outs
}

// secretForms are the ways a log handler can print key: hex, base64 (the
// JSON handler's []byte) and fmt's decimal list (the text handler's %+v).
func secretForms(key []byte) []string {
	dec := make([]string, len(key))
	for i, b := range key {
		dec[i] = strconv.Itoa(int(b))
	}
	return []string{
		hex.EncodeToString(key), strings.ToUpper(hex.EncodeToString(key)),
		base64.StdEncoding.EncodeToString(key), strings.Join(dec, " "),
	}
}

func assertNoSecret(t *testing.T, out string, keys ...[]byte) {
	t.Helper()
	for _, k := range keys {
		for _, form := range secretForms(k) {
			if strings.Contains(out, form) {
				t.Errorf("log carries the secret as %q:\n%s", form, out)
			}
		}
	}
}

func assertContains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("log lacks %q:\n%s", w, out)
		}
	}
}

func keyOf(seed byte) []byte {
	k := make([]byte, 16)
	for i := range k {
		k[i] = seed + byte(i)*7
	}
	return k
}

func attrReport(cluster, attr uint32, v im.AttributeValue) im.AttributeReport {
	return im.AttributeReport{
		Path:  im.ConcreteAttributePath{Endpoint: 1, Cluster: cluster, Attribute: attr},
		Value: v,
	}
}

func TestValueLogsRedactSecrets(t *testing.T) {
	t.Parallel()
	t.Run("IcdManagement RegisteredClients Key", func(t *testing.T) {
		t.Parallel()
		key := keyOf(0xA1)
		r := attrReport(icddef.ClusterID, icddef.AttrRegisteredClients, im.AttributeValue{Value: spec.List[icddef.MonitoringRegistrationStruct]{{
			CheckInNodeId: 0x1122334455, MonitoredSubject: 0x66778899AA, Key: &key, FabricIndex: 1,
		}}})
		for _, site := range redactLogSites {
			for _, out := range logged(site, r) {
				assertNoSecret(t, out, key)
				assertContains(t, out, "(redacted)", "73588229205", "440092105130")
			}
		}
	})
	t.Run("JointFabricDatastore GroupKeySetList EpochKey0-2", func(t *testing.T) {
		t.Parallel()
		k0, k1, k2 := keyOf(0x31), keyOf(0x52), keyOf(0x73)
		r := attrReport(dsdef.ClusterID, dsdef.AttrGroupKeySetList, im.AttributeValue{Value: spec.List[dsdef.DatastoreGroupKeySetStruct]{{
			GroupKeySetId:   4242,
			EpochKey0:       spec.Nullable[[]byte]{Value: k0},
			EpochStartTime0: spec.Nullable[uint64]{Value: 987654321},
			EpochKey1:       spec.Nullable[[]byte]{Value: k1},
			EpochStartTime1: spec.Nullable[uint64]{Null: true},
			EpochKey2:       spec.Nullable[[]byte]{Value: k2},
			EpochStartTime2: spec.Nullable[uint64]{Null: true},
		}}})
		for _, site := range redactLogSites {
			for _, out := range logged(site, r) {
				assertNoSecret(t, out, k0, k1, k2)
				assertContains(t, out, "(redacted)", "4242", "987654321")
			}
		}
	})
	t.Run("DoorLock AliroGroupResolvingKey, the attribute itself", func(t *testing.T) {
		t.Parallel()
		key := keyOf(0x15)
		r := attrReport(dldef.ClusterID, dldef.AttrAliroGroupResolvingKey, im.AttributeValue{Value: key})
		for _, site := range redactLogSites {
			for _, out := range logged(site, r) {
				assertNoSecret(t, out, key)
				assertContains(t, out, "(redacted)")
			}
		}
	})
	t.Run("a null secret stays null", func(t *testing.T) {
		t.Parallel()
		r := attrReport(dldef.ClusterID, dldef.AttrAliroGroupResolvingKey, im.AttributeValue{IsNull: true})
		for _, site := range redactLogSites {
			for _, out := range logged(site, r) {
				if strings.Contains(out, "(redacted)") {
					t.Errorf("a null secret logged as redacted:\n%s", out)
				}
			}
		}
	})
	t.Run("fails closed on a value the writer cannot encode", func(t *testing.T) {
		t.Parallel()
		// A host's own Go type the attribute writer does not know encodes
		// as TLV null; on a path that holds a secret the log carries the
		// placeholder, never the raw value.
		type hostClient struct{ Key []byte }
		key := keyOf(0x09)
		r := attrReport(icddef.ClusterID, icddef.AttrRegisteredClients, im.AttributeValue{Value: []hostClient{{Key: key}}})
		for _, site := range redactLogSites {
			for _, out := range logged(site, r) {
				assertNoSecret(t, out, key)
				assertContains(t, out, "(redacted)")
			}
		}
	})
	t.Run("a path without a secret logs its value as it is", func(t *testing.T) {
		t.Parallel()
		for _, r := range []im.AttributeReport{
			attrReport(onoffdef.ClusterID, onoffdef.AttrOnTime, im.AttributeValue{Value: uint16(31337)}),
			attrReport(dldef.ClusterID, dldef.AttrNumberOfTotalUsersSupported, im.AttributeValue{Value: uint16(31337)}),
			// A cluster no definition describes: no model, no redaction
			// (SecretRedaction.redact without a model, :33).
			attrReport(0xFFF1FC00, 0, im.AttributeValue{Value: uint16(31337)}),
		} {
			for _, site := range redactLogSites {
				for _, out := range logged(site, r) {
					assertContains(t, out, "31337")
					if strings.Contains(out, "(redacted)") {
						t.Errorf("a value without a secret redacted:\n%s", out)
					}
				}
			}
		}
	})
}
