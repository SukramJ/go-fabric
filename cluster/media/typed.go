// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import "github.com/SukramJ/go-fabric/cluster/spec"

// typedServer is the typed server's four optional interfaces: every media
// server embeds the generated store ([spec.Server]), which answers
// Handle, Emit and SetBeforeWrite; a server with state answers
// SetAttributes itself, so the host's typed Set reaches the state its
// commands are handed ([stateful.setAttributes]).
type typedServer interface {
	spec.AttributeSetter
	spec.CommandRegistrar
	spec.EventEmitting
	spec.BeforeWriteSetter
}

var (
	_ typedServer = (*PlaybackServer)(nil)
	_ typedServer = (*KeypadServer)(nil)
	_ typedServer = (*ContentLauncherServer)(nil)
	_ typedServer = (*AppLauncherServer)(nil)
	_ typedServer = (*AppBasicServer)(nil)
	_ typedServer = (*AccountLoginServer)(nil)
	_ typedServer = (*InputsServer)(nil)
	_ typedServer = (*OutputsServer)(nil)
	_ typedServer = (*ChannelsServer)(nil)
	_ typedServer = (*TargetsServer)(nil)
	_ typedServer = (*LowPowerServer)(nil)
	_ typedServer = (*WakeOnLanServer)(nil)
	_ typedServer = (*ContentControlServer)(nil)
	_ typedServer = (*ContentAppObserverServer)(nil)
)
