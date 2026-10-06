# SPDX-License-Identifier: MIT
# Copyright (C) 2026 SukramJ.
#
# Generates the reference daemon's per-endpoint PICS slices from the device
# itself: one wildcard read, then CHIP's own derivation helpers
# (matter/testing/pics.py generate_device_element_pics_from_device_wildcard,
# derive_base_pics_facts_from_device_wildcard) — the functions TC_pics_checker
# (TC-IDM-10.4) holds a PICS file against. Every server-side element code of
# every spec cluster is written explicitly, 1 when the endpoint has it and 0
# when it does not, so a slice overrides whatever CHIP's ci-pics-values says
# for its all-clusters app.
#
# Run inside the CHIP harness image against a commissioned DUT:
#   python3 gen_pics.py --string-arg out_dir:/path
# Writes <out_dir>/ep<N>.txt for every endpoint.
import math
import os

import matter.clusters as Clusters
from matter.clusters.Attribute import AsyncReadTransaction
from matter.testing.basic_composition import BasicCompositionTests
from matter.testing.decorators import async_test_body
from matter.testing.pics import (BASE_PICS_CODES_DERIVED, accepted_cmd_pics_str, attribute_pics_str, base_pics_facts_to_pics_codes,
                                 derive_base_pics_facts_from_device_wildcard, event_pics_str, feature_pics_str,
                                 generate_device_element_pics_from_device_wildcard, generated_cmd_pics_str, server_pics_str)
from matter.testing.runner import default_matter_test_main


class GenPICS(BasicCompositionTests):
    @async_test_body
    async def setup_class(self):
        super().setup_class()
        await self.setup_class_helper(False)
        self.build_spec_xmls()

    def test_generate(self):
        out_dir = self.user_params.get("out_dir")
        os.makedirs(out_dir, exist_ok=True)
        wildcard = AsyncReadTransaction.ReadResponse(attributes=self.endpoints, events=[], tlvAttributes=self.endpoints_tlv)
        present, _ = generate_device_element_pics_from_device_wildcard(wildcard, self.xml_clusters)
        facts, _ = derive_base_pics_facts_from_device_wildcard(wildcard, self.xml_clusters)
        base = base_pics_facts_to_pics_codes(facts)
        ota = {Clusters.OtaSoftwareUpdateProvider.id, Clusters.OtaSoftwareUpdateRequestor.id}
        for ep in sorted(self.endpoints_tlv):
            have = set(present.get(ep, []))
            codes = {}
            for cid, xc in sorted(self.xml_clusters.items()):
                if xc.pics is None or cid in ota or cid not in Clusters.ClusterObjects.ALL_CLUSTERS:
                    continue
                p = xc.pics
                codes[server_pics_str(p)] = server_pics_str(p) in have
                for aid in Clusters.ClusterObjects.ALL_ATTRIBUTES.get(cid, {}):
                    if aid < 0xF000:
                        codes[attribute_pics_str(p, aid)] = attribute_pics_str(p, aid) in have
                for cmd in Clusters.ClusterObjects.ALL_ACCEPTED_COMMANDS.get(cid, {}):
                    if cmd < 0xF000:
                        codes[accepted_cmd_pics_str(p, cmd)] = accepted_cmd_pics_str(p, cmd) in have
                for cmd in Clusters.ClusterObjects.ALL_GENERATED_COMMANDS.get(cid, {}):
                    if cmd < 0xF000:
                        codes[generated_cmd_pics_str(p, cmd)] = generated_cmd_pics_str(p, cmd) in have
                feats = getattr(getattr(Clusters.ClusterObjects.ALL_CLUSTERS[cid], "Bitmaps", None), "Feature", None)
                for mask in (feats or []):
                    if mask <= 0:
                        continue
                    bit = int(math.log2(mask))
                    codes[feature_pics_str(p, bit)] = feature_pics_str(p, bit) in have
                mandatory = facts.mandatory_events_by_cluster.get(ep, {}).get(cid, set())
                for eid in xc.events:
                    codes[event_pics_str(p, eid)] = eid in mandatory
            for code in BASE_PICS_CODES_DERIVED:
                codes[code] = ep == 0 and code in base
            with open(os.path.join(out_dir, f"ep{ep}.txt"), "w") as f:
                f.write(f"# Endpoint {ep}: generated from the device by internal/chiptool/testdata/gen_pics.py.\n")
                f.write("# Server-side element codes and the derivable MCORE codes only; do not edit.\n")
                for code in sorted(codes):
                    f.write(f"{code}={int(codes[code])}\n")


if __name__ == "__main__":
    default_matter_test_main()
