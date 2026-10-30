// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package leveltable

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const levelA = `{"matter":{"revision":"1.4","tag":"v1","sourceCommit":"0123456789abc","method":"model"},
"deviceTypes":[{"id":256,"revision":3}],
"clusters":[{"id":6,"revision":5,"features":[{"bit":0,"code":"LT"}],"attributes":[0,65533],"commands":[0],"events":[],
"mandatory":{"attributes":[0],"commands":[0],"events":[]}}]}`

const levelB = `{"matter":{"revision":"1.4.2","tag":"v2","sourceCommit":"fedcba","method":"model"},
"deviceTypes":[{"id":256,"revision":3},{"id":560,"revision":1}],
"clusters":[{"id":6,"revision":6,"features":[{"bit":0,"code":"LT"},{"bit":2,"code":"OFFONLY"}],"attributes":[0,1,65533],"commands":[0,1],"events":[0],
"mandatory":{"attributes":[0,1],"commands":[0,1],"events":[0]}},
{"id":260,"revision":1,"features":[],"attributes":[0],"commands":[],"events":[],"mandatory":{"attributes":[],"commands":[],"events":[]}}]}`

func TestLoadOrdersAndChecks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "1.4.2.json", levelB)
	write(t, dir, "1.4.json", levelA)
	files, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Matter.Revision != "1.4" || files[1].Matter.Revision != "1.4.2" {
		t.Fatalf("order %s, %s", files[0].Name, files[1].Name)
	}
	h := Hash(files)
	if len(h) != 64 || h == Hash(files[:1]) {
		t.Errorf("hash %s", h)
	}
	src, err := GoSource(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"1.4",   // matter.js v1 (012345678), method model`, "0x0230: 0x02", "features:   map[uint8]LevelSet{0: 0x03, 2: 0x02}", "revisions:  [MaxLevels]uint16{5, 6}"} {
		if !bytes.Contains(src, []byte(want)) {
			t.Errorf("generated source lacks %q:\n%s", want, src)
		}
	}
	pin := Pin{
		Revision: "1.4.2", DeviceTypes: []uint32{0x100, 0x230}, Clusters: []PinCluster{{ID: 6, Features: []uint8{0, 2}, Attributes: []uint32{0, 1}, Commands: []uint32{1}, Events: []uint32{0}}},
		DeviceTypeNames: map[uint32]string{0x100: "OnOffLight", 0x230: "Closure"}, ClusterNames: map[uint32]string{6: "OnOff", 0x104: "ClosureControl", 0x39: "BDBI"},
	}
	if err := CheckPin(files, pin); err != nil {
		t.Fatal(err)
	}
	bad := pin
	bad.DeviceTypes = append(bad.DeviceTypes, 0x999)
	bad.Clusters = append(bad.Clusters, PinCluster{ID: 0x777}, PinCluster{ID: 6, Features: []uint8{9}, Attributes: []uint32{0x55}, Commands: []uint32{0x56}, Events: []uint32{0x57}})
	err = CheckPin(files, bad)
	for _, want := range []string{"device type 0x0999", "cluster 0x0777", "feature bit 9 of cluster 0x0006", "attribute 0x0055", "command 0x0056", "event 0x0057"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CheckPin: %v lacks %q", err, want)
		}
	}
	if err := CheckPin(files, Pin{Revision: "1.6.1"}); err == nil {
		t.Error("a last level that is not the pin's revision")
	}
	doc := Doc(files, pin, map[string][]Internal{"1.4": {{Endpoint: "", Cluster: 6, Kind: "attribute", ID: 1}, {Endpoint: "a", Cluster: 6, Kind: "attribute", ID: 1}, {Endpoint: "b", Cluster: 6, Kind: "cluster", ID: 6}}})
	for _, want := range []string{
		"| `1.4` | `v1` | `012345678` | model | 1 | 1 | 1 | 2 | 1 | 0 |",
		"Device types it does not know (1): Closure `0x0230`.",
		"Clusters it does not know (1): ClosureControl `0x0104`.",
		"| OnOff `0x0006` | attribute `0x0001`; command `0x0001`; event `0x0000` |",
		"| OnOff `0x0006` | attribute `0x00000001` | (root), `a` |",
		"| OnOff `0x0006` | cluster `0x0006` | `b` |",
		"BDBI `0x0039`",
	} {
		if !bytes.Contains(doc, []byte(want)) {
			t.Errorf("doc lacks %q:\n%s", want, doc)
		}
	}
	page := []byte("head\n" + DocBegin + "\nold\n" + DocEnd + "\ntail\n")
	out, err := Splice(page, doc)
	if err != nil || !bytes.HasPrefix(out, []byte("head\n"+DocBegin)) || !bytes.HasSuffix(out, []byte(DocEnd+"\ntail\n")) {
		t.Fatalf("splice: %v\n%s", err, out)
	}
	if b, ok := Block(out); !ok || !bytes.Equal(b, doc) {
		t.Error("Block does not return the spliced block")
	}
	if _, err := Splice([]byte("no markers"), doc); err == nil {
		t.Error("splice without markers")
	}
	if _, ok := Block([]byte("no markers")); ok {
		t.Error("block without markers")
	}
}

func TestLoadRejects(t *testing.T) {
	for name, c := range map[string]struct{ file, body string }{
		"misnamed":   {"1.5.json", levelA},
		"bad json":   {"1.4.json", "{"},
		"bad level":  {"x.json", strings.Replace(levelA, `"1.4"`, `"x"`, 1)},
		"empty":      {"", ""},
		"unreadable": {"1.4.json/", ""},
	} {
		dir := t.TempDir()
		switch {
		case c.file == "":
		case strings.HasSuffix(c.file, "/"):
			if err := os.Mkdir(filepath.Join(dir, strings.TrimSuffix(c.file, "/")), 0o700); err != nil {
				t.Fatal(err)
			}
		default:
			write(t, dir, c.file, c.body)
		}
		if _, err := Load(dir); err == nil {
			t.Errorf("%s: Load accepted it", name)
		}
	}
	dir := t.TempDir()
	for i := range MaxLevels + 1 {
		l := "1." + string(rune('0'+i))
		write(t, dir, l+".json", strings.Replace(levelA, `"1.4"`, `"`+l+`"`, 1))
	}
	if _, err := Load(dir); err == nil {
		t.Error("more levels than a LevelSet holds")
	}
	if _, err := LoadPin(filepath.Join(dir, "none.json")); err == nil {
		t.Error("LoadPin of a missing file")
	}
	write(t, dir, "bad.json", "{")
	if _, err := LoadPin(filepath.Join(dir, "bad.json")); err == nil {
		t.Error("LoadPin of bad JSON")
	}
}

func TestLevelOrder(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.4", "1.4.1", -1}, {"1.4.2", "1.5.1", -1}, {"1.6.1", "1.6.0", 1}, {"1.4", "1.4", 0}, {"a", "b", -1}} {
		if got := CompareLevels(c.a, c.b); got != c.want {
			t.Errorf("CompareLevels(%s, %s) = %d", c.a, c.b, got)
		}
	}
	if _, err := ParseLevel("1..4"); err == nil {
		t.Error("an empty part")
	}
	if short("abc") != "abc" {
		t.Error("short")
	}
}

func TestLoadPinOfTheSnapshot(t *testing.T) {
	pin, err := LoadPin(filepath.Join("..", "..", "..", "parity", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if pin.Revision == "" || len(pin.Clusters) == 0 || pin.ClusterNames[6] != "OnOff" {
		t.Fatalf("pin %s, %d clusters", pin.Revision, len(pin.Clusters))
	}
}
