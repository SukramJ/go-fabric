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
