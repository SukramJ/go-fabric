// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	acdef "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// valueOf encodes e and reads it back as the tree the role hands out.
func valueOf(t *testing.T, e client.Encodable) client.Value {
	t.Helper()
	enc := tlv.NewEncoder()
	e.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(b)
	el, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !el.IsContainer {
		return spec.Node{El: el}
	}
	n, err := spec.ReadContainer(dec, el)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// sameJSON compares two JSON values, numbers by their text.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	parse := func(raw []byte) any {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		return v
	}
	return reflect.DeepEqual(parse(a), parse(b))
}

// roundTrip encodes raw as a value of f and decodes it in form, which
// must give raw back.
func roundTrip(t *testing.T, what string, f client.Field, raw json.RawMessage, form client.Form) {
	t.Helper()
	e, err := client.EncodeJSON(f, raw)
	if err != nil {
		t.Fatalf("%s: EncodeJSON(%s): %v", what, raw, err)
	}
	got, err := client.DecodeJSON(f, valueOf(t, e), form)
	if err != nil {
		t.Fatalf("%s: DecodeJSON: %v", what, err)
	}
	if !sameJSON(t, raw, got) {
		t.Fatalf("%s: %s round-trips as %s", what, raw, got)
	}
}

func attributeOf(t *testing.T, path string) client.Field {
	t.Helper()
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		t.Fatalf("path %q", path)
	}
	c, err1 := strconv.ParseUint(parts[1], 10, 32)
	a, err2 := strconv.ParseUint(parts[2], 10, 32)
	if err1 != nil || err2 != nil {
		t.Fatalf("path %q", path)
	}
	f := client.AttributeField(uint32(c), uint32(a))
	if !f.Typed() {
		t.Fatalf("attribute %s is untyped", path)
	}
	return f
}

func commandByName(t *testing.T, cluster uint32, name string) client.Field {
	t.Helper()
	def := spec.Lookup(cluster)
	if def == nil {
		t.Fatalf("cluster %d has no definition", cluster)
	}
	for i := range def.Commands {
		if c := &def.Commands[i]; c.Direction == spec.Request && strings.EqualFold(c.Name, name) {
			return client.CommandField(cluster, c.ID)
		}
	}
	t.Fatalf("cluster %d has no command %q", cluster, name)
	return client.Field{}
}

func readFixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "compat", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// TestJSONCompat round-trips every Matter value of matterjs-server's
// documented examples: attribute values tag-based, command fields and
// event data name-based.
func TestJSONCompat(t *testing.T) {
	t.Parallel()
	type event struct {
		ClusterID uint32          `json:"cluster_id"`
		EventID   uint32          `json:"event_id"`
		Data      json.RawMessage `json:"data"`
	}
	type report struct {
		Attributes map[string]json.RawMessage `json:"attributes"`
		Events     []event                    `json:"events"`
	}
	for _, name := range []string{"read.response.json", "subscribe.response.json"} {
		var doc struct {
			Result report `json:"result"`
		}
		readFixture(t, name, &doc)
		for path, raw := range doc.Result.Attributes {
			roundTrip(t, name+" "+path, attributeOf(t, path), raw, client.TagBased)
		}
		for _, e := range doc.Result.Events {
			roundTrip(t, name+" event", client.EventField(e.ClusterID, e.EventID), e.Data, client.NameBased)
		}
	}
	var sub struct {
		Data report `json:"data"`
	}
	readFixture(t, "subscription_report.event.json", &sub)
	for path, raw := range sub.Data.Attributes {
		roundTrip(t, "subscription_report "+path, attributeOf(t, path), raw, client.TagBased)
	}
	var ev struct {
		Data event `json:"data"`
	}
	readFixture(t, "node_event.event.json", &ev)
	roundTrip(t, "node_event", client.EventField(ev.Data.ClusterID, ev.Data.EventID), ev.Data.Data, client.NameBased)
	var upd struct {
		Data []json.RawMessage `json:"data"`
	}
	readFixture(t, "attribute_updated.event.json", &upd)
	var path string
	if err := json.Unmarshal(upd.Data[1], &path); err != nil {
		t.Fatal(err)
	}
	roundTrip(t, "attribute_updated", attributeOf(t, path), upd.Data[2], client.TagBased)
	var wr struct {
		Args struct {
			Path  string          `json:"attribute_path"`
			Value json.RawMessage `json:"value"`
		} `json:"args"`
	}
	readFixture(t, "write_attribute.request.json", &wr)
	roundTrip(t, "write_attribute", attributeOf(t, wr.Args.Path), wr.Args.Value, client.TagBased)
	type command struct {
		ClusterID uint32          `json:"cluster_id"`
		Name      string          `json:"command_name"`
		Payload   json.RawMessage `json:"payload"`
	}
	for _, name := range []string{"device_command.request.json", "device_command.request.2.json"} {
		var dc struct {
			Args command `json:"args"`
		}
		readFixture(t, name, &dc)
		f := commandByName(t, dc.Args.ClusterID, dc.Args.Name)
		e, err := client.EncodeJSON(f, dc.Args.Payload)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := client.DecodeJSON(f, valueOf(t, e), client.NameBased)
		if err != nil {
			t.Fatal(err)
		}
		// The payload's members come back; a mandatory member the payload
		// left out comes back with its default.
		var want, have map[string]any
		_ = json.Unmarshal(dc.Args.Payload, &want)
		_ = json.Unmarshal(got, &have)
		for k, v := range want {
			if !reflect.DeepEqual(have[k], v) {
				t.Fatalf("%s: %s is %v, want %v (%s)", name, k, have[k], v, got)
			}
		}
	}
	var batch struct {
		Args struct {
			Commands []struct {
				command
				EndpointID uint16 `json:"endpoint_id"`
			} `json:"commands"`
		} `json:"args"`
	}
	readFixture(t, "batched_device_commands.request.json", &batch)
	for _, c := range batch.Args.Commands {
		if _, err := client.EncodeJSON(commandByName(t, c.ClusterID, c.Name), c.Payload); err != nil {
			t.Fatalf("batched %s: %v", c.Name, err)
		}
	}
}

// TestJSONOnASite: a JSON invoke turns a site device's light on, a JSON
// write sets its OnTime, and the values read back render as the JSON sent.
func TestJSONOnASite(t *testing.T) {
	t.Parallel()
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := pair.Controller.CommissionNode(ctx, pair.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ep, _ := light.EndpointNumber()
	on, err := client.EncodeJSON(client.CommandField(onoffdef.ClusterID, onoffdef.CmdOn), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	onTime, err := client.EncodeJSON(client.AttributeField(onoffdef.ClusterID, onoffdef.AttrOnTime), json.RawMessage(`30`))
	if err != nil {
		t.Fatal(err)
	}
	var rep *client.Report
	if err := site.Run(ctx, func(ctx context.Context) error {
		if _, err := n.Invoke(ctx, client.CommandAt(ep, onoffdef.ClusterID, onoffdef.CmdOn), on, client.InvokeOptions{}); err != nil {
			return err
		}
		st, err := n.Write(ctx, []client.WriteRequest{{Path: client.AttributeAt(ep, onoffdef.ClusterID, onoffdef.AttrOnTime), Value: onTime}}, client.WriteOptions{})
		if err != nil {
			return err
		}
		if err := st.Err(); err != nil {
			return err
		}
		var req client.ReadRequest
		req.Attributes = []client.AttributePath{client.ClusterAt(ep, onoffdef.ClusterID)}
		rep, err = n.Read(ctx, req)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for attr, want := range map[uint32]string{onoffdef.AttrOnOff: `true`, onoffdef.AttrOnTime: `30`} {
		v, ok := rep.Value(ep, onoffdef.ClusterID, attr)
		if !ok {
			t.Fatalf("attribute %d not read", attr)
		}
		got, err := client.DecodeJSON(client.AttributeField(onoffdef.ClusterID, attr), v, client.TagBased)
		if err != nil || string(got) != want {
			t.Fatalf("attribute %d: %s (%v), want %s", attr, got, err, want)
		}
	}
}

// TestJSONAccessControlOnASite: the ACL of a commissioned site device
// reads through DecodeJSON and writes through EncodeJSON — typed by the
// definition-only AccessControl definition (ADR 0013, amendment of
// 2026-10-25), served by the hand-written cluster/core server.
func TestJSONAccessControlOnASite(t *testing.T) {
	t.Parallel()
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := pair.Controller.CommissionNode(ctx, pair.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	acl := client.AttributeField(acdef.ClusterID, acdef.AttrAcl)
	if !acl.Typed() {
		t.Fatal("AccessControl.ACL is untyped")
	}
	path := client.AttributeAt(0, acdef.ClusterID, acdef.AttrAcl)
	readACL := func(ctx context.Context) ([]map[string]any, error) {
		var req client.ReadRequest
		req.Attributes = []client.AttributePath{path}
		rep, err := n.Read(ctx, req)
		if err != nil {
			return nil, err
		}
		v, ok := rep.Value(0, acdef.ClusterID, acdef.AttrAcl)
		if !ok {
			return nil, errors.New("ACL not read")
		}
		raw, err := client.DecodeJSON(acl, v, client.NameBased)
		if err != nil {
			return nil, err
		}
		var entries []map[string]any
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("%s: %w", raw, err)
		}
		return entries, nil
	}
	var before, after []map[string]any
	if err := site.Run(ctx, func(ctx context.Context) error {
		if before, err = readACL(ctx); err != nil {
			return err
		}
		// The commissioner's Administer entry, plus an Operate entry for a
		// second node on endpoint 1's OnOff cluster.
		added := map[string]any{"privilege": 3, "authMode": 2, "subjects": []any{0x1234}, "targets": []any{map[string]any{"endpoint": 1, "cluster": 6, "deviceType": nil}}}
		raw, err := json.Marshal(append(slices.Clone(before), added))
		if err != nil {
			return err
		}
		val, err := client.EncodeJSON(acl, raw)
		if err != nil {
			return err
		}
		st, err := n.Write(ctx, []client.WriteRequest{{Path: path, Value: val}}, client.WriteOptions{})
		if err != nil {
			return err
		}
		if err := st.Err(); err != nil {
			return err
		}
		after, err = readACL(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0]["privilege"] != float64(5) || before[0]["authMode"] != float64(2) {
		t.Fatalf("commissioned ACL %v, want the one Administer CASE entry", before)
	}
	if len(after) != 2 || after[1]["privilege"] != float64(3) || after[1]["fabricIndex"] != before[0]["fabricIndex"] {
		t.Fatalf("ACL after the write %v", after)
	}
	if subjects, _ := after[1]["subjects"].([]any); len(subjects) != 1 || subjects[0] != float64(0x1234) {
		t.Fatalf("written subjects %v", after[1]["subjects"])
	}
}

// TestJSONForms: bytes are base64, a 64-bit integer survives in full, an
// optional non-nullable member given null is left out, an unknown member
// is refused, an untyped struct is keyed by tag, and a value of the wrong
// type is ErrJSONValue.
func TestJSONForms(t *testing.T) {
	t.Parallel()
	// BasicInformation UniqueID is a string; OnOff's GlobalSceneControl a bool.
	roundTrip(t, "bool", client.AttributeField(onoffdef.ClusterID, onoffdef.AttrGlobalSceneControl), json.RawMessage(`true`), client.TagBased)
	// GeneralDiagnostics UpTime is a uint64 (0x0033/0x0002).
	roundTrip(t, "uint64", client.AttributeField(0x0033, 0x0002), json.RawMessage(`18446744073709551615`), client.TagBased)
	// OperationalCredentials' TrustedRootCertificates is a list of bytes (0x003E/0x0004).
	roundTrip(t, "bytes", client.AttributeField(0x003E, 0x0004), json.RawMessage(`["AAEC/w=="]`), client.TagBased)
	roundTrip(t, "FeatureMap", client.AttributeField(0xFFF1, spec.AttrFeatureMap), json.RawMessage(`4`), client.TagBased)
	untyped := client.Field{}
	roundTrip(t, "untyped", untyped, json.RawMessage(`{"0":1,"1":"x"}`), client.TagBased)
	// OnOff's OnWithTimedOff: OnOffControl, OnTime, OffWaitTime.
	f := client.CommandField(onoffdef.ClusterID, onoffdef.CmdOnWithTimedOff)
	if _, err := client.EncodeJSON(f, json.RawMessage(`{"onOffControl":0,"onTime":10,"offWaitTime":0,"bogus":1}`)); !errors.Is(err, client.ErrJSONValue) {
		t.Fatalf("an unknown member: %v, want ErrJSONValue", err)
	}
	if _, err := client.EncodeJSON(client.AttributeField(onoffdef.ClusterID, onoffdef.AttrGlobalSceneControl), json.RawMessage(`"yes"`)); !errors.Is(err, client.ErrJSONValue) {
		t.Fatalf("a string for a bool: %v, want ErrJSONValue", err)
	}
	if e, err := client.EncodeJSON(client.CommandField(onoffdef.ClusterID, onoffdef.CmdToggle), nil); err != nil || e == nil {
		t.Fatalf("an empty payload: %v", err)
	}
	// Groups' AddGroupResponse (0x0004/0x00): Status, GroupID, name-based.
	resp := client.ResponseField(0x0004, 0x00)
	if !resp.Typed() {
		t.Fatal("AddGroupResponse is untyped")
	}
	roundTrip(t, "AddGroupResponse", resp, json.RawMessage(`{"status":0,"groupId":7}`), client.NameBased)
	if client.ResponseField(0x0004, 0x7F).Typed() {
		t.Fatal("an unknown response command is typed")
	}
}
