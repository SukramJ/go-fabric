-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Reverts v12. The resumption records stay and lose their role; a v11
-- store counts every record against its per-fabric cap.

ALTER TABLE matter_resumption DROP COLUMN initiator;
