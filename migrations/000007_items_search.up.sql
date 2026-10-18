-- Trigram search and common item list filters.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Substring / fuzzy search over title and description (ILIKE '%term%').
CREATE INDEX idx_items_title_trgm
    ON items USING gin (title gin_trgm_ops);

CREATE INDEX idx_items_description_trgm
    ON items USING gin (description gin_trgm_ops);

-- Common list filters and sorts.
CREATE INDEX idx_items_org_priority
    ON items (org_id, priority);

CREATE INDEX idx_items_org_closed_at
    ON items (org_id, closed_at);

CREATE INDEX idx_items_org_workflow
    ON items (org_id, workflow_id);

CREATE INDEX idx_items_org_updated_at
    ON items (org_id, updated_at DESC);
