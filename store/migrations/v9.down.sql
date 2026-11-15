-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Reverts v9. The owed fabric events go with their table: a v8 node has
-- no place for them, so a hook still owed is not delivered after the
-- downgrade. The resumption records lose their session parameters, which
-- a v8 node does not read.

DROP TABLE IF EXISTS matter_owed_fabric_events;
ALTER TABLE matter_resumption DROP COLUMN max_tcp_message_size;
ALTER TABLE matter_resumption DROP COLUMN supported_transports;
ALTER TABLE matter_resumption DROP COLUMN max_paths_per_invoke;
ALTER TABLE matter_resumption DROP COLUMN specification_version;
ALTER TABLE matter_resumption DROP COLUMN interaction_model_revision;
ALTER TABLE matter_resumption DROP COLUMN data_model_revision;
ALTER TABLE matter_resumption DROP COLUMN session_active_threshold;
ALTER TABLE matter_resumption DROP COLUMN session_active_interval;
ALTER TABLE matter_resumption DROP COLUMN session_idle_interval;
