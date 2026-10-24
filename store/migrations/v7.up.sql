-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v7 (brief CH, milestone 5 phase 23): the ICD Management
-- server's registered Check-In clients with their ICDToken keys, nonvolatile
-- as matter.js keeps them (IcdManagementServer.ts State :787-791,
-- RegisteredClients quality "N"). The statement is store/schema.sql's.

CREATE TABLE IF NOT EXISTS matter_icd_registrations (
    fabric_index        INTEGER  NOT NULL,
    position            INTEGER  NOT NULL,
    check_in_node_id    BLOB     NOT NULL,
    monitored_subject   BLOB     NOT NULL,
    icd_key             BLOB     NOT NULL,
    client_type         INTEGER  NOT NULL CHECK(client_type BETWEEN 0 AND 255),
    PRIMARY KEY(fabric_index, position),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);
