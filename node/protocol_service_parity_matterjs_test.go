// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/endpoint/server/ProtocolServiceTest.ts
// at 862ad75f: the node's protocol service as one unit — fabric-filtered
// reads and writes, subscribe and notify, the command lists and the
// FeatureMap a server publishes, invoke, the deprecated EventList, every
// attribute value encoding, and command payload validation.
//
// matter.js drives a MockServerNode through its interaction helpers; the
// port speaks over the wire from commissioned fabrictest pairs, a second
// fabric commissioned by a second controller. The servers matter.js
// declares as behaviors are generic spec.Servers here (a command handler
// registered where matter.js overrides a method, the rest left
// unimplemented); the synthetic TestCluster of the command-validation
// cases is a definition registered for the test. What has no Go subject
// is skipped with its reason: the protocol state read with and without a
// transaction, and the request TLV schema's constraint.
package node_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	ncdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

const (
	psClOpCreds            = 0x003E
	psAttrNOCs             = 0x0000
	psAttrFabrics          = 0x0001
	attrCommissionedFabric = 0x0003
	psClACL                = 0x001F
	psAttrACL              = 0x0000
	psClBasicInfo          = 0x0028
	psEvLeave              = 0x02
	tagFabricIndex         = 0xFE
)

// adminEntry is AcesWithoutFabric (ProtocolServiceTest.ts:105-110 at b25ed960) for an
// Administer CASE entry with no subjects and no targets: the ACL a fabric
// writes, its fabric index the device's.
type adminEntry struct{}

func (adminEntry) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	spec.PutUint[uint8](enc, tlv.ContextTag(1), 5) // Administer
	spec.PutUint[uint8](enc, tlv.ContextTag(2), 2) // CASE
	enc.PutNull(tlv.ContextTag(3))
	enc.PutNull(tlv.ContextTag(4))
	_ = enc.EndContainer()
}

// fieldsOf maps the context-tagged fields of each struct of a list to
// their unsigned values (or 1 for a present non-integer field).
func fieldsOf(list spec.Node) []map[uint32]uint64 {
	out := make([]map[uint32]uint64, 0, len(list.Children))
	for _, entry := range list.Children {
		m := map[uint32]uint64{}
		for _, f := range entry.Children {
			if f.El.Tag.Kind != tlv.TagKindContext {
				continue
			}
			if f.El.Type >= tlv.TypeUnsignedInt1 && f.El.Type <= tlv.TypeUnsignedInt8 {
				m[f.El.Tag.Number] = f.El.Uint
			} else {
				m[f.El.Tag.Number] = 1
			}
		}
		out = append(out, m)
	}
	return out
}

// twoFabrics commissions a device onto a first and then a second fabric.
func twoFabrics(t *testing.T, topo fabrictest.Topology) (site *fabrictest.Site, first, second *fabrictest.Pair) {
	t.Helper()
	site = quietSite(t)
	first = site.CommissionedPair(fabrictest.PairOptions{Device: topo})
	second = secondFabric(t, site, first)
	return site, first, second
}

func readWith(t *testing.T, p *fabrictest.Pair, noFabricFilter bool, path im.ConcreteAttributePath) spec.Node {
	t.Helper()
	r, err := fabrictest.ReadWith(parityCtx(t), p, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}, NoFabricFilter: noFabricFilter})
	if err != nil {
		t.Fatal(err)
	}
	v, ok := r.Value(path.Endpoint, path.Cluster, path.Attribute)
	if !ok {
		t.Fatalf("%v: no value", path)
	}
	return v
}

// The synthetic TestCluster of the command-validation cases
// (ProtocolServiceTest.ts:693-727 at b25ed960): TestRequest (Enable, Value max 10)
// answered by TestResponse, whose Result is mandatory when
// TestRequest.Enable.
const testClusterID = 0xFFF1FC01

type testRequest struct {
	Enable bool
	Value  uint8
}

type testResponse struct {
	Result *uint8
}

func (r testResponse) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	spec.PutOptional(enc, tlv.ContextTag(0), r.Result, spec.PutUint[uint8])
	_ = enc.EndContainer()
}

func (testResponse) ResponseCommand() (cluster, command uint32) { return testClusterID, 1 }

var testClusterDef = &spec.Cluster{
	ID: testClusterID, Name: "TestCluster", Revision: 1,
	Commands: []spec.Command{
		{
			ID: 1, Name: "TestRequest", Direction: spec.Request, Response: "TestResponse", Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory},
			Fields: []spec.Field{
				{ID: 0, Name: "Enable", Type: spec.Type{Kind: spec.KindBool}, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}},
				{ID: 1, Name: "Value", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Constraint: spec.Constraint{Text: "max 10", Max: &spec.Bound{Int: 10}}},
			},
			// The decoder clustergen emits for these fields: a Go
			// definition holds a request field's bound in its decoder
			// (spec.DecodeUintIn), where matter.js builds it into the
			// request TLV schema of the model (review II, R2-P5-tassert-09).
			Decode: func(n spec.Node) (any, error) {
				var req testRequest
				err := spec.DecodeFields(n, "TestRequest",
					spec.Bind(0, "Enable", true, &req.Enable, spec.DecodeBool),
					spec.Bind(1, "Value", true, &req.Value, spec.DecodeUintIn[uint8](8, 0, 10)),
				)
				return req, err
			},
		},
		{
			ID: 1, Name: "TestResponse", Direction: spec.Response, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory},
			// The response's generated form is testResponse: the response
			// check judges a payload of the definition's own type only
			// (decision 339, A3).
			Decode: func(spec.Node) (any, error) { return testResponse{}, nil },
			Fields: []spec.Field{
				{ID: 0, Name: "Result", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{
					Text: "TestRequest.Enable", Op: spec.ConfDot,
					Args: []spec.Conformance{{Op: spec.ConfName, Name: "TestRequest"}, {Op: spec.ConfName, Name: "Enable"}},
				}},
			},
		},
	},
}

func init() { spec.Register(testClusterDef) }

// testDevice is a commissioned pair whose bridged endpoint serves the
// TestCluster with handler.
func testDevice(t *testing.T, handler func(req testRequest) testResponse) (p *fabrictest.Pair, ep uint16) {
	t.Helper()
	srv, err := spec.NewServer(testClusterDef, spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{
		1: spec.HandlerOf(func(_ context.Context, req testRequest) (testResponse, error) { return handler(req), nil }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	p = quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{fabrictest.Endpoint("test", onoff.DeviceTypeOnOffLight, srv)},
	}})
	ep, ok := p.Device.EndpointOf("test")
	if !ok {
		t.Fatal("the test endpoint has no number")
	}
	return p, ep
}

// testRequestFields is TlvTestRequest (ProtocolServiceTest.ts:693 at b25ed960).
type testRequestFields testRequest

func (r testRequestFields) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	spec.PutBool(enc, tlv.ContextTag(0), r.Enable)
	spec.PutUint[uint8](enc, tlv.ContextTag(1), r.Value)
	_ = enc.EndContainer()
}

func TestProtocolServiceParityMatterJS(t *testing.T) {
	t.Run("ProtocolServiceTest", func(t *testing.T) {
		t.Run("properly fabric filters reads and writes", func(t *testing.T) {
			_, p1, p2 := twoFabrics(t, fabrictest.Topology{})
			acl := client.AttributeAt(0, psClACL, psAttrACL)
			for _, p := range []*fabrictest.Pair{p1, p2} {
				if err := fabrictest.Write(parityCtx(t), p, acl, spec.List[adminEntry]{{}}); err != nil {
					t.Fatalf("ACL write: %v", err)
				}
			}
			f1, f2 := uint64(fabricIndexOf(t, p1)), uint64(fabricIndexOf(t, p2))
			admin := func(fabric uint64) map[uint32]uint64 {
				return map[uint32]uint64{1: 5, 2: 2, 3: 1, 4: 1, tagFabricIndex: fabric}
			}
			for _, c := range []struct {
				p      *fabrictest.Pair
				fabric uint64
			}{{p1, f1}, {p2, f2}} {
				got := fieldsOf(readWith(t, c.p, false, acl))
				if len(got) != 1 || !mapsEqual(got[0], admin(c.fabric)) {
					t.Errorf("fabric %d reads ACL %v, want its own entry", c.fabric, got)
				}
			}
			all := fieldsOf(readWith(t, p1, true, acl))
			if len(all) != 2 || !mapsEqual(all[0], admin(f1)) || !mapsEqual(all[1], map[uint32]uint64{tagFabricIndex: f2}) {
				t.Errorf("unfiltered ACL read by fabric %d = %v, want its entry and fabric %d's index alone", f1, all, f2)
			}
			nocs := client.AttributeAt(0, psClOpCreds, psAttrNOCs)
			for _, c := range []struct {
				p      *fabrictest.Pair
				fabric uint64
			}{{p1, f1}, {p2, f2}} {
				got := fieldsOf(readWith(t, c.p, false, nocs))
				if len(got) != 1 || got[0][tagFabricIndex] != c.fabric || got[0][1] != 1 {
					t.Errorf("fabric %d reads NOCs %v, want its own NOC", c.fabric, got)
				}
			}
		})

		t.Run("properly handles subscribe and notify of attributes and events", func(t *testing.T) {
			site := quietSite(t)
			p1 := site.CommissionedPair(fabrictest.PairOptions{})
			fabrics, nocs, commissioned := client.AttributeAt(0, psClOpCreds, psAttrFabrics), client.AttributeAt(0, psClOpCreds, psAttrNOCs), client.AttributeAt(0, psClOpCreds, attrCommissionedFabric)
			reports := make(chan *interaction.Report, 32)
			minFloor, maxCeiling := time.Second, 2*time.Second
			sub, err := fabrictest.Subscribe(parityCtx(t), p1, interaction.SubscribeRequest{
				Attributes:       []im.ConcreteAttributePath{fabrics, nocs, commissioned},
				Events:           []im.ConcreteEventPath{client.EventAt(0, psClBasicInfo, psEvLeave)},
				NoFabricFilter:   true,
				MinIntervalFloor: &minFloor, MaxIntervalCeiling: &maxCeiling,
				Updated: func(r *interaction.Report) error { reports <- r; return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Close()
			drain(reports)

			p2 := secondFabric(t, site, p1)
			f2 := fabricIndexOf(t, p2)
			var seen []uint32
			var fabricCount, fabricList int
			err = site.Eventually(parityCtx(t), "the fabric reports", func() bool {
				for {
					select {
					case r := <-reports:
						for _, a := range r.Attributes {
							if !slices.Contains(seen, a.Path.Attribute) {
								seen = append(seen, a.Path.Attribute)
							}
							switch a.Path.Attribute {
							case attrCommissionedFabric:
								fabricCount = int(a.Value.El.Uint) //nolint:gosec // a small count
							case psAttrFabrics:
								fabricList = len(a.Value.Children)
							}
						}
					default:
						return fabricCount == 2 && fabricList == 2
					}
				}
			})
			if err != nil {
				t.Fatalf("%v: seen %v", err, seen)
			}
			// NOCs carries the "changes omitted" quality, so its path stays silent.
			if slices.Contains(seen, psAttrNOCs) {
				t.Errorf("reported %v; NOCs reported", seen)
			}

			if err := site.Run(parityCtx(t), func(ctx context.Context) error {
				return p1.Device.Act(func(n *node.Node) error { return n.RemoveFabric(ctx, f2) })
			}); err != nil {
				t.Fatal(err)
			}
			var leave *interaction.EventData
			err = site.Eventually(parityCtx(t), "the Leave event", func() bool {
				select {
				case r := <-reports:
					for i := range r.Events {
						if e := &r.Events[i]; e.Path.Cluster == psClBasicInfo && e.Path.Event == psEvLeave {
							leave = e
						}
					}
				default:
				}
				return leave != nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := fieldsOf(spec.Node{Children: []spec.Node{leave.Value}}); len(got) != 1 || got[0][0] != uint64(f2) {
				t.Errorf("Leave data %v, want fabric %d", got, f2)
			}
		})

		t.Run("indicates support only for implemented commands", func(t *testing.T) {
			srv, err := spec.NewServer(ncdef.Definition, spec.Options{Features: uint32(ncdef.FeatureWiFiNetworkInterface)}, spec.ServerConfig{
				Unimplemented: true,
				Initial:       map[uint32]any{ncdef.AttrMaxNetworks: uint8(4)},
				Handlers: map[uint32]spec.CommandHandler{
					ncdef.CmdScanNetworks: spec.HandlerOf(func(context.Context, ncdef.ScanNetworksRequest) (ncdef.ScanNetworksResponse, error) {
						return ncdef.ScanNetworksResponse{NetworkingStatus: ncdef.NetworkCommissioningStatusSuccess}, nil
					}),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			p := quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
				Bridged: []node.Endpoint{fabrictest.Endpoint("light", onoff.DeviceTypeOnOffLight, srv)},
			}})
			ep, _ := p.Device.EndpointOf("light")
			accepted := listOf(readWith(t, p, true, client.AttributeAt(ep, ncdef.ClusterID, 0xFFF9)))
			generated := listOf(readWith(t, p, true, client.AttributeAt(ep, ncdef.ClusterID, 0xFFF8)))
			if !slices.Equal(accepted, []uint64{uint64(ncdef.CmdScanNetworks)}) || !slices.Equal(generated, []uint64{uint64(ncdef.CmdScanNetworksResponse)}) {
				t.Errorf("AcceptedCommandList %v, GeneratedCommandList %v", accepted, generated)
			}
		})

		t.Run("publishes correct feature map", func(t *testing.T) {
			srv, err := onoff.New(onoff.Config{Features: onoff.Feature(onoff.FeatureLighting)})
			if err != nil {
				t.Fatal(err)
			}
			p := quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
				Bridged: []node.Endpoint{fabrictest.Endpoint("light", onoff.DeviceTypeOnOffLight, srv)},
			}})
			ep, _ := p.Device.EndpointOf("light")
			// Lighting set, DeadFrontBehavior and OffOnly clear.
			if got := readWith(t, p, true, client.AttributeAt(ep, onoffdef.ClusterID, 0xFFFC)); got.El.Uint != uint64(onoffdef.FeatureLighting) {
				t.Errorf("FeatureMap 0x%X, want Lighting alone", got.El.Uint)
			}
		})

		// describe("invoke") (:317-540 at b25ed960): the request is held to
		// the model before the handler, every error collected
		// (ProtocolService.ts:671-705 at b25ed960).
		t.Run("invoke", func(t *testing.T) {
			// invokeOffWithEffect is :318-350: OnOff with Lighting, its
			// OffWithEffect handler recording the request.
			invokeOffWithEffect := func(t *testing.T, req onoffdef.OffWithEffectRequest) (*onoffdef.OffWithEffectRequest, error) {
				t.Helper()
				var received *onoffdef.OffWithEffectRequest
				srv, err := spec.NewServer(onoffdef.Definition, spec.Options{Features: uint32(onoffdef.FeatureLighting)}, spec.ServerConfig{
					Unimplemented: true,
					Handlers: map[uint32]spec.CommandHandler{
						onoffdef.CmdOffWithEffect: spec.HandlerOf(func(_ context.Context, req onoffdef.OffWithEffectRequest) (struct{}, error) {
							received = &req
							return struct{}{}, nil
						}),
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				p := quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
					Bridged: []node.Endpoint{fabrictest.Endpoint("light", onoff.DeviceTypeOnOffLight, srv)},
				}})
				ep, _ := p.Device.EndpointOf("light")
				_, err = fabrictest.Invoke(parityCtx(t), p, client.CommandAt(ep, onoffdef.ClusterID, onoffdef.CmdOffWithEffect), req)
				return received, err
			}

			t.Run("invokes", func(t *testing.T) {
				received, err := invokeOffWithEffect(t, onoffdef.OffWithEffectRequest{EffectIdentifier: 0, EffectVariant: 1})
				if err != nil {
					t.Fatalf("invoke: %v, want Success", err)
				}
				if received == nil || received.EffectIdentifier != 0 || received.EffectVariant != 1 {
					t.Errorf("received %+v", received)
				}
			})

			t.Run("refuses a request with an enum value the enum does not define with CONSTRAINT_ERROR", func(t *testing.T) {
				received, err := invokeOffWithEffect(t, onoffdef.OffWithEffectRequest{EffectIdentifier: 2, EffectVariant: 0})
				if status := imStatus(t, err); status != im.StatusConstraintError {
					t.Errorf("invoke: %v, want CONSTRAINT_ERROR", err)
				}
				if received != nil {
					t.Errorf("the handler ran: %+v", received)
				}
			})

			t.Run("refuses an undefined enum value also when an earlier field has an error that only gets a warning", func(t *testing.T) {
				// UpdateFlags carries a reserved bit, and Action is undefined.
				var ran atomic.Bool
				features := ccdef.FeatureHueSaturation | ccdef.FeatureEnhancedHue | ccdef.FeatureColorLoop
				srv, err := spec.NewServer(ccdef.Definition, spec.Options{Features: uint32(features)}, spec.ServerConfig{
					Unimplemented: true,
					Handlers: map[uint32]spec.CommandHandler{
						ccdef.CmdColorLoopSet: func(context.Context, any) (any, error) { ran.Store(true); return nil, nil },
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				p := quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
					Bridged: []node.Endpoint{fabrictest.Endpoint("light", onoff.DeviceTypeOnOffLight, srv)},
				}})
				ep, _ := p.Device.EndpointOf("light")
				_, err = fabrictest.Invoke(parityCtx(t), p, client.CommandAt(ep, ccdef.ClusterID, ccdef.CmdColorLoopSet),
					ccdef.ColorLoopSetRequest{UpdateFlags: 0x11, Action: 3})
				if status := imStatus(t, err); status != im.StatusConstraintError {
					t.Errorf("invoke: %v, want CONSTRAINT_ERROR", err)
				}
				if ran.Load() {
					t.Error("the handler ran")
				}
			})

			t.Run("accepts any value for an enum that defines no values", func(t *testing.T) {
				req := onoffdef.OffWithEffectRequest{EffectIdentifier: 0, EffectVariant: 7}
				received, err := invokeOffWithEffect(t, req)
				if err != nil {
					t.Fatalf("invoke: %v, want Success", err)
				}
				if received == nil || received.EffectVariant != 7 {
					t.Errorf("received %+v", received)
				}
				// No "Request validation error" warning: the gate logs only
				// a request its check finds an error in.
				inst, err := spec.New(onoffdef.Definition, spec.Options{Features: uint32(onoffdef.FeatureLighting)})
				if err != nil {
					t.Fatal(err)
				}
				if err := inst.CheckRequest(onoffdef.CmdOffWithEffect, req, nil); err != nil {
					t.Errorf("the request check found %v", err)
				}
			})

			t.Run("with cluster-specific handling of invalid requests", func(t *testing.T) {
				// invokeOnNode is :426-451, on the device's own servers.
				root := func(t *testing.T) *fabrictest.Pair {
					t.Helper()
					return quietSite(t).CommissionedPair(fabrictest.PairOptions{})
				}
				invokeOnNode := func(t *testing.T, p *fabrictest.Pair, ep uint16, cluster, cmd uint32, fields spec.Encodable) (interaction.Result, error) {
					t.Helper()
					return fabrictest.Invoke(parityCtx(t), p, client.CommandAt(ep, cluster, cmd), fields)
				}

				t.Run("answers an undefined certificate type with INVALID_COMMAND", func(t *testing.T) {
					_, err := invokeOnNode(t, root(t), 0, opcredsdef.ClusterID, opcredsdef.CmdCertificateChainRequest,
						opcredsdef.CertificateChainRequestRequest{CertificateType: 3})
					if status := imStatus(t, err); status != im.StatusInvalidCommand {
						t.Errorf("invoke: %v, want INVALID_COMMAND", err)
					}
				})

				t.Run("keeps the TLV check for a command without configuration in a cluster with configured commands", func(t *testing.T) {
					_, err := invokeOnNode(t, root(t), 0, opcredsdef.ClusterID, opcredsdef.CmdAttestationRequest, tlvFields(func(*tlv.Encoder) {}))
					if status := imStatus(t, err); status != im.StatusInvalidCommand {
						t.Errorf("invoke: %v, want INVALID_COMMAND", err)
					}
				})

				t.Run("answers an undefined regulatory location with ValueOutsideRange in the response", func(t *testing.T) {
					res, err := invokeOnNode(t, root(t), 0, gencommdef.ClusterID, gencommdef.CmdSetRegulatoryConfig,
						gencommdef.SetRegulatoryConfigRequest{NewRegulatoryConfig: 3, CountryCode: "XX", Breadcrumb: 1})
					if err != nil || !res.HasFields {
						t.Fatalf("invoke = %+v, %v; want a response", res, err)
					}
					if got, ok := field(res.Fields, 0); !ok || got.Uint != uint64(gencommdef.CommissioningErrorValueOutsideRange) {
						t.Errorf("ErrorCode %+v, want ValueOutsideRange", got)
					}
				})

				t.Run("answers a missing mandatory field with INVALID_COMMAND for a command with a field override", func(t *testing.T) {
					_, err := invokeOnNode(t, root(t), 0, gencommdef.ClusterID, gencommdef.CmdSetRegulatoryConfig, tlvFields(func(enc *tlv.Encoder) {
						enc.PutUTF8(tlv.ContextTag(1), "XX")
						spec.PutUint[uint8](enc, tlv.ContextTag(2), 1)
					}))
					if status := imStatus(t, err); status != im.StatusInvalidCommand {
						t.Errorf("invoke: %v, want INVALID_COMMAND", err)
					}
				})

				// identifyingPair is a pair whose bridged endpoint's source is
				// the host's IdentifyHandler, recording each effect.
				identifyingPair := func(t *testing.T) (*fabrictest.Pair, uint16, *effectSource) {
					t.Helper()
					src := &effectSource{}
					p := quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
						Bridged: []node.Endpoint{identifyingEndpoint{inner: kitLight(t, "light"), src: src}},
					}})
					ep, ok := p.Device.EndpointOf("light")
					if !ok {
						t.Fatal("the light has no endpoint number")
					}
					return p, ep, src
				}

				t.Run("accepts an identify effect the enum does not define", func(t *testing.T) {
					p, ep, src := identifyingPair(t)
					if _, err := invokeOnNode(t, p, ep, identifydef.ClusterID, identifydef.CmdTriggerEffect,
						identifydef.TriggerEffectRequest{EffectIdentifier: 3, EffectVariant: identifydef.EffectVariantDefault}); err != nil {
						t.Fatalf("invoke: %v, want SUCCESS", err)
					}
					if got := src.recorded(); !slices.Equal(got, [][2]uint8{{3, 0}}) {
						t.Errorf("effects %v, want [[3 0]]", got)
					}
				})

				t.Run("runs the default identify effect variant for a variant the enum does not define", func(t *testing.T) {
					p, ep, src := identifyingPair(t)
					if _, err := invokeOnNode(t, p, ep, identifydef.ClusterID, identifydef.CmdTriggerEffect,
						identifydef.TriggerEffectRequest{EffectIdentifier: identifydef.EffectIdentifierBlink, EffectVariant: 0x42}); err != nil {
						t.Fatalf("invoke: %v, want SUCCESS", err)
					}
					want := [][2]uint8{{uint8(identifydef.EffectIdentifierBlink), uint8(identifydef.EffectVariantDefault)}}
					if got := src.recorded(); !slices.Equal(got, want) {
						t.Errorf("effects %v, want %v", got, want)
					}
				})
			})
		})

		t.Run("excludes deprecated global attributes like EventList from protocol", func(t *testing.T) {
			p := quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{kitLight(t, "light")}}})
			r, err := fabrictest.ReadWith(parityCtx(t), p, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{Attribute: 0xFFFB, HasAttribute: true}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Attributes) == 0 {
				t.Fatal("no AttributeList read")
			}
			for _, a := range r.Attributes {
				if slices.Contains(listOf(a.Value), 0xFFFA) {
					t.Errorf("endpoint %d cluster 0x%04X lists the deprecated EventList", a.Path.Endpoint, a.Path.Cluster)
				}
			}
		})

		t.Run("tags manufacturer-specific attributes in a standard cluster with skipCustomElements", func(t *testing.T) {
			// ProtocolServiceTest.ts:572-607 at b25ed960: an attribute with an MEI prefix
			// in a standard-range cluster (0x1234) is skipped by
			// skipCustomElements and not by skipGlobalAttributes — the
			// per-attribute branch of im.ApplyWildcardPathFlags (matter.js
			// ProtocolService.ts:491-493).
			mei := im.ConcreteAttributePath{Endpoint: 1, Cluster: 0x1234, Attribute: 0xFFF1_0001, HasEndpoint: true, HasCluster: true, HasAttribute: true}
			standard := mei
			standard.Attribute = 0
			wild := func(flags uint32) im.ConcreteAttributePath {
				return im.ConcreteAttributePath{Cluster: 0x1234, HasCluster: true, WildcardPathFlags: flags}
			}
			ctx := t.Context()
			if !im.WildcardPathFlagsSkip(ctx, nil, wild(im.WildcardSkipCustomElements), mei) {
				t.Error("MEI attribute must be flagged skipCustomElements")
			}
			if im.WildcardPathFlagsSkip(ctx, nil, wild(im.WildcardSkipGlobalAttributes), mei) {
				t.Error("MEI attribute must not be flagged skipGlobalAttributes")
			}
			if im.WildcardPathFlagsSkip(ctx, nil, wild(im.WildcardSkipCustomElements), standard) {
				t.Error("the standard attribute of the standard-range cluster is not custom")
			}
		})
		t.Run("shares one state reference between local reads without a transaction", func(t *testing.T) {
			t.Skip("ProtocolServiceTest.ts:609 at b25ed960: the protocol state read with or without a transaction has no Go subject: a Go server answers each read from its own state, there is no transaction model")
		})
		t.Run("requires a transaction to read cluster state for a remote subject", func(t *testing.T) {
			t.Skip("ProtocolServiceTest.ts:630 at b25ed960: the protocol state read with or without a transaction has no Go subject: a Go server answers each read from its own state, there is no transaction model")
		})

		t.Run("all attribute TLV schemas can encode their values", func(t *testing.T) {
			p := quietSite(t).CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{kitLight(t, "light")}}})
			// A wildcard read of every attribute: each value the device holds
			// is encoded on the wire and decodes again.
			r, err := fabrictest.ReadWith(parityCtx(t), p, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range r.Statuses {
				if s.Status != im.StatusUnsupportedRead && s.Status != im.StatusUnsupportedAccess {
					t.Errorf("%v answered %v", s.Path, s.Status)
				}
			}
			if len(r.Attributes) == 0 {
				t.Fatal("no attribute encoded")
			}
		})
	})

	t.Run("command validation", func(t *testing.T) {
		t.Run("validates response with qualified conformance", func(t *testing.T) {
			p, ep := testDevice(t, func(req testRequest) testResponse {
				if req.Enable {
					return testResponse{Result: &req.Value}
				}
				return testResponse{}
			})
			res, err := fabrictest.Invoke(parityCtx(t), p, client.CommandAt(ep, testClusterID, 1), testRequestFields{Enable: true, Value: 5})
			if err != nil || !res.HasFields {
				t.Fatalf("invoke = %+v, %v; want a command response", res, err)
			}
		})

		t.Run("rejects request with constraint violation", func(t *testing.T) {
			// ProtocolServiceTest.ts:767-800 at b25ed960: value 99 against "max 10" is
			// answered CONSTRAINT_ERROR, not with a command response; the
			// handler never runs.
			var ran atomic.Bool
			p, ep := testDevice(t, func(req testRequest) testResponse {
				ran.Store(true)
				v := req.Value
				return testResponse{Result: &v}
			})
			res, err := fabrictest.Invoke(parityCtx(t), p, client.CommandAt(ep, testClusterID, 1), testRequestFields{Enable: true, Value: 99})
			var se *interaction.StatusError
			if !errors.As(err, &se) || se.Status != im.StatusConstraintError {
				t.Fatalf("invoke = %+v, %v; want CONSTRAINT_ERROR", res, err)
			}
			if ran.Load() {
				t.Fatal("the handler ran on a request outside its bound")
			}
		})

		t.Run("rejects response missing required field per qualified conformance", func(t *testing.T) {
			p, ep := testDevice(t, func(testRequest) testResponse { return testResponse{} })
			_, err := fabrictest.Invoke(parityCtx(t), p, client.CommandAt(ep, testClusterID, 1), testRequestFields{Enable: true, Value: 5})
			var se *interaction.StatusError
			if !errors.As(err, &se) || se.Status != im.StatusFailure {
				t.Fatalf("invoke = %v, want FAILURE", err)
			}
		})
	})
}

// effectSource is a bridged endpoint's source that is the host's
// IdentifyHandler: it records the effects TriggerEffect hands it.
type effectSource struct {
	contract.EndpointSource
	mu      sync.Mutex
	effects [][2]uint8
}

func (*effectSource) MatterIdentify(context.Context, uint16) error { return nil }

func (s *effectSource) MatterTriggerEffect(_ context.Context, effect, variant uint8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.effects = append(s.effects, [2]uint8{effect, variant})
	return nil
}

func (s *effectSource) recorded() [][2]uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.effects)
}

// identifyingEndpoint is inner with src as its source.
type identifyingEndpoint struct {
	inner node.Endpoint
	src   *effectSource
}

func (e identifyingEndpoint) Spec() endpoint.Spec {
	s := e.inner.Spec()
	e.src.EndpointSource = s.Source
	s.Source = e.src
	return s
}

// listOf is the unsigned entries of a list.
func listOf(n spec.Node) []uint64 {
	out := make([]uint64, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c.El.Uint)
	}
	return out
}

func mapsEqual(a, b map[uint32]uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func drain(ch chan *interaction.Report) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
