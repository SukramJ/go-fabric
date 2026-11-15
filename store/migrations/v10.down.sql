-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Reverts v10. A v9 store has no seal mark and would read a sealed value
-- as the secret itself, so the step refuses while any row is sealed: the
-- INSERT below fails its CHECK (v10_down_refuses_sealed_rows) and the
-- columns stay. A database a store.Sealer sealed is not downgraded. The
-- DoorLock records lose their source; a v9 server answers both fabric
-- indexes of every record.

DROP TABLE IF EXISTS temp.matter_v10_down_guard;
CREATE TEMP TABLE matter_v10_down_guard (
    sealed_rows INTEGER CONSTRAINT v10_down_refuses_sealed_rows CHECK(sealed_rows = 0)
);
INSERT INTO matter_v10_down_guard (sealed_rows) SELECT
    (SELECT COUNT(*) FROM matter_node_identities WHERE sealed <> 0) +
    (SELECT COUNT(*) FROM matter_group_keys WHERE sealed <> 0) +
    (SELECT COUNT(*) FROM matter_resumption WHERE sealed <> 0) +
    (SELECT COUNT(*) FROM matter_settings WHERE sealed <> 0) +
    (SELECT COUNT(*) FROM matter_doorlock_credentials WHERE sealed <> 0) +
    (SELECT COUNT(*) FROM matter_icd_registrations WHERE sealed <> 0);
DROP TABLE matter_v10_down_guard;

ALTER TABLE matter_icd_registrations DROP COLUMN sealed;
ALTER TABLE matter_doorlock_credentials DROP COLUMN modified_through_matter;
ALTER TABLE matter_doorlock_credentials DROP COLUMN created_through_matter;
ALTER TABLE matter_doorlock_credentials DROP COLUMN sealed;
ALTER TABLE matter_doorlock_users DROP COLUMN modified_through_matter;
ALTER TABLE matter_doorlock_users DROP COLUMN created_through_matter;
ALTER TABLE matter_settings DROP COLUMN sealed;
ALTER TABLE matter_resumption DROP COLUMN sealed;
ALTER TABLE matter_group_keys DROP COLUMN sealed;
ALTER TABLE matter_node_identities DROP COLUMN sealed;
