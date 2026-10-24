// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js
// packages/node/test/endpoint/validation/ConstructionValidationTest.ts at
// 862ad75f: device-type validation at construction — the initial tree and
// an addition judged, a strict violation refused, a composition judged
// once its parts are in place, a misplaced singleton refused in every
// mode, a mandatory command left unimplemented, off mode.
//
// matter.js judges a construction root's tree once its parts are
// constructed; the Go validator judges the whole tree once per reassembly
// (ADR 0016), the first reassembly holding every endpoint as added. Each
// case ports as a verdict over a fake tree and the service of
// devicetype_service_helpers_test.go: what is logged (a line per endpoint),
// what is refused, what is recorded. The counts of validations and passes
// (countingValidations) are not ported; the phases of a construction — a
// crashed endpoint, an endpoint still initializing, a part constructed
// before its owner — have no Go subject, since a snapshot holds an
// endpoint whole or not at all; peers and BLE are not judged on the Go
// device side; the whole-node cases are
// node:TestDeviceTypeValidationNodeParityMatterJS.
package endpoint

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tcdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	"github.com/SukramJ/go-fabric/schema"
)

const (
	skipPhases = "the phases of a construction have no Go subject: a snapshot holds an endpoint whole or not at all, so no endpoint is judged while it or its owner is still constructing"
	skipBLE    = "BLE commissioning has no Go subject on the device side (ADR 0024)"
)

// fakeSubject adapts a fake endpoint to [DeviceTypeSubject] for what the
// validator adds beside the device-type judgement — the commands its
// servers leave unimplemented.
type fakeSubject struct {
	f  *fakeTree
	ep *fakeEP
}

func (s fakeSubject) EndpointNumber() uint16     { return s.ep.id }
func (s fakeSubject) DeviceTypeIDs() []uint32    { return s.ep.deviceTypes }
func (s fakeSubject) ServerClusterIDs() []uint32 { return s.ep.order }
func (s fakeSubject) ClientClusterIDs() []uint32 { return s.ep.clients }
func (s fakeSubject) StatedConditions() []string { return s.ep.stated }
func (s fakeSubject) PartNumbers() []uint16      { return s.f.partsOf(s.ep.id) }
func (s fakeSubject) ServerClusterShape(cluster uint32) (ClusterShape, bool) {
	return ClusterShape{UnimplementedCommands: s.ep.unimplemented[cluster]}, true
}

// unimplementedPass is pass with the fake servers' unimplemented commands
// reported as [DeviceTypeValidator.Validate] reports a mounted server's.
func (s *service) unimplementedPass() ([]string, error) {
	shape := treeShape{parent: map[uint16]uint16{}}
	all := make([]DeviceTypeViolation, 0, len(s.f.eps))
	pass := newDTPass(s.f, s.l)
	for _, ep := range s.f.order() {
		shape.order = append(shape.order, ep.id)
		if ep.parent != nil {
			shape.parent[ep.id] = ep.parent.id
		}
		all = append(all, pass.judged(ep.id)...)
		all = append(all, unimplementedOf(fakeSubject{s.f, ep})...)
	}
	verdict, err := s.v.judge(all, shape)
	if err != nil {
		return nil, err
	}
	return linesOf(s.f, verdict.Fresh), nil
}

// lightWithGKM is an OnOffLight carrying GroupKeyManagement, a RootNode
// singleton (validation-helpers.ts:65 lightWithGroupKeyManagement).
func lightWithGKM(f *fakeTree, parent *fakeEP, name string) *fakeEP {
	return light(f, parent, name).server(clGroupKeyMgmt)
}

// nodelessModel: no device type is a node; RootNode declares
// GroupKeyManagement a singleton and OnOffLight requires Identify
// (ConstructionValidationTest.ts:181-198).
func nodelessModel() *dtLookups {
	return syntheticModel(
		&schema.DeviceTypeDefinition{Name: "Base", Classification: "base"},
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{singletonReq("GroupKeyManagement", clGroupKeyMgmt, schema.Conformance{})}},
		&schema.DeviceTypeDefinition{ID: dtOnOffLight, Name: "OnOffLight", Classification: "simple", Requirements: []schema.DeviceTypeRequirement{clusterReq("Identify", clIdentify, confM())}},
	)
}

// stubCabinet is a TemperatureControlledCabinet whose TemperatureControl
// server leaves SetTemperature unimplemented
// (ConstructionValidationTest.ts:596 stubCabinet, the generated behavior
// without SettableTemperatureControlServer's setTemperature).
func stubCabinet(f *fakeTree, parent *fakeEP) *fakeEP {
	ep := cabinetOf(f, parent, "cabinet")
	ep.unimplemented = map[uint32][]uint32{clTempControl: {tcdef.CmdSetTemperature}}
	return ep
}

func countLines(lines []string, substr string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, substr) {
			n++
		}
	}
	return n
}

func TestConstructionValidationParityMatterJS(t *testing.T) {
	t.Run("device type validation at construction", func(t *testing.T) {
		t.Run("judges a node's initial tree in one pass", func(t *testing.T) {
			// The one pass is not counted (countingValidations); every light
			// is judged and logged.
			f, root := nodeTree()
			for i := range 100 {
				lightWithoutIdentify(f, root, "light"+strconv.Itoa(i))
			}
			s := newService(t, DeviceTypeValidationWarn, deviceTypeLookups(), f)
			if n := countLines(s.mustPass(), "Identify"); n != 100 {
				t.Errorf("%d lines name Identify, want 100", n)
			}
		})

		t.Run("judges an endpoint added with its descendants in one pass", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationWarn)
			agg := aggregatorOf(f, root, "aggregator")
			for i := range 50 {
				lightWithoutIdentify(f, agg, "light"+strconv.Itoa(i))
			}
			if n := countLines(s.mustPass(), "Identify"); n != 50 {
				t.Errorf("%d lines name Identify, want 50", n)
			}
		})

		t.Run("refuses a strict violation at construction and leaves no child behind", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationStrict)
			l := lightWithoutIdentify(f, root, "light")
			var refusal *DeviceTypeConformanceError
			if _, err := s.pass(); !errors.As(err, &refusal) {
				t.Fatalf("pass = %v, want a refusal", err)
			}
			f.remove(l)
			wantRequirements(t, s.requirementsOf(l))
			s.mustPass()
		})

		t.Run("judges a composition in strict mode once the parts are constructed", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationStrict)
			empty, _ := fridgeOf(f, root, "empty", 0)
			if _, err := s.pass(); err == nil {
				t.Fatal("an empty fridge was accepted")
			}
			f.remove(empty)
			fridge, _ := fridgeOf(f, root, "fridge", 1)
			s.mustPass()
			wantRequirements(t, s.requirementsOf(fridge))
		})

		t.Run("logs a composition violation when not strict", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationWarn)
			fridgeOf(f, root, "fridge", 0)
			logged := s.mustPass()
			if countLines(logged, "fridge") != 1 || !strings.Contains(joined(logged), "instanceCount Refrigerator device:TemperatureControlledCabinet") {
				t.Errorf("logged %v", logged)
			}
		})

		t.Run("judges the composing ancestor when a child is added later", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationWarn)
			fridge, _ := fridgeOf(f, root, "fridge", 0)
			s.mustPass()
			if len(s.requirementsOf(fridge)) == 0 {
				t.Fatal("the empty fridge conforms")
			}
			cabinetOf(f, fridge, "cabinet")
			s.mustPass()
			wantRequirements(t, s.requirementsOf(fridge))
		})

		t.Run("refuses a misplaced singleton before the behaviors initialize", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationWarn)
			lightWithGKM(f, root, "light")
			if _, err := s.pass(); !slices.Equal(refusalNames(f, err), []string{"light"}) {
				t.Errorf("pass = %v, want the light refused", err)
			}
		})

		for _, mode := range []DeviceTypeValidationMode{DeviceTypeValidationWarn, DeviceTypeValidationStrict} {
			t.Run("accepts a RootNode singleton on a bridged node whose device type lists it, in "+mode.String()+" mode", func(t *testing.T) {
				// A Fabric Synchronization bridged node carries
				// AdministratorCommissioning for its bridged node.
				s, f, root := standardNode(t, mode)
				agg := aggregatorOf(f, root, "aggregator")
				s.mustPass()
				f.add("bridged", agg, dtBridgedNode).server(clDescriptor).server(clBridgedBasic).server(clAdminComm)
				logged, err := s.pass()
				if err != nil || len(logged) != 0 {
					t.Errorf("pass = %v, %v; want it accepted and nothing logged", logged, err)
				}
			})
		}

		t.Run("refuses a misplaced singleton below an added endpoint before any behavior initializes", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationWarn)
			agg := aggregatorOf(f, root, "aggregator")
			lightWithGKM(f, agg, "light")
			if _, err := s.pass(); !slices.Equal(refusalNames(f, err), []string{"light"}) {
				t.Errorf("pass = %v, want aggregator.light refused", err)
			}
		})

		t.Run("refuses only the first endpoint in tree order that misplaces a singleton", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationWarn)
			agg := aggregatorOf(f, root, "aggregator")
			lightWithGKM(f, agg, "first")
			lightWithGKM(f, agg, "second")
			if _, err := s.pass(); !slices.Equal(refusalNames(f, err), []string{"first"}) {
				t.Errorf("pass = %v, want only aggregator.first refused", err)
			}
		})

		t.Run("accepts a singleton on an endpoint configured as a node endpoint of its own", func(t *testing.T) {
			s, f, root := standardNode(t, DeviceTypeValidationWarn)
			nested := lightWithGKM(f, root, "nested")
			nested.deviceTypes = []uint32{dtRootNode}
			if _, err := s.pass(); err != nil {
				t.Errorf("refused: %v", err)
			}
		})

		t.Run("reads nothing of an endpoint that crashed", func(t *testing.T) { t.Skip(skipCrash) })
		t.Run("does not count a crashed endpoint as a present component", func(t *testing.T) { t.Skip(skipCrash) })
		t.Run("judges an addition under a constructed part without its ancestors still under construction", func(t *testing.T) { t.Skip(skipPhases) })
		t.Run("judges no endpoint under construction below a sibling whose duplicate status changes", func(t *testing.T) { t.Skip(skipPhases) })
		t.Run("reads nothing of a sibling whose behaviors are still initializing", func(t *testing.T) { t.Skip(skipPhases) })

		t.Run("judges nothing in a tree without a node endpoint", func(t *testing.T) {
			f, root := syntheticNode()
			lightWithoutIdentify(f, root, "light").server(clGroupKeyMgmt)
			s := newService(t, DeviceTypeValidationWarn, nodelessModel(), f)
			logged, err := s.pass()
			if err != nil || len(logged) != 0 {
				t.Errorf("pass = %v, %v; want nothing judged", logged, err)
			}
		})

		t.Run("never judges a peer's endpoints", func(t *testing.T) { t.Skip(skipPeer) })
		t.Run("never refuses a peer's misplaced singleton", func(t *testing.T) { t.Skip(skipPeer) })
		t.Run("constructs a strict node in default configuration", func(t *testing.T) { t.Skip(skipNode) })
		t.Run("refuses a strict node that commissions over BLE without NetworkCommissioning", func(t *testing.T) { t.Skip(skipBLE) })

		t.Run("of a cluster server whose mandatory command throws unimplemented", func(t *testing.T) {
			// The generated server with SetTemperature left unimplemented:
			// what it lists and what it reports.
			srv, err := spec.NewServer(tcdef.Definition, spec.Options{Features: uint32(tcdef.FeatureTemperatureNumber)}, spec.ServerConfig{Unimplemented: true})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(srv.UnimplementedMandatoryCommands(), []uint32{tcdef.CmdSetTemperature}) || slices.Contains(srv.MatterAcceptedCommands(), tcdef.CmdSetTemperature) {
				t.Fatalf("unimplemented %v, accepted %v", srv.UnimplementedMandatoryCommands(), srv.MatterAcceptedCommands())
			}

			t.Run("refuses it in strict mode", func(t *testing.T) {
				s, f, root := standardNode(t, DeviceTypeValidationStrict)
				stubCabinet(f, root)
				_, err := s.unimplementedPass()
				var refusal *DeviceTypeConformanceError
				if !errors.As(err, &refusal) || refusal.Violations[0].Kind != ViolationUnimplemented {
					t.Errorf("pass = %v, want the cabinet refused for SetTemperature", err)
				}
			})
			for _, mode := range []DeviceTypeValidationMode{DeviceTypeValidationWarn, DeviceTypeValidationOff} {
				t.Run("logs it and leaves it out of the accepted commands in "+mode.String()+" mode", func(t *testing.T) {
					s, f, root := standardNode(t, mode)
					stubCabinet(f, root)
					logged, err := s.unimplementedPass()
					if err != nil || !strings.Contains(joined(logged), "unimplemented TemperatureControl TemperatureControl.SetTemperature") {
						t.Errorf("pass = %v, %v; want SetTemperature logged", logged, err)
					}
				})
			}
		})

		t.Run("in off mode", func(t *testing.T) {
			t.Run("judges neither the initial tree nor an addition", func(t *testing.T) {
				f, root := nodeTree()
				for i := range 3 {
					lightWithoutIdentify(f, root, "light"+strconv.Itoa(i))
				}
				s := newService(t, DeviceTypeValidationOff, deviceTypeLookups(), f)
				first := s.mustPass()
				fridge, _ := fridgeOf(f, root, "fridge", 0)
				second := s.mustPass()
				if len(first) != 0 || len(second) != 0 {
					t.Errorf("logged %v, %v", first, second)
				}
				wantRequirements(t, s.requirementsOf(fridge))
			})
			t.Run("still refuses a misplaced singleton before the behaviors initialize", func(t *testing.T) {
				s, f, root := standardNode(t, DeviceTypeValidationOff)
				lightWithGKM(f, root, "light")
				if _, err := s.pass(); !slices.Equal(refusalNames(f, err), []string{"light"}) {
					t.Errorf("pass = %v, want the light refused", err)
				}
			})
		})

		t.Run("fails the node's construction for an unknown mode", func(t *testing.T) { t.Skip(skipNode) })

		t.Run("in default configuration", func(t *testing.T) {
			t.Run("logs no warning for a plain light", func(t *testing.T) { t.Skip(skipNode) })
			t.Run("logs no warning for a bridge", func(t *testing.T) { t.Skip(skipNode) })
		})
	})
}
