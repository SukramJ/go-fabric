#!/usr/bin/env python3
"""BX3 sweep 2b, part 2 (owner decisions after the review of #202):

  * GeneralDiagnostics and Zeroconf have no config struct, so they get
    SetLogger (the precedent is DiagnosticLogs.SetLogger; nil is
    slog.Default()), stored in an atomic pointer and read through a nil-safe
    accessor;
  * CaseAdapter's two hex dumps of the first 128 Sigma2 wire bytes
    (matter.tx.sigma2resume.wire, matter.tx.sigma2.wire) are removed, with
    their helper peekBytes and its tests: a wire dump belongs in a test, not
    in the library (the G1-17 case).

Idempotent; run from the repository root.
"""
import os
import re
import subprocess


def rd(p):
    return open(p).read()


def wr(p, s):
    open(p, "w").write(s)


def add_import(p, name):
    s = rd(p)
    if f'\t"{name}"\n' not in s:
        wr(p, s.replace("import (\n", f'import (\n\t"{name}"\n', 1))


def add_field(p, header, text):
    s = rd(p)
    if text in s:
        return
    assert s.count(header) == 1, (p, header)
    wr(p, s.replace(header, header + text, 1))


def append(p, marker, text):
    s = rd(p)
    if marker in s:
        return
    wr(p, s.rstrip("\n") + "\n" + text)


def set_logger(p, recv, typ, header, field_comment, doc_noun):
    add_field(p, header, field_comment + "\tlog atomic.Pointer[slog.Logger]\n")
    append(p, f"func ({recv} *{typ}) SetLogger(", f"""
// SetLogger sets the logger the {doc_noun} diagnostics go to; nil is
// slog.Default(). It is safe to call at any time.
func ({recv} *{typ}) SetLogger(l *slog.Logger) {{ {recv}.log.Store(l) }}

// logger is the {typ}'s logger, slog.Default() until SetLogger gives it one.
func ({recv} *{typ}) logger() *slog.Logger {{
	if l := {recv}.log.Load(); l != nil {{
		return l
	}}
	return slog.Default()
}}
""")
    add_import(p, "sync/atomic")
    add_import(p, "log/slog")


CALL = re.compile(r"slog\.Default\(\)\.(Debug|Info|Warn|Error)\(")

# ---- GeneralDiagnostics -------------------------------------------------
p = "cluster/core/general_diagnostics.go"
s = rd(p)
a = s.index("func (g *GeneralDiagnostics) EmitBootReason() {")
b = s.index("\n}\n", a) + 3
wr(p, s[:a] + CALL.sub(r"g.logger().\1(", s[a:b]) + s[b:])
set_logger(p, "g", "GeneralDiagnostics", "type GeneralDiagnostics struct {\n\tmu sync.RWMutex\n",
           "\n\t// log is the server's logger, set by [GeneralDiagnostics.SetLogger].\n", "server's")

# ---- Zeroconf --------------------------------------------------------------
p = "mdns/zeroconf.go"
s = rd(p)
wr(p, CALL.sub(r"z.logger().\1(", s))
set_logger(p, "z", "Zeroconf", "type Zeroconf struct {\n",
           "\t// log is the advertiser's logger, set by [Zeroconf.SetLogger].\n", "advertiser's")

# ---- CaseAdapter: no wire dump in the library -------------------------------
p = "bridge/handlers.go"
s = rd(p)
d1 = '''		slog.Default().Debug("matter.tx.sigma2resume.wire",
			slog.Int("len", len(out)),
			slog.String("hex_first128", hex.EncodeToString(peekBytes(out, 128))))
'''
d2 = '''	slog.Default().Debug("matter.tx.sigma2.wire",
		slog.Int("len", len(out)),
		slog.String("hex_first128", hex.EncodeToString(peekBytes(out, 128))))
'''
h = '''func peekBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}

'''
for blk in (d1, d2, h):
    s = s.replace(blk, "")
if "hex." not in s.replace('"encoding/hex"', ""):
    s = s.replace('\t"encoding/hex"\n', "")
if "slog." not in s.replace('"log/slog"', ""):
    s = s.replace('\t"log/slog"\n', "")
wr(p, s)
p = "bridge/securechannel_noop_test.go"
s = rd(p)
a = s.find("// ─── peekBytes")
if a >= 0:
    b = s.index("// ─── resolveCaseHandler")
    s = s[:a] + s[b:]
    s = s.replace("noopCaseHandler, peekBytes, and", "noopCaseHandler, and")
    wr(p, s)

g = os.path.expanduser("~/go/bin/gofumpt")
if os.path.exists(g):
    subprocess.run([g, "-w", "cluster/core/general_diagnostics.go", "mdns/zeroconf.go",
                    "bridge/handlers.go", "bridge/securechannel_noop_test.go"])
print("setlogger: done")
