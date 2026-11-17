-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v3 down: the seal canary goes with its column. No value is
-- lost: the sealed values keep their seal mark (v2), and a release at v2
-- checks the key on one of them, as the release before v3 did; v3's Up
-- and the next sqlstore.NewSealed mark the registry again.

DELETE FROM matter_client_metadata WHERE key = 'sqlstore.seal_canary';
ALTER TABLE matter_client_metadata DROP COLUMN data;
