-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v5 (go-fabric v0.8.3): the Thermostat presets (brief BL). The
-- statement is store/schema.sql's.

CREATE TABLE IF NOT EXISTS matter_thermostat_presets (
    endpoint_key            TEXT       PRIMARY KEY,
    presets                 BLOB       NOT NULL,
    active_preset_handle    BLOB,
    updated_at              TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
