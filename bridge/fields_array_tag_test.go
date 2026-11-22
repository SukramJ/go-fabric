// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestCommandFieldsWithATaggedArrayElementAreInvalidCommand holds what the
// fuzz target FuzzGroupcastFieldsReader found: command fields whose array
// holds an element with a tag were read as a tree and written again for
// the hand-written reader, and the encoder panics for a context tag inside
// an array. Such fields are refused INVALID_COMMAND for their command, the
// container read through to its end — matter.js refuses the array when its
// schema decodes it (packages/types/src/tlv/TlvArray.ts:77-78 at db021da0,
// "Array element tags should be anonymous.") and answers a payload the
// schema does not decode InvalidCommand
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:549-563).
func TestCommandFieldsWithATaggedArrayElementAreInvalidCommand(t *testing.T) {
	t.Parallel()
	// The input the fuzzer wrote: a structure whose field 1 is an array
	// holding a two-octet unsigned integer under context tag 0x30, then a
	// field 2 and the structure's end.
	payload := []byte("\x15%\x00\x01\x016\x01%000\x18$\x020\x18")
	for cmd := range uint32(6) {
		dec := tlv.NewDecoder(payload)
		open, err := dec.Next()
		if err != nil {
			t.Fatal(err)
		}
		_, err = commandFieldsReader(im.ConcreteCommandPath{Cluster: mattercore.GroupcastClusterID, Command: cmd}, dec, open)
		if err == nil {
			t.Fatalf("command %d: the fields were accepted", cmd)
		}
		sc, ok := errors.AsType[im.StatusCodeError](err)
		if !ok || sc.MatterStatusCode() != im.StatusInvalidCommand {
			t.Fatalf("command %d: err = %v, want a refusal with INVALID_COMMAND", cmd, err)
		}
		c, ok := errors.AsType[im.FieldsContainerConsumed](err)
		if !ok || !c.FieldsContainerConsumed() {
			t.Fatalf("command %d: the refusal does not say the fields container was read through: %v", cmd, err)
		}
		if next, err := dec.Next(); err == nil {
			t.Fatalf("command %d: the stream holds %+v behind the fields container", cmd, next)
		}
	}
}
