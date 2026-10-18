CREATE TABLE workflows (
                           id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                           org_id UUID NOT NULL REFERENCES organization(id) ON DELETE RESTRICT,

                           name TEXT NOT NULL,
                           description TEXT NOT NULL DEFAULT '',

                           created_by UUID NOT NULL,
                           created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                           updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                           FOREIGN KEY (org_id, created_by)
                               REFERENCES organization_members(org_id, user_id)
                               ON DELETE RESTRICT,
                           UNIQUE (org_id, name),
                           UNIQUE (org_id, id)
);

CREATE TABLE workflow_stages (
                                 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

                                 org_id UUID NOT NULL,
                                 workflow_id UUID NOT NULL,

                                 assigned_team_id UUID NOT NULL,
                                 name TEXT NOT NULL,
                                 description TEXT NOT NULL DEFAULT '',
                                 position INTEGER NOT NULL CHECK (position >= 1),

                                 completion_requirements JSONB NOT NULL DEFAULT '[]'::jsonb
                                     CHECK (jsonb_typeof(completion_requirements) = 'array'),

                                 approval_required BOOLEAN NOT NULL DEFAULT false,
                                 approver_user_id UUID,

                                 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                                 FOREIGN KEY (org_id, workflow_id)
                                     REFERENCES workflows(org_id, id) ON DELETE CASCADE,

                                 FOREIGN KEY (org_id, assigned_team_id)
                                     REFERENCES teams(org_id, id),

                                 FOREIGN KEY (org_id, approver_user_id)
                                     REFERENCES organization_members(org_id, user_id),

                                 UNIQUE (workflow_id, position) DEFERRABLE INITIALLY DEFERRED,
                                 UNIQUE (workflow_id, name),
                                 UNIQUE (org_id, id),
                                 UNIQUE (org_id, id, workflow_id),

                                 CHECK (
                                     (approval_required AND approver_user_id IS NOT NULL)
                                         OR
                                     (NOT approval_required AND approver_user_id IS NULL)
                                     )
);

CREATE TABLE items (
                       id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                       title TEXT NOT NULL,
                       description TEXT NOT NULL DEFAULT '',
                       org_id UUID NOT NULL REFERENCES organization(id) ON DELETE RESTRICT,
                       workflow_id UUID,
                       current_stage_id UUID,
                       priority TEXT NOT NULL DEFAULT 'medium'
                           CHECK (priority IN ('urgent', 'high', 'medium', 'low')),
                       version BIGINT NOT NULL DEFAULT 1
                           CHECK (version >= 1),
                       custom_fields JSONB NOT NULL DEFAULT '{}'::jsonb
                           CHECK (jsonb_typeof(custom_fields) = 'object'),
                       created_by UUID NOT NULL,
                       created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                       updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                       closed_at TIMESTAMPTZ,

                       UNIQUE (org_id, id),

                       FOREIGN KEY (org_id, current_stage_id, workflow_id)
                           REFERENCES workflow_stages(org_id, id, workflow_id),

                       FOREIGN KEY (org_id, created_by)
                           REFERENCES organization_members(org_id, user_id) ON DELETE RESTRICT
);

CREATE TABLE item_changes (
                              id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                              org_id UUID NOT NULL,
                              item_id UUID NOT NULL,
                              version BIGINT NOT NULL CHECK (version >= 1),
                              actor_id UUID NOT NULL,
                              changes JSONB NOT NULL
                                  CHECK (jsonb_typeof(changes) = 'object'),
                              created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                              FOREIGN KEY (org_id, item_id)
                                  REFERENCES items(org_id, id) ON DELETE RESTRICT,

                              FOREIGN KEY (org_id, actor_id)
                                  REFERENCES organization_members(org_id, user_id) ON DELETE RESTRICT,

                              UNIQUE (item_id, version),
                              UNIQUE (org_id, item_id, version)
);

CREATE TABLE item_stage_activity (
                                     id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                                     org_id UUID NOT NULL,
                                     item_id UUID NOT NULL,
                                     stage_id UUID NOT NULL,

                                     team_id UUID NOT NULL,

                                     assigned_user_id UUID NOT NULL,
                                     assigned_by UUID NOT NULL,
                                     assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                                     completed_by UUID,
                                     completion_note TEXT,
                                     completed_at TIMESTAMPTZ,

                                     released_at TIMESTAMPTZ,
                                     release_reason TEXT
                                         CHECK (release_reason IN ('completed', 'reassigned', 'stage_changed')),

                                     approval_status TEXT
                                         CHECK (approval_status IN ('pending', 'approved', 'rejected')),
                                     approved_by UUID,
                                     approved_at TIMESTAMPTZ,
                                     approval_note TEXT,

                                     item_version BIGINT NOT NULL CHECK (item_version >= 1),

                                     FOREIGN KEY (org_id, item_id, item_version)
                                         REFERENCES item_changes(org_id, item_id, version)
                                         ON DELETE RESTRICT,

                                     FOREIGN KEY (org_id, stage_id)
                                         REFERENCES workflow_stages(org_id, id) ON DELETE RESTRICT,

                                     FOREIGN KEY (org_id, team_id)
                                         REFERENCES teams(org_id, id) ON DELETE RESTRICT,

                                     FOREIGN KEY (org_id, assigned_user_id)
                                         REFERENCES organization_members(org_id, user_id) ON DELETE RESTRICT,

                                     FOREIGN KEY (org_id, assigned_by)
                                         REFERENCES organization_members(org_id, user_id) ON DELETE RESTRICT,

                                     FOREIGN KEY (org_id, completed_by)
                                         REFERENCES organization_members(org_id, user_id) ON DELETE RESTRICT,

                                     FOREIGN KEY (org_id, approved_by)
                                         REFERENCES organization_members(org_id, user_id) ON DELETE RESTRICT,

                                     CHECK (
                                         (completed_by IS NULL
                                             AND completion_note IS NULL
                                             AND completed_at IS NULL)
                                             OR
                                         (completed_by IS NOT NULL
                                             AND completion_note IS NOT NULL
                                             AND length(trim(completion_note)) > 0
                                             AND completed_at IS NOT NULL)
                                         ),

                                     CHECK ((released_at IS NULL) = (release_reason IS NULL)),

                                     CHECK (
                                         completed_at IS NULL
                                             OR (released_at IS NOT NULL AND release_reason = 'completed')
                                         )
);

CREATE INDEX idx_item_changes_item_id_created_at
    ON item_changes(item_id, created_at DESC);

CREATE INDEX idx_item_stage_activity_item
    ON item_stage_activity(item_id, assigned_at DESC);

CREATE INDEX idx_item_stage_activity_stage
    ON item_stage_activity(org_id, stage_id);

CREATE INDEX idx_item_stage_activity_assigned_user
    ON item_stage_activity(org_id, assigned_user_id);

CREATE INDEX idx_item_changes_actor
    ON item_changes(org_id, actor_id);

CREATE INDEX idx_items_current_stage
    ON items(org_id, current_stage_id);

CREATE INDEX idx_items_created_by
    ON items(org_id, created_by);

CREATE INDEX idx_workflow_stages_assigned_team
    ON workflow_stages(org_id, assigned_team_id);

CREATE UNIQUE INDEX idx_item_stage_activity_one_open_per_item
    ON item_stage_activity(item_id)
    WHERE completed_at IS NULL AND released_at IS NULL;
