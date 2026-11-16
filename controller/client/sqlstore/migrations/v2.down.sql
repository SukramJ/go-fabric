-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v2 down: refused while a sealed row exists — dropping the mark
-- would leave a sealed value that reads as clear (store's v10 down does
-- the same).

DROP TABLE IF EXISTS temp.matter_client_v2_down_guard;
CREATE TEMP TABLE matter_client_v2_down_guard (
    sealed_rows INTEGER CONSTRAINT v2_down_refuses_sealed_rows CHECK(sealed_rows = 0)
);
INSERT INTO matter_client_v2_down_guard (sealed_rows) SELECT COUNT(*) FROM matter_client_values WHERE sealed <> 0;
DROP TABLE matter_client_v2_down_guard;

ALTER TABLE matter_client_values DROP COLUMN sealed;
DROP TABLE IF EXISTS matter_client_metadata;
