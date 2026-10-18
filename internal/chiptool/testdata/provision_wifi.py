# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# Puts a Wi-Fi network on the commissioned reference daemon (--wifi-sim) as a
# TH commissioning it over BLE-Wi-Fi would have: ArmFailSafe,
# AddOrUpdateWiFiNetwork, ConnectNetwork, CommissioningComplete, then the
# network reads back connected. The CNETWIFI run's py snapshot commissions
# over IP, where chip configures no network (src/controller/
# AutoCommissioner.cpp:780-797 at 6170af84), so the harness runs this on the
# snapshot (chipcases_test.go provisionWiFiNetwork).
#
# Run inside the CHIP harness image in the snapshot's directory:
#   python3 provision_wifi.py --wifi-ssid <ssid> --wifi-passphrase <passphrase>
import logging

from mobly import asserts

import matter.clusters as Clusters
from matter.testing.decorators import async_test_body
from matter.testing.matter_testing import MatterTestCommissionedDevice
from matter.testing.runner import default_matter_test_main

log = logging.getLogger(__name__)


class ProvisionWiFi(MatterTestCommissionedDevice):

    @async_test_body
    async def test_TC_GOFABRIC_WIFI_1_1(self):
        ssid = self.matter_test_config.wifi_ssid.encode("utf-8")
        passphrase = self.matter_test_config.wifi_passphrase.encode("utf-8")
        gc = Clusters.GeneralCommissioning
        cnet = Clusters.NetworkCommissioning

        rsp = await self.send_single_cmd(cmd=gc.Commands.ArmFailSafe(expiryLengthSeconds=120, breadcrumb=1), endpoint=0)
        asserts.assert_equal(rsp.errorCode, gc.Enums.CommissioningErrorEnum.kOk, "ArmFailSafe")
        rsp = await self.send_single_cmd(cmd=cnet.Commands.AddOrUpdateWiFiNetwork(ssid=ssid, credentials=passphrase, breadcrumb=2), endpoint=0)
        asserts.assert_equal(rsp.networkingStatus, cnet.Enums.NetworkCommissioningStatusEnum.kSuccess, "AddOrUpdateWiFiNetwork")
        rsp = await self.send_single_cmd(cmd=cnet.Commands.ConnectNetwork(networkID=ssid, breadcrumb=3), endpoint=0)
        asserts.assert_equal(rsp.networkingStatus, cnet.Enums.NetworkCommissioningStatusEnum.kSuccess, "ConnectNetwork")
        rsp = await self.send_single_cmd(cmd=gc.Commands.CommissioningComplete(), endpoint=0)
        asserts.assert_equal(rsp.errorCode, gc.Enums.CommissioningErrorEnum.kOk, "CommissioningComplete")

        networks = await self.read_single_attribute_check_success(cluster=cnet, attribute=cnet.Attributes.Networks, endpoint=0)
        asserts.assert_true(any(n.networkID == ssid and n.connected for n in networks), f"Networks after provisioning: {networks}")
        log.info("provisioned Wi-Fi network %s", ssid)


if __name__ == "__main__":
    default_matter_test_main()
