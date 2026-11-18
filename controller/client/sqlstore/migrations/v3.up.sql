-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v3: the registry's seal canary (sqlstore.NewSealed). The
-- reserved row sqlstore.seal_canary of matter_client_metadata carries a
-- known value sealed under the registry's sealer in data; the first
-- NewSealed writes it, every later one opens it first, sqlstore.Reseal
-- moves it to the new sealer. Every other row leaves data NULL.

ALTER TABLE matter_client_metadata ADD COLUMN data BLOB;
