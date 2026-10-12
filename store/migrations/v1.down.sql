-- SPDX-License-Identifier: MIT
-- Copyright (C) 2026 SukramJ.
--
-- Reverts v1: drops every table v1 creates (their indexes go with them),
-- children before matter_fabrics. Run v2's to v4's Down first.

DROP TABLE IF EXISTS matter_persistent_subscriptions;
DROP TABLE IF EXISTS matter_settings;
DROP TABLE IF EXISTS matter_metadata;
DROP TABLE IF EXISTS matter_diagnostics;
DROP TABLE IF EXISTS matter_resumption;
DROP TABLE IF EXISTS matter_acl_entries;
DROP TABLE IF EXISTS matter_group_key_map;
DROP TABLE IF EXISTS matter_group_keys;
DROP TABLE IF EXISTS matter_node_identities;
DROP TABLE IF EXISTS matter_fabrics;
