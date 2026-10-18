-- Unify item history and stage activity on one version track.
--
-- items.version is the single ordered track for everything that happens to an
-- item: every state-changing action bumps it by exactly 1 and writes exactly one
-- item_changes row at that version. item_stage_activity holds assignment detail
-- and is linked from the timeline entry via changes->>'activity_id'.

-- ── 1. Reconcile pre-existing data ───────────────────────────────────────────
-- The old code allowed several activity rows to share one item version
-- (reassign inserted a new row at the still-current version without bumping it).
-- The new model allows exactly one activity row per item version, so keep the
-- newest row for each (item_id, item_version) and drop the superseded ones.
-- Nothing references item_stage_activity, so this is safe.
DELETE FROM item_stage_activity a
USING item_stage_activity b
WHERE a.item_id = b.item_id
  AND a.item_version = b.item_version
  AND a.id < b.id;

-- ── 2. One activity row per item version ─────────────────────────────────────
ALTER TABLE item_stage_activity
    ADD CONSTRAINT item_stage_activity_item_version_key
        UNIQUE (item_id, item_version);

-- ── 3. Activity inserts must carry the item's current version ────────────────
CREATE FUNCTION check_activity_version() RETURNS trigger AS $$
BEGIN
  IF NEW.item_version <> (SELECT version FROM items
                          WHERE org_id = NEW.org_id AND id = NEW.item_id) THEN
    RAISE EXCEPTION 'stale item_version % for item %', NEW.item_version, NEW.item_id
      USING ERRCODE = '40001';
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER trg_activity_version
  BEFORE INSERT ON item_stage_activity
  FOR EACH ROW EXECUTE FUNCTION check_activity_version();

-- ── 4. Every item version must have a logged change (checked at commit) ──────
CREATE FUNCTION check_item_version_logged() RETURNS trigger AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM item_changes
                 WHERE org_id = NEW.org_id AND item_id = NEW.id AND version = NEW.version) THEN
    RAISE EXCEPTION 'item % at version % has no item_changes row', NEW.id, NEW.version;
  END IF;
  RETURN NULL;
END $$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_item_version_logged
  AFTER INSERT OR UPDATE OF version ON items
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION check_item_version_logged();

-- The existing UNIQUE (org_id, item_id, version) on item_changes already covers
-- timeline reads (org_id + item_id, ordered by version), so no extra index is
-- required. The existing partial unique index
-- idx_item_stage_activity_one_open_per_item is kept as a second safety net.
