-- Development seed data: the people and groups in deps/authentik/golden-portal.yaml,
-- so the portal has users, groups and roles before anyone signs in.
-- Every insert skips rows that already exist, so this is safe to run again.
-- psql sets :issuer to GP_OIDC_ISSUER; the dev Authentik uses the email as the subject.

INSERT INTO groups (name)
VALUES ('platform-engineers'), ('payments'), ('search')
ON CONFLICT (name) DO NOTHING;

INSERT INTO users (oidc_issuer, oidc_subject, email, display_name)
VALUES
  (:'issuer', 'admin@example.com', 'admin@example.com', 'authentik Default Admin'),
  (:'issuer', 'member@example.com', 'member@example.com', 'Dev Member'),
  (:'issuer', 'builder@example.com', 'builder@example.com', 'Dev Builder'),
  (:'issuer', 'ada@example.com', 'ada@example.com', 'Ada Lovelace'),
  (:'issuer', 'grace@example.com', 'grace@example.com', 'Grace Hopper')
ON CONFLICT (oidc_issuer, oidc_subject) DO NOTHING;

INSERT INTO group_members (group_id, user_id)
SELECT g.id, u.id
FROM (VALUES
  ('platform-engineers', 'builder@example.com'),
  ('payments', 'ada@example.com'),
  ('payments', 'grace@example.com'),
  ('search', 'grace@example.com')
) AS m (group_name, email)
JOIN groups g ON g.name = m.group_name
JOIN users u ON u.oidc_issuer = :'issuer' AND u.oidc_subject = m.email
ON CONFLICT DO NOTHING;

INSERT INTO role_bindings (subject_type, subject_id, role)
SELECT 'user', id, 'admin' FROM users
WHERE oidc_issuer = :'issuer' AND oidc_subject = 'admin@example.com'
ON CONFLICT DO NOTHING;

INSERT INTO role_bindings (subject_type, subject_id, role)
SELECT 'group', id, 'builder' FROM groups WHERE name = 'platform-engineers'
ON CONFLICT DO NOTHING;

-- Example blueprints, published as version 1. They're only examples: nothing
-- in the portal depends on them, and they can be changed or deleted.
WITH example (identifier, source) AS (VALUES
  ('team', $yaml$identifier: team
title: Team
description: A group of people who own things in the catalog.
icon: users
properties:
  slackChannel:
    title: Slack Channel
    type: string
    format: url
  oncall:
    title: On-call Rotation
    type: string
    format: url
$yaml$),
  ('service', $yaml$identifier: service
title: Service
description: A deployable unit of software.
icon: server
properties:
  language:
    title: Language
    type: string
    enum: [go, python, typescript, java]
  tier:
    title: Tier
    type: string
    enum: [critical, standard, experimental]
    required: true
  repository:
    title: Repository
    type: string
    format: url
  tags:
    title: Tags
    type: array
    items:
      type: string
  owner:
    title: Owner
    relation: team
  dependsOn:
    title: Depends On
    relation: service
    many: true
  ownerSlack:
    title: Owner Slack
    lookup: owner.slackChannel
  critical:
    title: Critical
    type: boolean
    compute: .properties.tier == "critical"
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'blueprint', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);
