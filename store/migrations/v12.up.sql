-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v12 (review II, R2-P7-sec-01, decision 270): a resumption
-- record carries the role that wrote it. initiator is 1 when the node's own
-- CASE client wrote the record (store.ResumptionRecord.Initiator), 0 when
-- the CASE responder did; store.UpsertResumption's per-fabric cap counts
-- and evicts only the responder's records. A row an earlier release stored
-- did not record its role and takes 0: it stays under the cap, as every
-- record was before v12. The statement is store/schema.sql's.

ALTER TABLE matter_resumption ADD COLUMN initiator INTEGER NOT NULL DEFAULT 0 CHECK(initiator IN (0, 1));
