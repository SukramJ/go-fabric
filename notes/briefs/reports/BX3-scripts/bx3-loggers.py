#!/usr/bin/env python3
"""BX3 sweep 2b: inline slog.Default().<Level>(...) calls go through the type's
logger (G1-18, ADR 0026 P9). Idempotent; run from the repository root, then
gofumpt -w on the touched files (the script runs `gofumpt -w` itself when it
is on PATH).

Types that get a `Logger *slog.Logger` config field (nil = slog.Default(),
resolved at construction) and a `log` field:
  cluster/core BasicInformation (Config), BridgedDeviceBasicInformation
  (BridgedConfig), cluster/thermo ThermostatServer (ThermostatConfig),
  cluster/cover WindowCoveringServer (Config), cluster/light
  ColorControlServer (ColorControlServerConfig), transport/udp Listener
  (Config), mdns Operational/CommissionableServiceConfig.

Left alone (no config struct to carry the field, or a security path where
only strings change): see notes/briefs/reports/BX3.md.
"""
import os
import re
import subprocess
import sys

GOFUMPT = os.path.expanduser("~/go/bin/gofumpt")


def rd(p):
    return open(p).read()


def wr(p, s):
    open(p, "w").write(s)


def sub(p, old, new, count=1):
    s = rd(p)
    if new in s:
        return  # an earlier run did this
    assert s.count(old) == count, f"{p}: expected {count}x {old!r}, found {s.count(old)}"
    wr(p, s.replace(old, new))


CALL_RE = re.compile(r"slog\.Default\(\)\.(Debug|Info|Warn|Error)\(")


def in_func(p, sig, old, new):
    """Replace old by new inside the function whose line starts with sig."""
    s = rd(p)
    if ("\n" + sig) not in s:
        return  # signature already rewritten: an earlier run did this
    a = s.index("\n" + sig) + 1
    b = s.index("\n}\n", a) + 3
    body = s[a:b]
    if old not in body:
        return
    body = re.sub(re.escape(old) + r"(Debug|Info|Warn|Error)\(", lambda m: new + m.group(1) + "(", body)
    wr(p, s[:a] + body + s[b:])


def ctor(p, header):
    """Add the log field to the struct literal that starts with header."""
    s = rd(p)
    if re.search(r"\tlog:\s+cmp\.Or\(cfg\.Logger", s):
        return
    assert s.count(header) == 1, (p, header)
    wr(p, s.replace(header, header + "\t\tlog: " + RES + ",\n"))


def add_import(p, name):
    s = rd(p)
    if f'\t"{name}"\n' in s:
        return
    s = s.replace("import (\n", f'import (\n\t"{name}"\n', 1)
    wr(p, s)


def struct_prepend(p, header, text):
    s = rd(p)
    assert s.count(header) == 1, (p, header)
    if text in s[s.index(header):s.index(header) + len(header) + len(text) + 4]:
        return
    wr(p, s.replace(header, header + text, 1))


def struct_append(p, header, text):
    s = rd(p)
    a = s.index(header)
    b = s.index("\n}\n", a)
    if text.rstrip("\n") in s[a:b]:
        return
    wr(p, s[:b] + "\n" + text.rstrip("\n") + s[b:])


def append_accessor(p, recv, typ, expr):
    s = rd(p)
    marker = f"func ({recv} *{typ}) logger() *slog.Logger"
    if marker in s:
        return
    s = s.rstrip("\n") + f"""

// logger is the {typ}'s logger; a value built without its constructor
// logs through slog.Default().
{marker} {{
	if {expr} != nil {{
		return {expr}
	}}
	return slog.Default()
}}
"""
    wr(p, s)


CFG_DOC = "\t// Logger receives the %s's diagnostics; nil is slog.Default().\n\tLogger *slog.Logger\n"
FIELD_DOC = "\t// log is the %s's logger ([%s.Logger]; slog.Default() when nil).\n\tlog *slog.Logger\n"
RES = "cmp.Or(cfg.Logger, slog.Default())"

# ---- cluster/core BasicInformation -----------------------------------
p = "cluster/core/basic_information.go"
in_func(p, "func validateBasicInfoAttributes(cfg Config) {", "slog.Default().", "log.")
sub(p, "func validateBasicInfoAttributes(cfg Config) {", "func validateBasicInfoAttributes(cfg Config, log *slog.Logger) {")
sub(p, "\tvalidateBasicInfoAttributes(cfg)\n", "\tvalidateBasicInfoAttributes(cfg, bi.log)\n")
in_func(p, "func (b *BasicInformation) EmitStartUp() {", "slog.Default().", "b.logger().")
struct_append(p, "type Config struct {", CFG_DOC % "cluster")
struct_prepend(p, "type BasicInformation struct {\n\tmu sync.RWMutex\n", "\n" + FIELD_DOC % ("cluster", "Config"))
ctor(p, "\tbi := &BasicInformation{\n")
add_import(p, "cmp")
append_accessor(p, "b", "BasicInformation", "b.log")

# ---- cluster/core BridgedDeviceBasicInformation ------------------------
p = "cluster/core/bridged_device_basic_information.go"
in_func(p, "func validateBridgedBasicInfoAttributes(cfg BridgedConfig, resolvedSerial string) {", "slog.Default().", "log.")
sub(p, "func validateBridgedBasicInfoAttributes(cfg BridgedConfig, resolvedSerial string) {",
    "func validateBridgedBasicInfoAttributes(cfg BridgedConfig, resolvedSerial string, log *slog.Logger) {")
sub(p, "\tvalidateBridgedBasicInfoAttributes(cfg, serialNumber)\n", "\tvalidateBridgedBasicInfoAttributes(cfg, serialNumber, b.log)\n")
struct_append(p, "type BridgedConfig struct {", CFG_DOC % "cluster")
struct_prepend(p, "type BridgedDeviceBasicInformation struct {\n", FIELD_DOC % ("cluster", "BridgedConfig"))
ctor(p, "\tb := &BridgedDeviceBasicInformation{\n")
add_import(p, "cmp")
append_accessor(p, "b", "BridgedDeviceBasicInformation", "b.log")

# ---- cluster/thermo ThermostatServer -----------------------------------
for p in ("cluster/thermo/thermostat_server.go", "cluster/thermo/presets.go"):
    s = rd(p)
    wr(p, CALL_RE.sub(r"s.logger().\1(", s))
p = "cluster/thermo/atomic.go"
in_func(p, "func (w *atomicWrites) closeFabric(fabricIndex uint8) {", "slog.Default().", "log.")
sub(p, "func (w *atomicWrites) closeFabric(fabricIndex uint8) {", "func (w *atomicWrites) closeFabric(fabricIndex uint8, log *slog.Logger) {")
sub(p, "s.atomic.closeFabric(fabricIndex)", "s.atomic.closeFabric(fabricIndex, s.logger())")
in_func(p, "func statusOf(err error, step string) im.StatusCode {", "slog.Default().", "log.")
sub(p, "func statusOf(err error, step string) im.StatusCode {", "func statusOf(err error, step string, log *slog.Logger) im.StatusCode {")
sub(p, 'statusOf(err, "committing attribute Presets")', 'statusOf(err, "committing attribute Presets", s.logger())')
p = "cluster/thermo/thermostat_server.go"
struct_append(p, "type ThermostatConfig struct {", CFG_DOC % "server")
struct_prepend(p, "type ThermostatServer struct {\n", FIELD_DOC % ("server", "ThermostatConfig"))
ctor(p, "\ts := &ThermostatServer{\n")
add_import(p, "cmp")
append_accessor(p, "s", "ThermostatServer", "s.log")

# ---- cluster/cover WindowCoveringServer --------------------------------
p = "cluster/cover/windowcovering_server.go"
s = rd(p)
wr(p, CALL_RE.sub(r"s.logger().\1(", s))
struct_append(p, "type Config struct {", CFG_DOC % "server")
struct_prepend(p, "type WindowCoveringServer struct {\n", FIELD_DOC % ("server", "Config"))
ctor(p, "\ts := &WindowCoveringServer{\n")
add_import(p, "cmp")
append_accessor(p, "s", "WindowCoveringServer", "s.log")

# ---- cluster/light ColorControlServer ----------------------------------
p = "cluster/light/colorcontrol_set.go"
s = rd(p)
wr(p, CALL_RE.sub(r"s.logger().\1(", s))
p = "cluster/light/colorcontrol_server.go"
struct_append(p, "type ColorControlServerConfig struct {", CFG_DOC % "server")
append_accessor(p, "s", "ColorControlServer", "s.cfg.Logger")
add_import(p, "log/slog")

# ---- transport/udp Listener ---------------------------------------------
p = "transport/udp/listener.go"
in_func(p, "func (l *Listener) safeDispatch(", "slog.Default().", "l.logger().")
struct_append(p, "type Config struct {", CFG_DOC % "listener")
sub(p, "\treturn newListener(conn), nil\n", "\tl := newListener(conn)\n\tl.log = " + RES + "\n\treturn l, nil\n")
struct_prepend(p, "type Listener struct {\n", FIELD_DOC % ("listener", "Config"))
append_accessor(p, "l", "Listener", "l.log")
add_import(p, "cmp")

# ---- mdns service builders ------------------------------------------------
p = "mdns/service.go"
in_func(p, "func capAdvertisedIntervals(sii, sai, sat uint32) (cappedSII, cappedSAI, cappedSAT uint32) {", "slog.Default().", "log.")
sub(p, "func capAdvertisedIntervals(sii, sai, sat uint32) (", "func capAdvertisedIntervals(log *slog.Logger, sii, sai, sat uint32) (")
sub(p, "sii, sai, sat = capAdvertisedIntervals(sii, sai, sat)", "sii, sai, sat = capAdvertisedIntervals(" + RES + ", sii, sai, sat)", count=2)
struct_append(p, "type OperationalServiceConfig struct {", CFG_DOC % "builder")
struct_append(p, "type CommissionableServiceConfig struct {", CFG_DOC % "builder")
add_import(p, "cmp")

if os.path.exists(GOFUMPT):
    subprocess.run([GOFUMPT, "-w"] + [
        "cluster/core/basic_information.go", "cluster/core/bridged_device_basic_information.go",
        "cluster/thermo/thermostat_server.go", "cluster/thermo/presets.go", "cluster/thermo/atomic.go",
        "cluster/cover/windowcovering_server.go", "cluster/light/colorcontrol_set.go",
        "cluster/light/colorcontrol_server.go", "transport/udp/listener.go", "mdns/service.go"])
print("loggers: done")

# the udp test swapped slog.Default() after building the listener; the listener
# now keeps the logger it was built with, so the test hands it one
p = "transport/udp/listener_test.go"
old = '''	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &logMu, w: &logBuf}, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(prev) })
'''
new = '''	// The listener logs through the logger it was built with (Config.Logger,
	// slog.Default() at construction), so the test hands it one.
	a.log = slog.New(slog.NewTextHandler(&lockedWriter{mu: &logMu, w: &logBuf}, &slog.HandlerOptions{Level: slog.LevelError}))
'''
if old in rd(p):
    wr(p, rd(p).replace(old, new))
