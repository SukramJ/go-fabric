-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v2: what a sealed store (sqlstore.NewSealed) needs. The seal
-- mark of a secret value on matter_client_values — the kept onboarding
-- code (cluster 0xFFFFFFFC, attribute 0) and the controller's ICD Check-In
-- key and pending key (cluster 0xFFFFFFFD, attributes 0 and 1) — which the
-- store sets on the rows it seals; 0 is a value in clear. And
-- matter_client_metadata, where the sealing records a scrub of the file it
-- still owes (key sqlstore.seal_scrub_owed), until a scrub finished.

CREATE TABLE IF NOT EXISTS matter_client_metadata (
    key         TEXT     PRIMARY KEY NOT NULL,
    value       INTEGER  NOT NULL
);

ALTER TABLE matter_client_values ADD COLUMN sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1));
