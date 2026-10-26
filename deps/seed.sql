-- Development seed data: the people and groups in deps/authentik/golden-portal.yaml,
-- so the portal has users, groups and roles before anyone signs in, plus example
-- blueprints and the push integrations the mock pusher in deps.dev.yaml feeds.
-- Every insert skips rows that already exist, so this is safe to run again.
-- psql sets :issuer to GP_OIDC_ISSUER, the dev Authentik uses the email as the
-- subject, and :team_token and :service_token are the mock pusher's tokens.

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
  ('team', $yaml$kind: Blueprint
metadata:
  name: team
  title: Team
  description: A group of people who own things in the catalog.
  icon: users
spec:
  properties:
    slackChannel:
      title: Slack Channel
      type: string
      format: uri
    oncall:
      title: On-call Rotation
      type: string
      format: uri
    members:
      title: Members
      relation: user
      many: true
  entity:
    tabs:
      services:
        title: Services
        icon: server
        widgets:
          owned:
            type: stat
            title: Services
            blueprint: service
            filter: {property: owner, op: eq, value: $entity}
          critical:
            type: stat
            title: Critical
            blueprint: service
            filter:
              and:
                - {property: owner, op: eq, value: $entity}
                - {property: tier, op: eq, value: critical}
          readiness:
            type: scorecard
            title: Readiness
            scorecard: service-readiness
            filter: {property: owner, op: eq, value: $entity}
          services:
            type: table
            title: Services
            blueprint: service
            filter: {property: owner, op: eq, value: $entity}
            columns: [tier, language, lastDeployedAt]
$yaml$),
  ('service', $yaml$kind: Blueprint
metadata:
  name: service
  title: Service
  description: A deployable unit of software.
  icon: server
spec:
  properties:
    language:
      title: Language
      type: string
      enum: [go, python, typescript, java]
      display:
        column: true
    tier:
      title: Tier
      type: string
      enum: [critical, standard, experimental]
      required: true
      display:
        as: badge
        colors: {critical: red, standard: blue, experimental: gray}
        column: true
    repository:
      title: Repository
      type: string
      format: uri
    tags:
      title: Tags
      type: array
      items:
        type: string
      display:
        filter: true
    lastDeployedAt:
      title: Last Deployed
      type: string
      format: date-time
      display:
        column: true
    owner:
      title: Owner
      relation: team
      display:
        column: true
    dependsOn:
      title: Depends On
      relation: service
      many: true
    ownerSlack:
      title: Owner Slack
      lookup: owner.slackChannel
    ownerOncall:
      title: Owner On-call
      lookup: owner.oncall
    critical:
      title: Critical
      type: boolean
      compute: .properties.tier == "critical"
      display:
        hidden: true
  catalog:
    tabs:
      overview:
        title: Overview
        icon: layout-dashboard
        widgets:
          byTier:
            type: donut
            title: By Tier
            groupBy: tier
          byLanguage:
            type: bar
            title: By Language
            width: 4
            groupBy: language
          readiness:
            type: scorecard
            title: Readiness
            width: 4
            scorecard: service-readiness
          critical:
            type: table
            title: Critical Services
            filter: {property: critical, op: eq, value: true}
            columns: [owner, language, lastDeployedAt]
            sort: -lastDeployedAt
  entity:
    tabs:
      deployments:
        title: Deployments
        icon: rocket
        widgets:
          about:
            type: markdown
            width: 12
            text: 'Deployments of **{{ $entity.title }}**, owned by {{ $entity.owner }}, as of {{ $today }}.'
          total:
            type: stat
            title: Deployments
            blueprint: deployment
            filter: {property: service, op: eq, value: $entity}
          byStatus:
            type: donut
            title: By Status
            width: 3
            blueprint: deployment
            groupBy: status
            filter: {property: service, op: eq, value: $entity}
          byEnvironment:
            type: bar
            title: By Environment
            blueprint: deployment
            groupBy: environment
            filter: {property: service, op: eq, value: $entity}
          recent:
            type: table
            title: Recent
            blueprint: deployment
            filter: {property: service, op: eq, value: $entity}
            columns: [environment, version, status, startedAt]
            sort: -startedAt
$yaml$),
  ('user', $yaml$kind: Blueprint
metadata:
  name: user
  title: User
  description: Someone who can sign in to the portal.
  icon: user
spec:
  properties:
    email:
      title: Email
      type: string
      format: email
    active:
      title: Active
      type: boolean
    lastSignedIn:
      title: Last Signed In
      type: string
      format: date-time
    groups:
      title: Groups
      relation: group
      many: true
$yaml$),
  ('group', $yaml$kind: Blueprint
metadata:
  name: group
  title: Group
  description: A group from the identity provider.
  icon: users-round
spec:
  properties:
    members:
      title: Members
      relation: user
      many: true
$yaml$),
  ('blueprint', $yaml$kind: Blueprint
metadata:
  name: blueprint
  title: Blueprint
  description: A kind of entity in the catalog.
  icon: shapes
spec:
  properties:
    description:
      title: Description
      type: string
    version:
      title: Version
      type: integer
    entities:
      title: Entities
      type: integer
    relatesTo:
      title: Relates To
      relation: blueprint
      many: true
$yaml$),
  ('integration', $yaml$kind: Blueprint
metadata:
  name: integration
  title: Integration
  description: Where entities come from.
  icon: plug
spec:
  properties:
    connector:
      title: Connector
      type: string
    version:
      title: Version
      type: integer
    entities:
      title: Entities
      type: integer
    lastRunAt:
      title: Last Run
      type: string
      format: date-time
    writes:
      title: Writes
      relation: blueprint
      many: true
$yaml$),
  ('deployment', $yaml$kind: Blueprint
metadata:
  name: deployment
  title: Deployment
  description: A release of a service to an environment.
  icon: rocket
spec:
  properties:
    service:
      title: Service
      relation: service
      required: true
    environment:
      title: Environment
      type: string
      enum: [production, staging]
      display:
        as: badge
        colors: {production: violet, staging: gray}
    version:
      title: Version
      type: string
    status:
      title: Status
      type: string
      enum: [running, succeeded, failed]
      display:
        as: badge
        colors: {running: sky, succeeded: emerald, failed: red}
    startedAt:
      title: Started
      type: string
      format: date-time
    finishedAt:
      title: Finished
      type: string
      format: date-time
    owner:
      title: Owner
      lookup: service.owner.$title
  catalog:
    tabs:
      overview:
        title: Overview
        icon: layout-dashboard
        widgets:
          byStatus:
            type: donut
            title: By Status
            groupBy: status
          byService:
            type: bar
            title: By Service
            width: 8
            groupBy: service
          failed:
            type: table
            title: Failed
            filter: {property: status, op: eq, value: failed}
            columns: [service, environment, version, startedAt]
            sort: -startedAt
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'blueprint', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);

-- Example integrations: two push integrations, where each mapping is jq over
-- the JSON its system sends; the Portal, which syncs the portal's own objects;
-- and an http integration pulling from the mock deploy tracker.
WITH example (identifier, source) AS (VALUES
  ('team-directory', $yaml$kind: Integration
metadata:
  name: team-directory
  title: Team Directory
  description: Teams and their channels, from the team directory.
spec:
  connector:
    type: push
  mappings:
    - blueprint: team
      items: .teams[]
      entity:
        identifier: .slug
        title: .name
        properties:
          slackChannel: .links.slack
          oncall: .links.oncall
          members: .members
$yaml$),
  ('service-registry', $yaml$kind: Integration
metadata:
  name: service-registry
  title: Service Registry
  description: Services from the deployment registry. Retired ones are left out.
spec:
  connector:
    type: push
  mappings:
    - blueprint: service
      items: .services[]
      filter: .lifecycle != "retired"
      entity:
        identifier: .name
        title: .displayName
        properties:
          language: .language
          tier: .tier
          repository: .repo
          tags: .tags
          lastDeployedAt: .lastDeploy
          owner: .owner
          dependsOn: .dependencies
$yaml$),
  ('portal', $yaml$kind: Integration
metadata:
  name: portal
  title: Portal
  description: The portal's own users, groups, blueprints and integrations.
spec:
  connector:
    type: portal
  sync:
    interval: 1m
  mappings:
    - resource: users
      blueprint: user
      entity:
        identifier: .email
        title: .displayName
        properties:
          email: .email
          active: .active
          lastSignedIn: .lastSignedIn
          groups: .groups
    - resource: groups
      blueprint: group
      entity:
        identifier: .name
        title: .name
        properties:
          members: .members
    - resource: blueprints
      blueprint: blueprint
      entity:
        identifier: .identifier
        title: .title
        properties:
          description: .description
          version: .version
          entities: .entities
          relatesTo: .relations
    - resource: integrations
      blueprint: integration
      entity:
        identifier: .identifier
        title: .title
        properties:
          connector: .connector
          version: .version
          entities: .entities
          lastRunAt: .lastRunAt
          writes: .blueprints
$yaml$),
  ('deploy-tracker', $yaml$kind: Integration
metadata:
  name: deploy-tracker
  title: Deploy Tracker
  description: Deployments, pulled from the deploy tracker's API.
spec:
  connector:
    type: http
    config:
      resources:
        deployments:
          url: http://localhost:47801/deployments/1.json
          next: .next
          headers:
            Authorization: {env: GP_SECRET_DEPLOY_TRACKER}
  sync:
    interval: 1m
  mappings:
    - resource: deployments
      blueprint: deployment
      items: .deployments[]
      entity:
        identifier: .id
        title: .service + " " + .version
        properties:
          service: .service
          environment: .environment
          version: .version
          status: .status
          startedAt: .startedAt
          finishedAt: .finishedAt
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'integration', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);

INSERT INTO ingestion_tokens (integration_id, name, prefix, secret_hash)
SELECT d.id, 'Mock Pusher', left(t.token, 12), sha256(convert_to(t.token, 'UTF8'))
FROM (VALUES
  ('team-directory', :'team_token'),
  ('service-registry', :'service_token')
) AS t (identifier, token)
JOIN config_documents d ON d.kind = 'integration' AND d.identifier = t.identifier
ON CONFLICT (secret_hash) DO NOTHING;

-- Example scorecards. Checks are jq over each entity, with its relations and
-- lookups as properties.
WITH example (identifier, source) AS (VALUES
  ('service-readiness', $yaml$kind: Scorecard
metadata:
  name: service-readiness
  title: Service Readiness
  description: What a service needs before people can rely on it.
  icon: shield-check
spec:
  blueprint: service
  filter: {property: tier, op: ne, value: experimental}
  tiers:
    - name: bronze
      title: Bronze
      color: orange
      criteria:
        hasOwner:
          title: Has an owner
          check: .properties.owner != null
          fix: Set the owning team in the service registry.
        hasRepository:
          title: Has a repository
          check: .properties.repository != null
          fix: Add the repository URL to the service registry.
    - name: silver
      title: Silver
      color: sky
      criteria:
        ownerOnSlack:
          title: Owner has a Slack channel
          check: .properties.ownerSlack != null
          fix: Give the owning team a Slack channel in the team directory.
        tagged:
          title: Tagged
          check: (.properties.tags // []) | length > 0
          fix: Tag the service in the service registry.
        criticalOnCall:
          title: Owner is on call
          when: .properties.critical
          check: .properties.ownerOncall != null
          fix: Critical services need an owning team with an on-call rotation.
    - name: gold
      title: Gold
      color: amber
      criteria:
        deployedRecently:
          title: Deployed in the last 6 hours
          check: (.properties.lastDeployedAt // "1970-01-01T00:00:00Z" | fromdate) > (now - 6 * 3600)
          fix: Ship small changes often.
        dependenciesKnown:
          title: Dependencies are listed
          check: .properties.dependsOn != null
          fix: List what the service calls in the service registry.
$yaml$),
  ('team-basics', $yaml$kind: Scorecard
metadata:
  name: team-basics
  title: Team Basics
  description: How to reach a team.
  icon: contact
spec:
  blueprint: team
  tiers:
    - name: reachable
      title: Reachable
      color: emerald
      criteria:
        slack:
          title: Has a Slack channel
          check: .properties.slackChannel != null
        oncall:
          title: Has an on-call rotation
          check: .properties.oncall != null
          fix: Add the team's on-call rotation to the team directory.
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'scorecard', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);

-- Example dashboards, each a tab on Home. The blueprints above lay out
-- their own catalog and entity pages. The overview's filters narrow its
-- widgets to a team and tiers; unset, they match everything. My Services
-- starts its team filter at the teams whose members include whoever is
-- looking, found by their email, which is a user entity's identifier, and
-- greets them by name with {{ $me.name }}. Its last widget compares with
-- $me.groups instead: the dev teams are named after the identity
-- provider's groups, so it counts the services of the groups they're in.
WITH example (identifier, source) AS (VALUES
  ('overview', $yaml$kind: Dashboard
metadata:
  name: overview
  title: Overview
  description: How the services are doing.
  icon: layout-dashboard
spec:
  order: 1
  filters:
    team:
      title: Team
      type: relation
      relation: team
    tier:
      title: Tier
      type: enum
      values: [critical, standard, experimental]
      multiple: true
  widgets:
    services:
      type: stat
      title: Services
      blueprint: service
      filter:
        and:
          - {property: owner, op: eq, value: $filter.team}
          - {property: tier, op: eq, value: $filter.tier}
    critical:
      type: stat
      title: Critical Services
      blueprint: service
      filter:
        and:
          - {property: owner, op: eq, value: $filter.team}
          - {property: tier, op: eq, value: critical}
    teams:
      type: stat
      title: Teams
      blueprint: team
    failed:
      type: stat
      title: Failed Deployments
      blueprint: deployment
      filter: {property: status, op: eq, value: failed}
    readiness:
      type: scorecard
      title: Service Readiness
      scorecard: service-readiness
      filter:
        and:
          - {property: owner, op: eq, value: $filter.team}
          - {property: tier, op: eq, value: $filter.tier}
    byOwner:
      type: bar
      title: Services by Owner
      blueprint: service
      groupBy: owner
      filter: {property: tier, op: eq, value: $filter.tier}
    deployments:
      type: table
      title: Recent Deployments
      blueprint: deployment
      columns: [service, environment, status, startedAt]
      sort: -startedAt
      limit: 8
$yaml$),
  ('mine', $yaml$kind: Dashboard
metadata:
  name: mine
  title: My Services
  description: The services of the teams you're in.
  icon: user-check
spec:
  order: 2
  defaultFor: {groups: [payments, search]}
  filters:
    team:
      title: Team
      type: relation
      relation: team
      multiple: true
      default:
        from: team
        where: {property: members, op: contains, value: $me}
  widgets:
    hello:
      type: markdown
      width: 12
      text: |
        **Hello {{ $me.name }}.** These are the services of the teams you're in.
        Pick other teams above to look around.
    services:
      type: stat
      title: Services
      blueprint: service
      filter: {property: owner, op: eq, value: $filter.team}
    critical:
      type: stat
      title: Critical Services
      blueprint: service
      filter:
        and:
          - {property: owner, op: eq, value: $filter.team}
          - {property: tier, op: eq, value: critical}
    readiness:
      type: scorecard
      title: Service Readiness
      scorecard: service-readiness
      filter: {property: owner, op: eq, value: $filter.team}
    list:
      type: table
      title: Services
      blueprint: service
      filter: {property: owner, op: eq, value: $filter.team}
      columns: [owner, $scorecard.service-readiness, tier, language, lastDeployedAt]
      sort: -lastDeployedAt
    groupServices:
      type: stat
      title: Services of My Groups
      description: Teams named after the groups you're in.
      blueprint: service
      filter: {property: owner, op: eq, value: $me.groups}
$yaml$),
  ('portal', $yaml$kind: Dashboard
metadata:
  name: portal
  title: Portal
  description: What the portal holds and where it comes from.
  icon: database
spec:
  order: 3
  visibility: {roles: [builder, admin]}
  widgets:
    welcome:
      type: markdown
      width: 4
      text: |
        **Welcome to the portal.** Everything here comes from integrations,
        so fix data at its source. Builders can change these dashboards
        under Dashboards.
    users:
      type: stat
      title: Active Users
      width: 4
      blueprint: user
      filter: {property: active, op: eq, value: true}
    blueprints:
      type: stat
      title: Blueprints
      width: 4
      blueprint: blueprint
    integrations:
      type: table
      title: Integrations
      blueprint: integration
      columns: [connector, entities, lastRunAt]
      sort: -entities
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'dashboard', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);
