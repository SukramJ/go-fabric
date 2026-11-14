-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Reverts v8. A v7 database has no pending mark, so every pending fabric
-- is rolled back first, as RollbackFabric does: an uncommitted UpdateNOC's
-- prior values move back, an uncommitted AddNOC's fabric is deleted (its
-- rows go with it through the foreign keys' ON DELETE CASCADE, which needs
-- PRAGMA foreign_keys on the connection). The VID verification data moves
-- back into the settings v7 reads (lower-case hex, the encoding of
-- cluster/core's storeVidVerification). Then the columns go; the server
-- subscriptions fall back to updated_at's order.

UPDATE matter_node_identities
SET noc = prior_noc, icac = prior_icac, private_key = prior_private_key
WHERE fabric_index IN (SELECT fabric_index FROM matter_fabrics WHERE pending = 2);
UPDATE matter_fabrics
SET node_id = prior_node_id, vendor_id = prior_vendor_id,
    vid_statement = prior_vid_statement, vvsc = prior_vvsc
WHERE pending = 2;
DELETE FROM matter_fabrics WHERE pending = 1;

INSERT OR REPLACE INTO matter_settings (key, value)
SELECT 'matter.fabric.' || fabric_index || '.vid.statement', lower(hex(vid_statement))
FROM matter_fabrics WHERE vid_statement IS NOT NULL;
INSERT OR REPLACE INTO matter_settings (key, value)
SELECT 'matter.fabric.' || fabric_index || '.vid.vvsc', lower(hex(vvsc))
FROM matter_fabrics WHERE vvsc IS NOT NULL;

ALTER TABLE matter_server_subscriptions DROP COLUMN save_seq;
ALTER TABLE matter_node_identities DROP COLUMN prior_private_key;
ALTER TABLE matter_node_identities DROP COLUMN prior_icac;
ALTER TABLE matter_node_identities DROP COLUMN prior_noc;
ALTER TABLE matter_fabrics DROP COLUMN prior_vvsc;
ALTER TABLE matter_fabrics DROP COLUMN prior_vid_statement;
ALTER TABLE matter_fabrics DROP COLUMN prior_vendor_id;
ALTER TABLE matter_fabrics DROP COLUMN prior_node_id;
ALTER TABLE matter_fabrics DROP COLUMN vvsc;
ALTER TABLE matter_fabrics DROP COLUMN vid_statement;
ALTER TABLE matter_fabrics DROP COLUMN pending;
