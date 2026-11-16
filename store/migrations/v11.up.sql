-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v11 (review II, decisions 148 and 134): the GroupKeyMap keeps
-- the order it was written in, and a resumption record its write sequence.
-- position is the entry's place in the fabric's
-- map: store.ReplaceGroupKeyMappings writes the list's index, a new entry
-- of store.SetGroupKeyMapping goes after the last, and
-- store.ListGroupKeyMappings reads by position, then GroupID. A row an
-- earlier release stored takes 0, so a map written before v11 reads by
-- GroupID ascending, as it did.
--
-- write_seq is the order the store wrote the resumption records in:
-- store.UpsertResumption gives each write one more than the table's
-- highest, and the per-fabric cap evicts the fabric's lowest. It is the
-- store's own count, not the clock: a clock that steps back cannot make
-- the record just written the oldest. A row an earlier release stored
-- takes 0 and goes first. The statements are store/schema.sql's.

ALTER TABLE matter_group_key_map ADD COLUMN position INTEGER NOT NULL DEFAULT 0 CHECK(position >= 0);
ALTER TABLE matter_resumption ADD COLUMN write_seq INTEGER NOT NULL DEFAULT 0 CHECK(write_seq >= 0);
