// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	cclight "github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	tstatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// handFormServer is a server of a cluster whose requests the bridge decodes
// into cluster/core's hand-written forms: only its id and attributes are
// read by the gate.
type handFormServer struct{ id uint32 }

func (s handFormServer) MatterClusterID() uint32                      { return s.id }
func (handFormServer) MatterRead(uint32) (any, bool)                  { return nil, false }
func (handFormServer) MatterWrite(context.Context, uint32, any) error { return nil }
func (handFormServer) MatterReportable() []uint32                     { return nil }
func (handFormServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	return nil, nil
}

// fanState is a FanControl host port that holds nothing.
type fanState struct{}

func (fanState) FanState() fandef.State                               { return fandef.State{} }
func (fanState) ApplyFanSettings(context.Context, fandef.Patch) error { return nil }

// TestRequestGateReachesEveryServer (decision 339): the one request gate
// sits in the dispatcher in front of every server a wire command reaches
// ([requestHeld], matter.js ProtocolService.ts:347-350, :671-705 at
// b25ed960), so the servers that ran no gate of their own before — the
// spec.HostHandlers-based self-held servers and the hand-written
// cluster/core servers — refuse a value an enum does not define with
// CONSTRAINT_ERROR, and log a misfit that only gets a warning, before
// their handler runs. judged states whether the gate judges the payload
// (refuses it or logs a warning): a request the bridge decodes into a
// cluster/core hand-written form is judged where the form's Go fields
// carry every field the definition states by name (GeneralCommissioning's
// SetRegulatoryConfig, OperationalCredentials' CertificateChainRequest),
// and not judged otherwise (Groups' AddGroup: GroupID, not GroupId) — its
// decoder held it to the definition already (bridge heldByDefinition).
func TestRequestGateReachesEveryServer(t *testing.T) {
	newOr := func(srv contract.ClusterServer, err error) contract.ClusterServer {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	thermostat := func() contract.ClusterServer {
		return newOr(thermo.New(thermo.ThermostatConfig{Features: thermo.ThermostatFeatureHEAT}))
	}
	covering := func() contract.ClusterServer {
		return newOr(cover.New(cover.Config{FeatureMap: uint32(wcdef.FeatureLift | wcdef.FeaturePositionAwareLift)}))
	}
	fanServer := func() contract.ClusterServer {
		return newOr(fan.NewServer(fan.Config{Features: fan.FeatureStep, Source: fanState{}}))
	}
	level := func() contract.ClusterServer {
		lvl := uint8(10)
		return newOr(levelcontrol.New(levelcontrol.HeldConfig{Features: levelcontrol.FeatureLighting, CurrentLevel: &lvl}))
	}
	color := func() contract.ClusterServer {
		return newOr(cclight.NewColorControl(cclight.ColorControlServerConfig{Features: cclight.ColorFeatureHueSaturation}))
	}
	icd := func() contract.ClusterServer {
		return newOr(mattercore.NewICDManagement(mattercore.ICDManagementConfig{}))
	}
	identify := func() contract.ClusterServer { return mattercore.NewIdentify() }
	hand := func(id uint32) func() contract.ClusterServer {
		return func() contract.ClusterServer { return handFormServer{id} }
	}

	for _, c := range []struct {
		name   string
		srv    func() contract.ClusterServer
		cmd    uint32
		fields any
		judged bool
		status im.StatusCode // the gate's answer; Success: warning only
	}{
		{"Thermostat SetpointRaiseLower, undefined Mode", thermostat, tstatdef.CmdSetpointRaiseLower, tstatdef.SetpointRaiseLowerRequest{Mode: 9}, true, im.StatusConstraintError},
		{"WindowCovering GoToLiftPercentage over 10000 (no enum field)", covering, wcdef.CmdGoToLiftPercentage, wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 10001}, true, im.StatusSuccess},
		{"FanControl Step, undefined Direction", fanServer, fandef.CmdStep, fandef.StepRequest{Direction: 9}, true, im.StatusConstraintError},
		{"LevelControl Move, undefined MoveMode", level, lvldef.CmdMove, lvldef.MoveRequest{MoveMode: 9, Rate: spec.NullOf[uint8]()}, true, im.StatusConstraintError},
		{"ColorControl MoveHue, undefined MoveMode", color, ccdef.CmdMoveHue, ccdef.MoveHueRequest{MoveMode: 9, Rate: 1}, true, im.StatusConstraintError},
		{"ICDManagement RegisterClient, undefined ClientType", icd, icddef.CmdRegisterClient, icddef.RegisterClientRequest{Key: make([]byte, 16), ClientType: 9}, true, im.StatusConstraintError},
		// TriggerEffect's two enum fields are relaxed (IdentifyServer.ts:162-166
		// at b25ed960): nothing the gate refuses or warns of.
		{"Identify TriggerEffect, undefined EffectIdentifier (relaxed)", identify, identifydef.CmdTriggerEffect, identifydef.TriggerEffectRequest{EffectIdentifier: 0x42}, false, im.StatusSuccess},
		// A request without fields is judged as {}: IdentifyTime is missing.
		{"Identify Identify, without fields", identify, identifydef.CmdIdentify, nil, true, im.StatusSuccess},
		// The hand forms: judged where the form's Go fields carry every
		// field the definition states (by name), not judged otherwise.
		{"GeneralCommissioning SetRegulatoryConfig, hand form", hand(gencommdef.ClusterID), gencommdef.CmdSetRegulatoryConfig, mattercore.SetRegulatoryConfigRequest{NewRegulatoryConfig: 9}, true, im.StatusSuccess},
		{"OperationalCredentials CertificateChainRequest, hand form", hand(opcredsdef.ClusterID), opcredsdef.CmdCertificateChainRequest, mattercore.CertificateChainRequest{CertificateType: 9}, true, im.StatusInvalidCommand},
		{"Groups AddGroup, hand form", hand(groupsdef.ClusterID), groupsdef.CmdAddGroup, mattercore.AddGroupRequest{GroupID: 0, GroupName: "g"}, false, im.StatusSuccess},
	} {
		t.Run(c.name, func(t *testing.T) {
			var logged bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
			defer slog.SetDefault(prev)

			srv := c.srv()
			err := requestHeld(context.Background(), srv, c.cmd, c.fields)
			if got := spec.StatusOf(err); got != c.status {
				t.Errorf("gate: %v (%v), want %v", got, err, c.status)
			}
			warned := strings.Contains(logged.String(), "request validation error")
			if judged := err != nil || warned; judged != c.judged {
				t.Errorf("judged %v, want %v (log %q)", judged, c.judged, logged.String())
			}
			if c.status == im.StatusSuccess {
				return
			}
			// Through the dispatcher: the refusal is the command's answer.
			d := gateDispatcher(t, store.PrivilegeAdminister, srv)
			path := im.ConcreteCommandPath{Endpoint: 2, HasEndpoint: true, Cluster: srv.MatterClusterID(), Command: c.cmd}
			if res := d.Invoke(caseCtx(false), path, c.fields); res.Status != c.status {
				t.Errorf("Invoke: %v, want %v", res.Status, c.status)
			}
			if res := d.InvokeAuthorized(caseCtx(false), im.ConcreteCommandPath{Cluster: path.Cluster, Command: c.cmd}, c.fields, nil); len(res) != 1 || res[0].Status != c.status {
				t.Errorf("InvokeAuthorized (wildcard, group): %+v, want one %v", res, c.status)
			}
		})
	}
}
