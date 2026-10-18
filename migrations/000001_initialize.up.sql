CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE organization (
                              id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                              name TEXT NOT NULL,
                              created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
                       id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                       email TEXT NOT NULL,
                       username TEXT NOT NULL,
                       password_hash TEXT NOT NULL,
                       created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                       updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_users_email_lower ON users (lower(email));

CREATE TABLE organization_members (
                                      org_id UUID NOT NULL
                                          REFERENCES organization(id) ON DELETE CASCADE,
                                      user_id UUID NOT NULL
                                          REFERENCES users(id) ON DELETE CASCADE,
                                      usertype TEXT NOT NULL DEFAULT 'user'
                                          CHECK (usertype IN ('admin', 'user')),
                                      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                      updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                                      PRIMARY KEY (org_id, user_id)
);

CREATE TABLE teams (
                       id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                       org_id UUID NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
                       name TEXT NOT NULL,
                       created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                       updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                       UNIQUE (org_id, name),
                       UNIQUE (org_id, id)
);

CREATE TABLE team_members (
                              org_id UUID NOT NULL,
                              team_id UUID NOT NULL,
                              user_id UUID NOT NULL,
                              role TEXT NOT NULL DEFAULT 'viewer'
                                  CHECK (role IN ('viewer', 'editor')),
                              created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                              updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                              PRIMARY KEY (team_id, user_id),

                              FOREIGN KEY (org_id, team_id)
                                  REFERENCES teams(org_id, id) ON DELETE CASCADE,

                              FOREIGN KEY (org_id, user_id)
                                  REFERENCES organization_members(org_id, user_id) ON DELETE CASCADE
);

CREATE INDEX idx_team_members_org_user ON team_members (org_id, user_id);