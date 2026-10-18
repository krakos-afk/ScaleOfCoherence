DROP INDEX IF EXISTS idx_item_stage_activity_pending_approval;
DROP INDEX IF EXISTS idx_item_stage_activity_assigned_user_open;

CREATE INDEX idx_item_stage_activity_assigned_user
    ON item_stage_activity(org_id, assigned_user_id);
