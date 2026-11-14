// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabricscopecheck

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/tls"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/parity"
)

const modulePath = "github.com/SukramJ/go-fabric"

// server is a registry entry: a server type the module builds for a
// cluster, as a typed nil pointer.
type server struct {
	cluster uint32
	typ     any
}

// servers are the module's server types for the clusters with a
// fabric-scoped attribute.
var servers = []server{
	{0x001E, (*core.Binding)(nil)},
	{0x001F, (*core.AccessControl)(nil)},
	{0x002A, (*core.OTASoftwareUpdateRequestor)(nil)},
	{0x0046, (*core.ICDManagement)(nil)},
	{0x0062, (*core.ScenesManagement)(nil)},
	{0x0097, (*core.Messages)(nil)},
	{0x0553, (*camera.Provider)(nil)},
	{0x0554, (*camera.Requestor)(nil)},
	{0x0555, (*camera.PushTransport)(nil)},
	{0x0801, (*tls.CertificateManagement)(nil)},
	{0x0802, (*tls.ClientManagement)(nil)},
}

// listed are the clusters with a fabric-scoped attribute whose server
// holds no fabric state of its own, with the reason.
var listed = map[uint32]string{
	0x003E: "OperationalCredentials is the fabric table: Fabrics and NOCs are the store's fabric rows, which RemoveFabric deletes before the removal contract runs",
	0x003F: "GroupKeyManagement holds no state of its own: the key sets, the key map and the group table are store rows the fabric cascade deletes, held in memory by groups.Manager, which the bridge's fabric removal forgets (ForgetFabric)",
	0x0065: "Groupcast holds no state of its own: Membership and the Groupcast groups are groups.Manager's, as for GroupKeyManagement",
	0x0750: "EcosystemInformation holds no entries: the host's EcosystemDirectory answers every read, behind contract/",
}

// schemaCluster is the part of a parity/schema.json cluster the check
// reads.
type schemaCluster struct {
	ID         uint32 `json:"id"`
	Name       string `json:"name"`
	Attributes []struct {
		Name      string `json:"name"`
		Effective struct {
			Access struct {
				Fabric string `json:"fabric"`
			} `json:"access"`
		} `json:"effective"`
	} `json:"attributes"`
}

// fabricScopedClusters returns the clusters with an attribute whose
// effective access is fabric-scoped (F) or fabric-sensitive (S), by id,
// with their name — matter.js AttributeModel.fabricScoped.
func fabricScopedClusters(schema []byte) (map[uint32]string, error) {
	var doc struct {
		Clusters []schemaCluster `json:"clusters"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil, fmt.Errorf("fabricscopecheck: schema: %w", err)
	}
	out := map[uint32]string{}
	for _, c := range doc.Clusters {
		for _, a := range c.Attributes {
			if f := a.Effective.Access.Fabric; f == "F" || f == "S" {
				out[c.ID] = c.Name
				break
			}
		}
	}
	return out, nil
}

// built is a package that builds a server over a cluster's definition.
type built struct {
	pkg     string // module-relative package directory
	cluster uint32
	site    string // file:line of the call
}

// clusterIDPattern reads a generated definition's cluster id.
var clusterIDPattern = regexp.MustCompile(`(?m)^\s*ClusterID uint32 = 0x([0-9A-Fa-f]+)`)

// definitionBuilders are the calls that build a server over a definition.
var definitionBuilders = map[string]bool{"spec.New": true, "spec.NewServer": true, "mustInstance": true}

// scanBuilt walks the module at root and returns every package outside
// cluster/spec, script and examples that builds a server over a generated
// cluster definition.
func scanBuilt(root string) ([]built, error) {
	ids := map[string]uint32{} // cluster/spec/<name> -> cluster id
	clusterOf := func(specPkg string) (uint32, bool, error) {
		if id, ok := ids[specPkg]; ok {
			return id, true, nil
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(specPkg), "definition_gen.go"))
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		if err != nil {
			return 0, false, fmt.Errorf("fabricscopecheck: %w", err)
		}
		m := clusterIDPattern.FindSubmatch(data)
		if m == nil {
			return 0, false, nil
		}
		id, err := strconv.ParseUint(string(m[1]), 16, 32)
		if err != nil {
			return 0, false, fmt.Errorf("fabricscopecheck: %s: %w", specPkg, err)
		}
		ids[specPkg] = uint32(id)
		return uint32(id), true, nil
	}
	var out []built
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == ".":
				return nil
			case strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata",
				rel == "cluster/spec", rel == "script", rel == "examples", rel == "notes", rel == "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("fabricscopecheck: %w", err)
		}
		if ast.IsGenerated(f) {
			return nil
		}
		specAlias := map[string]string{} // import name -> cluster/spec/<name>
		for _, imp := range f.Imports {
			ip, _ := strconv.Unquote(imp.Path.Value)
			sub, ok := strings.CutPrefix(ip, modulePath+"/cluster/spec/")
			if !ok || strings.Contains(sub, "/") {
				continue
			}
			name := sub
			if imp.Name != nil {
				name = imp.Name.Name
			}
			specAlias[name] = "cluster/spec/" + sub
		}
		var walkErr error
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 || !definitionBuilders[callName(call.Fun)] {
				return true
			}
			sel, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Definition" {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || specAlias[x.Name] == "" {
				return true
			}
			id, found, err := clusterOf(specAlias[x.Name])
			if err != nil {
				walkErr = err
				return false
			}
			if found {
				pos := fset.Position(call.Pos())
				out = append(out, built{pkg: path.Dir(rel), cluster: id, site: fmt.Sprintf("%s:%d", rel, pos.Line)})
			}
			return true
		})
		return walkErr
	})
	return out, err
}

// callName is "pkg.Func" for a selector call and "Func" for a plain one.
func callName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok {
			return x.Name + "." + f.Sel.Name
		}
	case *ast.Ident:
		return f.Name
	}
	return ""
}

// pkgOf is the module-relative package of a registry entry's type.
func pkgOf(typ any) string {
	t := reflect.TypeOf(typ)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	rel, _ := strings.CutPrefix(t.PkgPath(), modulePath+"/")
	return rel
}

// check returns the sorted violations.
func check(scoped map[uint32]string, entries []server, listing map[uint32]string, builds []built) []string {
	var out []string
	registered := map[uint32]bool{}
	inPkg := map[[2]string]bool{}
	contractType := reflect.TypeFor[contract.FabricScopedState]()
	for _, e := range entries {
		name, ok := scoped[e.cluster]
		if !ok {
			out = append(out, fmt.Sprintf("stale entry: cluster 0x%04X (%T) has no fabric-scoped attribute in parity/schema.json", e.cluster, e.typ))
			continue
		}
		registered[e.cluster] = true
		inPkg[[2]string{pkgOf(e.typ), strconv.FormatUint(uint64(e.cluster), 10)}] = true
		if !reflect.TypeOf(e.typ).Implements(contractType) {
			out = append(out, fmt.Sprintf("%s (0x%04X): %T does not implement contract.FabricScopedState", name, e.cluster, e.typ))
		}
		if _, alsoListed := listing[e.cluster]; alsoListed {
			out = append(out, fmt.Sprintf("%s (0x%04X): both registered and listed", name, e.cluster))
		}
	}
	for id := range listing {
		if _, ok := scoped[id]; !ok {
			out = append(out, fmt.Sprintf("stale listing: cluster 0x%04X has no fabric-scoped attribute in parity/schema.json", id))
		}
	}
	for id, name := range scoped {
		if _, ok := listing[id]; !ok && !registered[id] {
			out = append(out, fmt.Sprintf("%s (0x%04X): fabric-scoped, but no server implementing contract.FabricScopedState is registered and the cluster is not listed", name, id))
		}
	}
	for _, b := range builds {
		name, ok := scoped[b.cluster]
		if !ok {
			continue
		}
		if _, ok := listing[b.cluster]; ok {
			continue
		}
		if !inPkg[[2]string{b.pkg, strconv.FormatUint(uint64(b.cluster), 10)}] {
			out = append(out, fmt.Sprintf("%s (0x%04X): %s builds a server over its definition, and no server of package %s is registered", name, b.cluster, b.site, b.pkg))
		}
	}
	sort.Strings(out)
	return out
}

// TestEveryFabricScopedServerHasARemovalPath is the guard.
func TestEveryFabricScopedServerHasARemovalPath(t *testing.T) {
	t.Parallel()
	scoped, err := fabricScopedClusters(parity.SchemaJSON())
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) == 0 {
		t.Fatal("parity/schema.json marks no attribute fabric-scoped: the schema's shape moved")
	}
	builds, err := scanBuilt(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) == 0 {
		t.Fatal("the scan found no server built over a definition: the scan is broken")
	}
	for _, v := range check(scoped, servers, listed, builds) {
		t.Error(v)
	}
}

// bare is a server without the removal contract.
type bare struct{}

// TestCheckFindsAServerWithoutARemovalPath is the guard's negative case.
func TestCheckFindsAServerWithoutARemovalPath(t *testing.T) {
	t.Parallel()
	scoped := map[uint32]string{0x0097: "Messages", 0x001E: "Binding", 0x0553: "WebRtcTransportProvider"}
	entries := []server{
		{0x0097, (*bare)(nil)},          // registered without the contract
		{0x001E, (*core.Binding)(nil)},  // fine
		{0x0006, (*core.Messages)(nil)}, // not fabric-scoped
	}
	builds := []built{
		{pkg: "cluster/core", cluster: 0x001E, site: "cluster/core/binding.go:1"},      // registered there
		{pkg: "cluster/other", cluster: 0x001E, site: "cluster/other/binding.go:7"},    // a second, unregistered server
		{pkg: "cluster/camera", cluster: 0x0553, site: "cluster/camera/provider.go:9"}, // nobody registers the cluster
	}
	got := check(scoped, entries, map[uint32]string{0x0008: "stale"}, builds)
	want := []string{
		"Binding (0x001E): cluster/other/binding.go:7 builds a server over its definition, and no server of package cluster/other is registered",
		"Messages (0x0097): *fabricscopecheck.bare does not implement contract.FabricScopedState",
		"WebRtcTransportProvider (0x0553): cluster/camera/provider.go:9 builds a server over its definition, and no server of package cluster/camera is registered",
		"WebRtcTransportProvider (0x0553): fabric-scoped, but no server implementing contract.FabricScopedState is registered and the cluster is not listed",
		"stale entry: cluster 0x0006 (*core.Messages) has no fabric-scoped attribute in parity/schema.json",
		"stale listing: cluster 0x0008 has no fabric-scoped attribute in parity/schema.json",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
