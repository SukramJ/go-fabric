-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v6 (brief BM, milestone 3 phase 17d): the DoorLock server's
-- users, credentials and schedules (cluster/lock AuthStore), nonvolatile as
-- matter.js keeps them (DoorLockServer.ts State :1538-1574). The statements
-- are store/schema.sql's.

CREATE TABLE IF NOT EXISTS matter_doorlock_users (
    endpoint_key               TEXT     NOT NULL,
    user_index                 INTEGER  NOT NULL CHECK(user_index BETWEEN 1 AND 65535),
    user_name                  TEXT     NOT NULL,
    user_unique_id             INTEGER,
    user_status                INTEGER  NOT NULL,
    user_type                  INTEGER  NOT NULL,
    credential_rule            INTEGER  NOT NULL,
    credentials_json           TEXT     NOT NULL,
    creator_fabric_index       INTEGER  NOT NULL,
    last_modified_fabric_index INTEGER  NOT NULL,
    expiring_user_expires_at   INTEGER,
    PRIMARY KEY(endpoint_key, user_index)
);

CREATE TABLE IF NOT EXISTS matter_doorlock_credentials (
    endpoint_key               TEXT     NOT NULL,
    credential_type            INTEGER  NOT NULL,
    credential_index           INTEGER  NOT NULL CHECK(credential_index BETWEEN 0 AND 65535),
    credential_data            BLOB     NOT NULL,
    creator_fabric_index       INTEGER  NOT NULL,
    last_modified_fabric_index INTEGER  NOT NULL,
    PRIMARY KEY(endpoint_key, credential_type, credential_index)
);

CREATE TABLE IF NOT EXISTS matter_doorlock_schedules (
    endpoint_key        TEXT     NOT NULL,
    kind                TEXT     NOT NULL CHECK(kind IN ('week_day', 'year_day', 'holiday')),
    schedule_index      INTEGER  NOT NULL CHECK(schedule_index BETWEEN 0 AND 255),
    user_index          INTEGER  NOT NULL,
    days_mask           INTEGER  NOT NULL DEFAULT 0,
    start_hour          INTEGER  NOT NULL DEFAULT 0,
    start_minute        INTEGER  NOT NULL DEFAULT 0,
    end_hour            INTEGER  NOT NULL DEFAULT 0,
    end_minute          INTEGER  NOT NULL DEFAULT 0,
    local_start_time    INTEGER  NOT NULL DEFAULT 0,
    local_end_time      INTEGER  NOT NULL DEFAULT 0,
    operating_mode      INTEGER  NOT NULL DEFAULT 0,
    PRIMARY KEY(endpoint_key, kind, user_index, schedule_index)
);
