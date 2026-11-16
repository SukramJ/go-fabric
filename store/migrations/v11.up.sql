-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v11 (review II, decision 148, F-38): the GroupKeyMap keeps the
-- order it was written in. position is the entry's place in the fabric's
-- map: store.ReplaceGroupKeyMappings writes the list's index, a new entry
-- of store.SetGroupKeyMapping goes after the last, and
-- store.ListGroupKeyMappings reads by position, then GroupID. A row an
-- earlier release stored takes 0, so a map written before v11 reads by
-- GroupID ascending, as it did. The statement is store/schema.sql's.

ALTER TABLE matter_group_key_map ADD COLUMN position INTEGER NOT NULL DEFAULT 0 CHECK(position >= 0);
