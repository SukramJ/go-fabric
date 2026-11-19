-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v12 (review II, R2-P7-sec-01, decision 270): a resumption
-- record carries the role that wrote it. initiator is 1 when the CASE
-- client of client/handshake wrote the record
-- (store.ResumptionRecord.Initiator), 0 for a CASE responder and every
-- writer that does not set it; store.UpsertResumption caps each role per
-- fabric on its own (16 rows of initiator 0, 1024 of initiator 1, owner
-- decision 273) and a write evicts only rows of its own role. A row an
-- earlier release stored did not record its role and takes 0: it stays
-- under the cap of 16, as every record was before v12. The statement is
-- store/schema.sql's.

ALTER TABLE matter_resumption ADD COLUMN initiator INTEGER NOT NULL DEFAULT 0 CHECK(initiator IN (0, 1));
