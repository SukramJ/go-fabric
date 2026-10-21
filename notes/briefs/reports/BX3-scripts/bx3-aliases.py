#!/usr/bin/env python3
"""BX3 sweep 4: owned aliases in node/facade.go for the host-port types a
node.Config field names from a lower package (G1-6, ADR 0025 P3), and the
node signatures spell them. Idempotent; run from the repository root.
"""
import glob
import os
import re
import subprocess

FACADE = "node/facade.go"

# (alias, original, doc)
ALIASES = [
    ("OTAImageSource", "mattercore.OTAImageSource", "is the type of [OTAProviderConfig].ImageSource: where the provider reads the images it offers"),
    ("OTAConsent", "mattercore.OTAConsent", "is the type of [OTAProviderConfig].Consent: the user-consent port of the provider"),
    ("OTAConsentState", "mattercore.OTAConsentState", "is what an [OTAConsent] answers"),
    ("OTAApplyPolicy", "mattercore.OTAApplyPolicy", "is the type of [OTAProviderConfig].ApplyPolicy: when a requestor may apply a downloaded image"),
    ("OTAPeer", "mattercore.OTAPeer", "names the requestor an [OTAProviderConfig] callback is about"),
    ("OTAUpdateStatus", "mattercore.OTAUpdateStatus", "is the status [OTAProviderConfig].OnStatusChange reports for a requestor"),
    ("OTAApplier", "mattercore.OtaApplier", "is the type of [OtaConfig].Applier: the requestor's consent and apply hooks"),
    ("WiFiReporter", "mattercore.WiFiReporter", "is the type of [WiFi].Reporter: the live Wi-Fi diagnostics the host reports"),
    ("CommissioningApprover", "mattercore.CommissioningApprover", "is the type of [CommissionerControlConfig].Approver: the host's decision on a commissioning request"),
    ("ReverseCommissioner", "mattercore.ReverseCommissioner", "is the type of [CommissionerControlConfig].Commissioner: the host's commissioner of the requesting node"),
    ("CapabilityMinimaStruct", "mattercore.CapabilityMinimaStruct", "is the type of [Config].CapabilityMinima: the BasicInformation CapabilityMinima"),
    ("GeneralDiagnosticsFeatures", "mattercore.GeneralDiagnosticsFeatures", "is the type of [Config].GeneralDiagnosticsFeatures: the feature selection of the root GeneralDiagnostics"),
    ("DeviceTypeValidationMode", "endpoint.DeviceTypeValidationMode", "is the type of [Config].DeviceTypeValidation: how the node treats a device type that breaks its conformance"),
    ("WindowStatus", "wire.WindowStatusSnapshot", "is the AdministratorCommissioning view [WindowSnapshot].Administrator carries"),
    ("AttestationChain", "attestation.Chain", "is the type of [Attestation].Chain: the DAC, its key and the PAI"),
]

BEGIN = "// Host ports and values named by the Config fields (G1-6): the same types as"
src = open(FACADE).read()
if BEGIN not in src:
    block = [BEGIN, "// the lower packages' own, owned here so `go doc` shows node.X and a host", "// implements node.OTAImageSource without importing cluster/core.", ""]
    for name, orig, doc in ALIASES:
        block.append(f"// {name} {doc} ([{orig}]).")
        block.append(f"type {name} = {orig}")
        block.append("")
    src = src.rstrip("\n") + "\n\n" + "\n".join(block).rstrip("\n") + "\n"
    for imp in ('\t"github.com/SukramJ/go-fabric/cluster/wire"\n', '\t"github.com/SukramJ/go-fabric/secure/attestation"\n'):
        if imp not in src:
            src = src.replace('\tmattercore "github.com/SukramJ/go-fabric/cluster/core"\n', '\tmattercore "github.com/SukramJ/go-fabric/cluster/core"\n' + imp, 1) if "cluster/wire" in imp else src.replace('\t"github.com/SukramJ/go-fabric/im/subscription"\n', '\t"github.com/SukramJ/go-fabric/im/subscription"\n' + imp, 1)
    open(FACADE, "w").write(src)
    print("facade.go: %d aliases" % len(ALIASES))

changed = []
# the alias spells OTA one way; the original type keeps its name
mc = "|".join(n for n, o, _ in ALIASES if o.startswith("mattercore.") and n != "OTAApplier")
rules = [
    (re.compile(r"\bmattercore\.OtaApplier\b"), "OTAApplier"),
    (re.compile(r"\bmattercore\.(" + mc + r")\b"), r"\1"),
    (re.compile(r"\battestation\.Chain\b"), "AttestationChain"),
    (re.compile(r"\bwire\.WindowStatusSnapshot\b"), "WindowStatus"),
    (re.compile(r"\bendpoint\.DeviceTypeValidationMode\b"), "DeviceTypeValidationMode"),
    (re.compile(r"\bendpoint\.SourceKey\b"), "device.SourceKey"),
]
n = 0
for path in sorted(glob.glob("node/*.go")):
    if path.endswith("_test.go") or path == FACADE:
        continue
    s = open(path).read()
    t = s
    for rx, rep in rules:
        # node/root.go keeps *attestation.Chain in the unexported
        # attestationMaterial: gocritic's unnamedResult flags the alias there
        if path == "node/root.go" and rep == "AttestationChain":
            continue
        t = rx.sub(rep, t)
    if t != s:
        cnt = sum(len(rx.findall(s)) for rx, _ in rules)
        print(f"{path}: {cnt} references")
        n += cnt
        open(path, "w").write(t)
        changed.append(path)
print("aliases:", n, "references rewritten")

gi = os.path.expanduser("~/go/bin/goimports")
if changed and os.path.exists(gi):
    subprocess.run([gi, "-w"] + changed + [FACADE])
g = os.path.expanduser("~/go/bin/gofumpt")
if os.path.exists(g):
    subprocess.run([g, "-w"] + changed + [FACADE])
