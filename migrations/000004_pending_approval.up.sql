-- Support the pending-approval state for stage activities.
--
-- When a stage requires approval, the assignee submits a completion note
-- (completed_by + completion_note set) but the activity is not finalized yet,
-- so completed_at/released_at remain NULL until an approver acts.
-- The original constraint required completed_at whenever completed_by was set,
-- which made the pending state impossible. Relax it so completed_at is only
-- tied to completed_by/completion_note being present, not to finalization.

ALTER TABLE item_stage_activity
    DROP CONSTRAINT item_stage_activity_check;

ALTER TABLE item_stage_activity
    ADD CONSTRAINT item_stage_activity_check CHECK (
        (completed_by IS NULL AND completion_note IS NULL AND completed_at IS NULL)
        OR
        (completed_by IS NOT NULL
            AND completion_note IS NOT NULL
            AND length(trim(completion_note)) > 0)
    );
