-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Reverts v11. The GroupKeyMap entries stay and lose their written order:
-- a v10 store reads them by GroupID ascending.

ALTER TABLE matter_group_key_map DROP COLUMN position;
