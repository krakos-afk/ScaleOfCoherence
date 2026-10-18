-- The "my work" view and the dashboard attention widget only ever look at
-- open activity rows (released_at IS NULL). History rows dominate this table
-- over time, so index just the open set: the partial index stays tiny and
-- hot for assignee lookups while every historical row is skipped entirely.
DROP INDEX IF EXISTS idx_item_stage_activity_assigned_user;

CREATE INDEX idx_item_stage_activity_assigned_user_open
    ON item_stage_activity(org_id, assigned_user_id)
    WHERE completed_at IS NULL AND released_at IS NULL;

-- Pending approvals are read per stage for the dashboard widget.
CREATE INDEX idx_item_stage_activity_pending_approval
    ON item_stage_activity(org_id, stage_id)
    WHERE approval_status = 'pending' AND released_at IS NULL;
