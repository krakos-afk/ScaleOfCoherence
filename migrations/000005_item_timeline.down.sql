-- Reverse the item timeline migration.

DROP TRIGGER IF EXISTS trg_item_version_logged ON items;
DROP FUNCTION IF EXISTS check_item_version_logged();

DROP TRIGGER IF EXISTS trg_activity_version ON item_stage_activity;
DROP FUNCTION IF EXISTS check_activity_version();

ALTER TABLE item_stage_activity
    DROP CONSTRAINT IF EXISTS item_stage_activity_item_version_key;
