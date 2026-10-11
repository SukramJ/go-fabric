// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/endpoint"
)

// The app-pipe commands of the soil sensor and the node clusters, kept
// out of control.go's dispatch table until that table takes them. The
// CHIP Actions cases (Test_TC_ACT_2_1/2_2/3_2.yaml at the harness pin
// 6170af84) are manual end to end — every step is `disabled: true` — so
// they drive no pipe command; the soil cases do.

// soilKey is the soil sensor's StableKey (fleet.go endpoints).
const soilKey = endpoint.StringKey("demo:soil:1")

// errWrongSoilEndpoint: SetSimulatedSoilMoisture named an endpoint that
// is not the soil sensor's.
var errWrongSoilEndpoint = errors.New("not the soil sensor's endpoint")

// setSimulatedSoilMoisture applies CHIP's SetSimulatedSoilMoisture
// ({"Name": "SetSimulatedSoilMoisture", "SoilMoistureValue": <uint8 or
// null>, "EndpointId": <id>}, TC_SOIL_2_2.py:97, :109): the all-clusters
// app refuses any endpoint but its soil sensor's and takes a null value as
// null (examples/all-clusters-app/linux/AllClustersCommandDelegate.cpp
// :576-598), then sets the measured value through the cluster
// (OnSoilMoistureChange :1044-1056), which refuses a value outside the
// limits. Here the sensor takes the value and the SoilMeasurement server
// holds it to the limits on the next read.
//
// control.go's applyPipeCommand does not dispatch it yet: the owner adds
//
//	case "SetSimulatedSoilMoisture":
//		return f.setSimulatedSoilMoisture(cmd.EndpointID, cmd.SoilMoistureValue)
//
// with a SoilMoistureValue *uint8 field (JSON "SoilMoistureValue") on
// pipeCommand.
func (f *fleet) setSimulatedSoilMoisture(endpointID uint16, value *uint8) error {
	if f.endpointOf != nil {
		if id, ok := f.endpointOf(soilKey); !ok || id != endpointID {
			return fmt.Errorf("SetSimulatedSoilMoisture: endpoint %d: %w", endpointID, errWrongSoilEndpoint)
		}
	}
	f.soil.reportFromDevice(value)
	return nil
}
