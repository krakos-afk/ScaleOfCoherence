-- Reverse migration: restore the stricter completion consistency check.
--
-- NOTE: this will FAIL if any rows are in the pending-approval state
--       (completed_by/completion_note set with completed_at NULL).
--       Finalize or clear those rows first.

ALTER TABLE item_stage_activity
    DROP CONSTRAINT item_stage_activity_check;

ALTER TABLE item_stage_activity
    ADD CONSTRAINT item_stage_activity_check CHECK (
        (completed_by IS NULL AND completion_note IS NULL AND completed_at IS NULL)
        OR
        (completed_by IS NOT NULL
            AND completion_note IS NOT NULL
            AND length(trim(completion_note)) > 0
            AND completed_at IS NOT NULL)
    );
