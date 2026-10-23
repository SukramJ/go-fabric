// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/storetest"
)

// TestStoreContract holds SQLite's Store to the port's contract, the same
// table store/memstore runs.
func TestStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Port { return store.New(openTestDB(t)) })
}

// TestStoreImplementsEveryArea: SQLite's Store is each area of the port, the
// interface a consumer names when it needs only that area.
func TestStoreImplementsEveryArea(t *testing.T) {
	var s any = store.New(openTestDB(t))
	areas := map[string]bool{}
	_, areas["Fabrics"] = s.(store.Fabrics)
	_, areas["Identity"] = s.(store.Identity)
	_, areas["ACL"] = s.(store.ACL)
	_, areas["GroupKeys"] = s.(store.GroupKeys)
	_, areas["Groups"] = s.(store.Groups)
	_, areas["Resumption"] = s.(store.Resumption)
	_, areas["ServerSubscriptions"] = s.(store.ServerSubscriptions)
	_, areas["Settings"] = s.(store.Settings)
	_, areas["OTA"] = s.(store.OTA)
	_, areas["ThermostatPresets"] = s.(store.ThermostatPresets)
	_, areas["DoorLocks"] = s.(store.DoorLocks)
	for area, ok := range areas {
		if !ok {
			t.Errorf("store.Store is not a store.%s", area)
		}
	}
}
