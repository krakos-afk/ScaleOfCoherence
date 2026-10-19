\set ON_ERROR_STOP on
\timing on

-- Target-A sized dataset: one busy org, 100k items, 1.5M changes,
-- 400k activity rows, 200k comments. Triggers are disabled only for the bulk
-- load (the consistency they enforce is orthogonal to size/latency
-- measurements); they are re-enabled before measuring.
BEGIN;
SET LOCAL synchronous_commit = off;

ALTER TABLE items DISABLE TRIGGER USER;
ALTER TABLE item_stage_activity DISABLE TRIGGER USER;
ALTER TABLE item_comments DISABLE TRIGGER USER;

INSERT INTO organization (id, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'Bench Org');

INSERT INTO users (id, email, username, password_hash)
SELECT ('00000000-0000-0000-0000-' || lpad(to_hex(i), 12, '0'))::uuid,
       'bench' || i || '@example.com',
       'bench' || i,
       'placeholder'
FROM generate_series(1, 100) AS i;

UPDATE users
SET password_hash = '__BENCH_HASH__'
WHERE id = '00000000-0000-0000-0000-000000000001';

INSERT INTO organization_members (org_id, user_id, usertype)
SELECT '00000000-0000-0000-0000-000000000001',
       ('00000000-0000-0000-0000-' || lpad(to_hex(i), 12, '0'))::uuid,
       CASE WHEN i = 1 THEN 'admin' ELSE 'user' END
FROM generate_series(1, 100) AS i;

INSERT INTO teams (id, org_id, name)
SELECT ('20000000-0000-0000-0000-' || lpad(t::text, 12, '0'))::uuid,
       '00000000-0000-0000-0000-000000000001',
       'Team ' || t
FROM generate_series(1, 3) AS t;

INSERT INTO team_members (org_id, team_id, user_id, role)
SELECT '00000000-0000-0000-0000-000000000001',
       ('20000000-0000-0000-0000-' || lpad(t::text, 12, '0'))::uuid,
       ('00000000-0000-0000-0000-' || lpad(to_hex(u), 12, '0'))::uuid,
       'editor'
FROM generate_series(1, 3) AS t, generate_series(1, 100) AS u;

INSERT INTO workflows (id, org_id, name, description, created_by)
VALUES ('30000000-0000-0000-0000-000000000001',
        '00000000-0000-0000-0000-000000000001',
        'Default',
        'Bench workflow',
        '00000000-0000-0000-0000-000000000001');

INSERT INTO workflow_stages (id, org_id, workflow_id, assigned_team_id, name, position)
SELECT ('40000000-0000-0000-0000-' || lpad(s::text, 12, '0'))::uuid,
       '00000000-0000-0000-0000-000000000001',
       '30000000-0000-0000-0000-000000000001',
       ('20000000-0000-0000-0000-' || lpad((s % 3 + 1)::text, 12, '0'))::uuid,
       'Stage ' || s,
       s
FROM generate_series(1, 4) AS s;

-- 100k items: half closed, spread over a year, ~250-byte descriptions.
INSERT INTO items (id, title, description, org_id, workflow_id, current_stage_id,
                   priority, version, custom_fields, created_by, created_at,
                   updated_at, closed_at)
SELECT ('50000000-0000-0000-0000-' || lpad(to_hex(i), 12, '0'))::uuid,
       'Item ' || i || ' ' ||
       (ARRAY['fix login redirect regression',
              'payments retry handling',
              'improve search ranking',
              'update onboarding documentation',
              'investigate latency spike'])[1 + i % 5],
       'Reported by customer ' || (i % 500) || '. Expected: the deployment keeps the ' ||
       'permissions intact and the dashboard shows the migration result. Actual: the ' ||
       'webhook retries, the cache is stale and the invoice export fails with a ' ||
       'notification error. Steps: open the calendar, refresh the database snapshot ' ||
       'and re-run the import.',
       '00000000-0000-0000-0000-000000000001',
       '30000000-0000-0000-0000-000000000001',
       ('40000000-0000-0000-0000-' || lpad((i % 4 + 1)::text, 12, '0'))::uuid,
       (ARRAY['urgent', 'high', 'medium', 'low'])[1 + i % 4],
       15,
       jsonb_build_object('customer', 'cust-' || (i % 500), 'estimate', i % 21),
       ('00000000-0000-0000-0000-' || lpad(to_hex(1 + i % 100), 12, '0'))::uuid,
       now() - (i % 365) * interval '1 day' - (i % 24) * interval '1 hour',
       now() - (i % 365) * interval '1 day' - (i % 24) * interval '1 hour'
           + (i % 30) * interval '1 hour',
       CASE WHEN i % 2 = 0 THEN now() - (i % 180) * interval '1 day' END
FROM generate_series(1, 100000) AS i;

-- 1.5M changes: 15 logged versions per item, realistic payload sizes.
INSERT INTO item_changes (org_id, item_id, version, actor_id, changes, created_at)
SELECT i.org_id, i.id, v,
       ('00000000-0000-0000-0000-' || lpad(to_hex(1 + (i.row_num % 100)), 12, '0'))::uuid,
       CASE
           WHEN v = 1 THEN jsonb_build_object('type', 'created', 'title', i.title)
           WHEN v % 5 = 0 THEN jsonb_build_object(
               'type', 'stage_moved',
               'from', ('40000000-0000-0000-0000-' || lpad(((i.row_num + v) % 4 + 1)::text, 12, '0')),
               'to', ('40000000-0000-0000-0000-' || lpad(((i.row_num + v + 1) % 4 + 1)::text, 12, '0')))
           WHEN v % 3 = 0 THEN jsonb_build_object(
               'type', 'updated',
               'title', jsonb_build_array('Item ' || i.row_num || ' draft', i.title),
               'priority', jsonb_build_array('low', i.priority))
           WHEN v % 3 = 1 THEN jsonb_build_object(
               'type', 'activity',
               'activity_id', 1000000 + i.row_num * 4 + (v % 4),
               'action', 'claimed')
           ELSE jsonb_build_object(
               'type', 'updated',
               'custom_fields', jsonb_build_array(
                   jsonb_build_object('estimate', i.row_num % 21),
                   jsonb_build_object('estimate', i.row_num % 13)))
       END,
       i.created_at + v * interval '1 day'
FROM (SELECT id, org_id, title, priority, created_at, row_number() OVER () AS row_num
      FROM items) AS i
CROSS JOIN generate_series(1, 15) AS v;

-- 400k activity rows: three closed plus one open per active item.
INSERT INTO item_stage_activity (org_id, item_id, stage_id, team_id,
                                 assigned_user_id, assigned_by, assigned_at,
                                 completed_by, completion_note, completed_at,
                                 released_at, release_reason, item_version)
SELECT i.org_id, i.id,
       ('40000000-0000-0000-0000-' || lpad(((i.row_num + k) % 4 + 1)::text, 12, '0'))::uuid,
       ('20000000-0000-0000-0000-'
           || lpad((((i.row_num + k) % 4 + 1) % 3 + 1)::text, 12, '0'))::uuid,
       ('00000000-0000-0000-0000-' || lpad(to_hex(1 + (i.row_num + k) % 100), 12, '0'))::uuid,
       ('00000000-0000-0000-0000-' || lpad(to_hex(1 + i.row_num % 100), 12, '0'))::uuid,
       i.created_at + k * interval '2 days',
       CASE WHEN (i.closed_at IS NOT NULL OR k < 4) AND k <> 2 THEN
           ('00000000-0000-0000-0000-' || lpad(to_hex(1 + (i.row_num + k) % 100), 12, '0'))::uuid
       END,
       CASE WHEN (i.closed_at IS NOT NULL OR k < 4) AND k <> 2
           THEN 'Handled with the team, see notes.' END,
       CASE WHEN (i.closed_at IS NOT NULL OR k < 4) AND k <> 2
           THEN i.created_at + k * interval '2 days' + interval '6 hours' END,
       CASE WHEN i.closed_at IS NOT NULL OR k < 4
           THEN i.created_at + k * interval '2 days' + interval '1 day' END,
       CASE WHEN i.closed_at IS NOT NULL OR k < 4 THEN
           CASE WHEN k <> 2 THEN 'completed' ELSE 'stage_changed' END
       END,
       k * 3 + 1
FROM (SELECT id, org_id, created_at, closed_at, row_number() OVER () AS row_num
      FROM items) AS i
CROSS JOIN generate_series(1, 4) AS k;

-- 200k comments: two per item, anchored to logged versions.
INSERT INTO item_comments (org_id, item_id, author_id, version, body, created_at)
SELECT i.org_id, i.id,
       ('00000000-0000-0000-0000-' || lpad(to_hex(1 + (i.row_num + c) % 100), 12, '0'))::uuid,
       CASE WHEN c = 1 THEN 7 ELSE 14 END,
       'Checked with the customer, reproduced on staging and attached the logs. '
       || 'Next step is a fix behind a flag, then backfill.',
       i.created_at + (c * 3) * interval '1 day'
FROM (SELECT id, org_id, created_at, row_number() OVER () AS row_num
      FROM items) AS i
CROSS JOIN generate_series(1, 2) AS c;

ALTER TABLE items ENABLE TRIGGER USER;
ALTER TABLE item_stage_activity ENABLE TRIGGER USER;
ALTER TABLE item_comments ENABLE TRIGGER USER;

COMMIT;

ANALYZE organization, users, organization_members, teams, team_members,
        workflows, workflow_stages, items, item_changes,
        item_stage_activity, item_comments;
