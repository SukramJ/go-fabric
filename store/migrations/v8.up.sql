-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v8 (review II, decision 36): the fabric row carries its own
-- commit state. matter_fabrics gains the pending mark (0 committed, 1 an
-- uncommitted AddNOC, 2 an uncommitted UpdateNOC), the VID verification
-- statement and VVSC (until v8 the settings matter.fabric.<index>.vid.*),
-- and the prior values an uncommitted UpdateNOC replaced; matter_node_identities
-- gains the prior NOC, ICAC and private key. store.Upgrade moves the VID
-- settings into the new columns. matter_server_subscriptions gains
-- save_seq, the exact save order its rows load in (review II, rework of
-- R2-P2-sess-01). The column definitions are store/schema.sql's.

ALTER TABLE matter_fabrics ADD COLUMN pending INTEGER NOT NULL DEFAULT 0 CHECK(pending BETWEEN 0 AND 2);
ALTER TABLE matter_fabrics ADD COLUMN vid_statement BLOB;
ALTER TABLE matter_fabrics ADD COLUMN vvsc BLOB;
ALTER TABLE matter_fabrics ADD COLUMN prior_node_id BLOB;
ALTER TABLE matter_fabrics ADD COLUMN prior_vendor_id INTEGER CHECK(prior_vendor_id BETWEEN 0 AND 65535);
ALTER TABLE matter_fabrics ADD COLUMN prior_vid_statement BLOB;
ALTER TABLE matter_fabrics ADD COLUMN prior_vvsc BLOB;
ALTER TABLE matter_node_identities ADD COLUMN prior_noc BLOB;
ALTER TABLE matter_node_identities ADD COLUMN prior_icac BLOB;
ALTER TABLE matter_node_identities ADD COLUMN prior_private_key BLOB;
ALTER TABLE matter_server_subscriptions ADD COLUMN save_seq INTEGER NOT NULL DEFAULT 0;
