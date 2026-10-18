-- Add 'leader' as a valid team member role.
ALTER TABLE team_members
    DROP CONSTRAINT team_members_role_check;

ALTER TABLE team_members
    ADD CONSTRAINT team_members_role_check
        CHECK (role IN ('viewer', 'editor', 'leader'));

-- Allow item_stage_activity rows without an assigned individual.
-- When a stage changes the team is responsible; a specific member self-claims via /claim.
ALTER TABLE item_stage_activity
    ALTER COLUMN assigned_user_id DROP NOT NULL;

-- Drop the existing FK so we can redefine it as nullable-aware.
-- (PostgreSQL allows nullable FK columns natively; the constraint stays, just the NOT NULL goes.)
-- No FK change needed — partial FKs on nullable columns work as-is in PostgreSQL.
