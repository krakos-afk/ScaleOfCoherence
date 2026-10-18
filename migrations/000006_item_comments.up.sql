-- Collaboration: item comments and a comment-only team role.

-- ── 1. 'commentator' may write comments but not work on item activity ────────
ALTER TABLE team_members
    DROP CONSTRAINT team_members_role_check;

ALTER TABLE team_members
    ADD CONSTRAINT team_members_role_check
        CHECK (role IN ('viewer', 'editor', 'commentator', 'leader'));

-- ── 2. Comments are anchored to the item version they were made on ───────────
-- version records items.version at insert time and must reference a logged
-- change (item_changes), so every comment sits on the same version track as
-- history and stage activity. Comments never bump the item version.
CREATE TABLE item_comments (
                               id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                               org_id UUID NOT NULL,
                               item_id UUID NOT NULL,
                               author_id UUID NOT NULL,
                               version BIGINT NOT NULL CHECK (version >= 1),
                               body TEXT NOT NULL CHECK (length(trim(body)) > 0),
                               created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                               FOREIGN KEY (org_id, item_id)
                                   REFERENCES items(org_id, id) ON DELETE CASCADE,

                               FOREIGN KEY (org_id, item_id, version)
                                   REFERENCES item_changes(org_id, item_id, version),

                               FOREIGN KEY (org_id, author_id)
                                   REFERENCES organization_members(org_id, user_id) ON DELETE CASCADE
);

CREATE INDEX idx_item_comments_item
    ON item_comments(org_id, item_id, created_at DESC, id DESC);

CREATE INDEX idx_item_comments_author
    ON item_comments(org_id, author_id);
