// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The subscription re-establishment proof of phase 30 over the suite's
// interactive chip-tool; connectedhomeip at the harness pin (6170af84) is
// the authority on chip-tool's subscription client.

const clusterBooleanState = 0x0045

// testSubscriptionAcrossRestart proves subscription re-establishment
// against chip-tool (ADR 0008): the interactive chip-tool holds a
// subscription to the contact sensor (SubscribeAttribute, ReportCommand.h:
// 248-317, its options :299-312), the daemon
// restarts on its database and re-establishes it as the CASE initiator
// (bridge/subscription_reestablish.go), and a state change reaches
// chip-tool without a new SubscribeRequest — chip-tool's ReadClient takes
// the unsolicited priming report on its subscription id
// (src/app/ReadClient.cpp:595-625). chip-tool advertises its operational
// node (interactive --advertise-operational, default true,
// InteractiveCommands.h:33-46), which the daemon resolves. auto-resubscribe
// stays off, so a report after the restart can only come from the
// daemon's re-establishment. The negative leg (chip-tool itself
// restarted, InvalidSubscription) is not run: chip-tool keeps no
// subscription across its own restart.
func testSubscriptionAcrossRestart(t *testing.T, f *fixture) {
	t.Helper()
	contact, ok := f.endpointWith(dtContactSensor, clusterBooleanState)
	if !ok {
		t.Fatal("no endpoint of the Contact Sensor device type serves BooleanState")
	}
	current, isBool := f.read(t, "booleanstate", "state-value", contact).(bool)
	if !isBool {
		t.Fatalf("StateValue on endpoint %d is not a bool", contact)
	}
	f.mustRun(t, fmt.Sprintf("booleanstate subscribe state-value 0 30 %s %d --keepSubscriptions true --auto-resubscribe false", f.node(), contact))

	f.br = f.br.restart(t)
	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(f.br.snapshotStderr(), "subscription.reestablish") {
		if time.Now().After(deadline) {
			f.br.dump(t)
			t.Fatal("the restarted daemon logged no subscription re-establishment within 30 s")
		}
		time.Sleep(250 * time.Millisecond)
	}
	mark := len(f.s.output())
	start := time.Now()
	f.br.pipe(t, map[string]any{"Name": "SetBooleanState", "EndpointId": contact, "NewState": !current})
	want := regexp.MustCompile(`StateValue: ` + map[bool]string{true: "TRUE", false: "FALSE"}[!current])
	deadline = time.Now().Add(20 * time.Second)
	for !want.MatchString(f.s.output()[mark:]) {
		if time.Now().After(deadline) {
			f.br.dump(t)
			t.Fatalf("chip-tool printed no report of StateValue %v within 20 s of the change after the restart; chip-tool output since the restart:\n%s",
				!current, tail(f.s.output()[mark:], 60))
		}
		time.Sleep(250 * time.Millisecond)
	}
	logs := f.br.snapshotStderr()
	resubscribed := strings.Count(logs, "msg=matter.rx.im.subscribe ")
	resumed := !strings.Contains(logs, "matter.subscription.reestablish.none")
	t.Logf("chip-tool got the change %s after it; daemon log: re-established=%v, SubscribeRequests since the restart=%d",
		time.Since(start).Round(100*time.Millisecond), resumed, resubscribed)
	if !resumed || resubscribed != 0 {
		t.Errorf("the subscription was not resumed by the daemon: re-established=%v, new SubscribeRequests=%d", resumed, resubscribed)
	}
}

// endpointWith is the first endpoint of device type dt that serves
// cluster.
func (f *fixture) endpointWith(dt, cluster uint32) (uint16, bool) {
	for _, id := range f.sortedEndpoints() {
		if ep := f.topology[id]; hasDeviceType(ep, dt) && ep.has(cluster) {
			return id, true
		}
	}
	return 0, false
}
