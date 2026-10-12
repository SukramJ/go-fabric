-- SPDX-License-Identifier: MIT
-- Copyright (C) 2026 SukramJ.
--
-- Migration v3 (go-fabric v0.2.0): matter_group_key_map loses its foreign
-- key into matter_group_keys (CHANGELOG [0.2.0], Fixed: "A GroupKeyMap
-- entry naming a key set not written yet failed with Failure"). SQLite
-- cannot drop a constraint in place, so the rows move to a table of the
-- new definition, which then takes the old name. store.Upgrade runs this
-- same text in one transaction when the foreign key is still there.

CREATE TABLE matter_group_key_map_rebuild (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 0 AND 65535),
    group_key_set_id    INTEGER NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

INSERT INTO matter_group_key_map_rebuild (fabric_index, group_id, group_key_set_id)
    SELECT fabric_index, group_id, group_key_set_id FROM matter_group_key_map;

DROP TABLE matter_group_key_map;

ALTER TABLE matter_group_key_map_rebuild RENAME TO matter_group_key_map;
