-- SPDX-License-Identifier: MIT
-- Copyright (C) 2026 SukramJ.
--
-- Reverts v3: rebuilds matter_group_key_map with the foreign key into
-- matter_group_keys again (v1's definition). This step is lossy: an
-- entry naming a key set that does not exist cannot satisfy that key and
-- is dropped, rather than failing the step.

CREATE TABLE matter_group_key_map_rebuild (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 0 AND 65535),
    group_key_set_id    INTEGER NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE,
    FOREIGN KEY(fabric_index, group_key_set_id)
        REFERENCES matter_group_keys(fabric_index, group_key_set_id) ON DELETE CASCADE
);

INSERT INTO matter_group_key_map_rebuild (fabric_index, group_id, group_key_set_id)
    SELECT m.fabric_index, m.group_id, m.group_key_set_id FROM matter_group_key_map m
    WHERE EXISTS (
        SELECT 1 FROM matter_group_keys k
        WHERE k.fabric_index = m.fabric_index AND k.group_key_set_id = m.group_key_set_id
    );

DROP TABLE matter_group_key_map;

ALTER TABLE matter_group_key_map_rebuild RENAME TO matter_group_key_map;
