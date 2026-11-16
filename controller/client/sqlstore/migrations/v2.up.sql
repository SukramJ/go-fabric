-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v2: the seal mark of a secret value (the kept onboarding
-- code), which a sealed store (sqlstore.NewSealed) sets on the rows it
-- seals; 0 is a value in clear.

ALTER TABLE matter_client_values ADD COLUMN sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1));
