-- Trigram search indexes for the remaining searchable list endpoints.

CREATE INDEX idx_teams_name_trgm
    ON teams USING gin (name gin_trgm_ops);

CREATE INDEX idx_workflows_name_trgm
    ON workflows USING gin (name gin_trgm_ops);

CREATE INDEX idx_users_username_trgm
    ON users USING gin (username gin_trgm_ops);
