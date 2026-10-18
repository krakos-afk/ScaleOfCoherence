DROP INDEX IF EXISTS idx_items_org_updated_at;
DROP INDEX IF EXISTS idx_items_org_workflow;
DROP INDEX IF EXISTS idx_items_org_closed_at;
DROP INDEX IF EXISTS idx_items_org_priority;
DROP INDEX IF EXISTS idx_items_description_trgm;
DROP INDEX IF EXISTS idx_items_title_trgm;

DROP EXTENSION IF EXISTS pg_trgm;
