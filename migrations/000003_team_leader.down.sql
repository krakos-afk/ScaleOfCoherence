-- Reverse migration: remove 'leader' role and make assigned_user_id NOT NULL again.

-- NOTE: this will FAIL if any team_members rows have role = 'leader'
--       or if any item_stage_activity rows have assigned_user_id = NULL.
--       Clean those up first.

ALTER TABLE item_stage_activity
    ALTER COLUMN assigned_user_id SET NOT NULL;

ALTER TABLE team_members
    DROP CONSTRAINT team_members_role_check;

ALTER TABLE team_members
    ADD CONSTRAINT team_members_role_check
        CHECK (role IN ('viewer', 'editor'));
