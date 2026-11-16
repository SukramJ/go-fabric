-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v10 (review II, decision 108 and R2-P5-port-08): every table
-- that holds a secret gains its row's seal mark, and the DoorLock users and
-- credentials their record source.
--
-- sealed is 0 when the row's secret columns are stored as they are and 1
-- when a store.Sealer sealed them (store.NewSealed). The mark is the
-- row's, not the value's: a clear key is random bytes, so no prefix of a
-- value could tell the two forms apart with certainty.
--
-- created_through_matter and modified_through_matter say whether the
-- record was created, and last changed, through a Matter command
-- (store.DoorLockUser.CreatedThroughMatter); a record a host seeds keeps
-- them 0. A row an earlier release stored takes 1, which keeps the
-- answers it gave. The statements are store/schema.sql's.

ALTER TABLE matter_node_identities ADD COLUMN sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1));
ALTER TABLE matter_group_keys ADD COLUMN sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1));
ALTER TABLE matter_resumption ADD COLUMN sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1));
ALTER TABLE matter_settings ADD COLUMN sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1));
ALTER TABLE matter_doorlock_users ADD COLUMN created_through_matter INTEGER NOT NULL DEFAULT 1 CHECK(created_through_matter IN (0, 1));
ALTER TABLE matter_doorlock_users ADD COLUMN modified_through_matter INTEGER NOT NULL DEFAULT 1 CHECK(modified_through_matter IN (0, 1));
ALTER TABLE matter_doorlock_credentials ADD COLUMN sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1));
ALTER TABLE matter_doorlock_credentials ADD COLUMN created_through_matter INTEGER NOT NULL DEFAULT 1 CHECK(created_through_matter IN (0, 1));
ALTER TABLE matter_doorlock_credentials ADD COLUMN modified_through_matter INTEGER NOT NULL DEFAULT 1 CHECK(modified_through_matter IN (0, 1));
ALTER TABLE matter_icd_registrations ADD COLUMN sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1));
