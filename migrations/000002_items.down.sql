
DROP INDEX IF EXISTS idx_item_stage_activity_one_open_per_item;
DROP INDEX IF EXISTS idx_workflow_stages_assigned_team;
DROP INDEX IF EXISTS idx_items_created_by;
DROP INDEX IF EXISTS idx_items_current_stage;
DROP INDEX IF EXISTS idx_item_changes_actor;
DROP INDEX IF EXISTS idx_item_stage_activity_assigned_user;
DROP INDEX IF EXISTS idx_item_stage_activity_stage;
DROP INDEX IF EXISTS idx_item_stage_activity_item;
DROP INDEX IF EXISTS idx_item_changes_item_id_created_at;

DROP TABLE IF EXISTS item_stage_activity;
DROP TABLE IF EXISTS item_changes;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS workflow_stages;
DROP TABLE IF EXISTS workflows;
