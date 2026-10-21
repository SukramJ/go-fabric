#!/usr/bin/env python3
"""BX3 sweep 2a: log event names in dotted lower case (G1-18, ADR 0026 P9),
and the tlv package-level logger. Idempotent; run from the repository root.
"""
import os
import subprocess


def rd(p):
    return open(p).read()


def sub(p, old, new):
    s = rd(p)
    if new in s:
        return
    assert s.count(old) == 1, f"{p}: {old!r} x{s.count(old)}"
    open(p, "w").write(s.replace(old, new))
    print(f"{p}: {old.strip()[:70]!r} -> {new.strip()[:70]!r}")


sub("im/subscription/manager.go", '"matter subscription added",', '"im.subscription.added",')
sub("im/subscription/persistence.go", '"matter subscription restored",', '"im.subscription.restored",')
sub("endpoint/assembler.go", '"matter endpoint gc skipped: model incomplete",', '"endpoint.gc_skipped",')
sub("endpoint/assembler.go", '''			a.logger.Debug(
				"matter endpoint gc",''', '''			a.logger.Debug(
				"endpoint.gc",''')
# the skipped line named its reason in the message; keep it as an attribute
sub("endpoint/assembler.go", '''				"endpoint.gc_skipped",
				slog.String("scope", snap.Scope),
''', '''				"endpoint.gc_skipped",
				slog.String("scope", snap.Scope),
				slog.String("reason", "model incomplete"),
''')

# matter.js log text kept verbatim (the parity tests count it), as an attribute
sub("cluster/core/general_diagnostics.go",
    '''		logger.Info("The DataModelTest feature is enabled but is only required when maxPathsPerInvoke is greater than 1; disable it with GeneralDiagnosticsFeatures if you do not want to advertise it")''',
    '''		logger.Info("matter.general_diagnostics.data_model_test_optional",
			slog.String("reason", "The DataModelTest feature is enabled but is only required when maxPathsPerInvoke is greater than 1; disable it with GeneralDiagnosticsFeatures if you do not want to advertise it"))''')

# tlv: a replaceable package logger, dotted names
p = "tlv/encode.go"
s = rd(p)
if "var Logger" not in s:
    s = s.replace('''		slog.Warn(
			"tlv: UTF-8 string exceeds max length; trimming",''', '''		Logger.Warn(
			"tlv.utf8_trimmed",''')
    s = s.replace('''	slog.Warn("tlv: PutUTF8Bounded: string truncated",''', '''	Logger.Warn("tlv.utf8_truncated",''')
    assert "slog.Warn(" not in s
    i = s.index("\nimport (")
    j = s.index("\n)\n", i) + 3
    s = s[:j] + '''
// Logger receives the codec's diagnostics (a string trimmed or truncated to
// its bound). A host replaces it to route them, once, before it encodes; the
// default is slog.Default().
var Logger = slog.Default()
''' + s[j:]
    open(p, "w").write(s)
    print("tlv/encode.go: slog.Warn -> Logger.Warn (tlv.utf8_trimmed, tlv.utf8_truncated)")

g = os.path.expanduser("~/go/bin/gofumpt")
if os.path.exists(g):
    subprocess.run([g, "-w", "im/subscription/manager.go", "im/subscription/persistence.go",
                    "endpoint/assembler.go", "cluster/core/general_diagnostics.go", "tlv/encode.go"])
print("logs: done")

# the test that read the old sentence as the record message reads the event name
sub("node/system_set_wiring_test.go",
    'log.contains("The DataModelTest feature is enabled but is only required")',
    'log.contains("matter.general_diagnostics.data_model_test_optional")')
