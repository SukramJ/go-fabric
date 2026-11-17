-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v3 down: refused while the registry carries the seal canary,
-- as store's v10 Down refuses a database its canary marks (the canary is a
-- sealed matter_settings row there). Dropping the mark would let a store
-- without the sealer write a secret value in clear again, and a sealer
-- under another key open the registry while it holds no sealed value. A
-- registry a sqlstore.NewSealed marked is not downgraded; the controller's
-- factory reset (an empty registry) drops the mark, and the step then
-- runs. data holds nothing but the canary's sealed value, so dropping the
-- column loses nothing else.

DROP TABLE IF EXISTS temp.matter_client_v3_down_guard;
CREATE TEMP TABLE matter_client_v3_down_guard (
    seal_canary INTEGER CONSTRAINT v3_down_refuses_the_seal_canary CHECK(seal_canary = 0)
);
INSERT INTO matter_client_v3_down_guard (seal_canary)
    SELECT COUNT(*) FROM matter_client_metadata WHERE key = 'sqlstore.seal_canary';
DROP TABLE matter_client_v3_down_guard;

ALTER TABLE matter_client_metadata DROP COLUMN data;
