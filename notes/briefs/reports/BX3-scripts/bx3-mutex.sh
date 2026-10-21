#!/bin/bash
# BX3 sweep 3: mutex comments. Run from the repository root; add -apply to write.
# The skip list is BX2's files (they get their comments there), the security
# paths (strings only for BX3), and the non-library trees.
exec go run notes/briefs/reports/BX3-scripts/bx3-mutex.go "$@" \
  -skip cluster/core/messages.go,cluster/cover/windowcovering_server.go,cluster/energy/devicemanagement.go,cluster/appliance/microwave.go,cluster/measurement/soil.go,bdx/sink.go,secure/,commissioning/,controller/exchange/manager.go,transport/tcp/conn.go,bridge/securechannel.go,cluster/core/identify.go,cluster/lock/,node/ota.go,cluster/core/diagnostic_logs.go,bridge/bdx_responder.go,fabrictest/,cluster/core/access_control.go,cluster/core/operational_credentials.go,cluster/core/group_key_management.go,cluster/core/general_commissioning.go,examples,script,internal \
  .
