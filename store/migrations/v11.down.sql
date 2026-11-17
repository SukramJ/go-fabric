-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Reverts v11. The GroupKeyMap entries stay and lose their written order:
-- a v10 store reads them by GroupID ascending. The resumption records stay
-- and lose their write sequence; a v10 store does not cap them.

ALTER TABLE matter_resumption DROP COLUMN write_seq;
ALTER TABLE matter_group_key_map DROP COLUMN position;
