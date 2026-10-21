#!/usr/bin/env python3
"""BX3 sweep 2b, part 3 (owner decision): node.Config.Logger (and the bridge's
own logger) reach the components the loggers sweep gave a Logger. Idempotent;
run from the repository root, after bx3-loggers.py and bx3-setlogger.py.

  node/root.go         core.Config.Logger and GeneralDiagnostics.SetLogger
  node/node.go         rootDeps.logger
  node/mdns.go         Zeroconf.SetLogger
  node/groupsend.go    udp.Config.Logger
  node/client.go       udp.Config.Logger
  bridge/tcp_listen.go udp.Config.Logger
  bridge/bridge.go     mdns.OperationalServiceConfig / CommissionableServiceConfig .Logger

The thermostat, window-covering and colour-control servers are built by the
host, not by node/ or bridge/; the host sets their Logger. endpoint.ClusterServers
builds BridgedConfig from a free function with no logger.
"""
import os
import re
import subprocess


def sub(p, old, new, count=1):
    s = open(p).read()
    if new in s:
        return
    assert s.count(old) == count, (p, old, s.count(old))
    open(p, "w").write(s.replace(old, new))


sub("node/root.go", "type rootDeps struct {\n\tcfg               *Config\n",
    "type rootDeps struct {\n\tcfg               *Config\n\tlogger            *slog.Logger\n")
sub("node/root.go", "\t\tMaxPathsPerInvoke:  cfg.MaxPathsPerInvoke,\n\t})",
    "\t\tMaxPathsPerInvoke:  cfg.MaxPathsPerInvoke,\n\t\tLogger:             d.logger,\n\t})")
sub("node/root.go", "\tr.GeneralDiagnostics.SetClock(d.cfg.Clock)\n",
    "\tr.GeneralDiagnostics.SetClock(d.cfg.Clock)\n\tr.GeneralDiagnostics.SetLogger(d.logger)\n")
sub("node/root.go", 'import (\n\t"context"\n', 'import (\n\t"context"\n\t"log/slog"\n')
sub("node/node.go", "buildRoot(ctx, rootDeps{\n\t\tcfg: &n.cfg,", "buildRoot(ctx, rootDeps{\n\t\tcfg: &n.cfg, logger: n.logger,")
sub("node/mdns.go", "\tzc := mdns.NewZeroconf()\n", "\tzc := mdns.NewZeroconf()\n\tzc.SetLogger(n.logger)\n")
sub("node/groupsend.go", "udp.Config{LocalAddr: groupSendAddr}", "udp.Config{LocalAddr: groupSendAddr, Logger: n.logger}")
sub("node/client.go", "udp.Config{LocalAddr: listen, PreferIPv4: n.cfg.Network.PreferIPv4}",
    "udp.Config{LocalAddr: listen, PreferIPv4: n.cfg.Network.PreferIPv4, Logger: n.logger}")
sub("bridge/tcp_listen.go", "udp.Config{LocalAddr: b.cfg.Listen, PreferIPv4: b.cfg.PreferIPv4}",
    "udp.Config{LocalAddr: b.cfg.Listen, PreferIPv4: b.cfg.PreferIPv4, Logger: b.logger}")
s = open("bridge/bridge.go").read()
for cfg in ("OperationalServiceConfig", "CommissionableServiceConfig"):
    head = "mdns.Build%sService(mdns.%s{\n" % ("Operational" if cfg.startswith("Op") else "Commissionable", cfg)
    new = head + "\t\tLogger: b.logger,\n"
    # gofumpt aligns the value, so test the site, not the literal text
    done = re.search(re.escape(head) + r"\t\tLogger:\s+b\.logger,\n", s)
    if not done:
        s = s.replace(head, new)
open("bridge/bridge.go", "w").write(s)

g = os.path.expanduser("~/go/bin/gofumpt")
if os.path.exists(g):
    subprocess.run([g, "-w", "node/root.go", "node/node.go", "node/mdns.go", "node/groupsend.go",
                    "node/client.go", "bridge/tcp_listen.go", "bridge/bridge.go"])
print("wire-logger: done")
