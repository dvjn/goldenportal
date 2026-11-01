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
  color: violet
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
    basics:
      title: Basics
      type: scorecard
      scorecard: team-basics
  catalog:
    columns: [slackChannel, members, basics, updatedAt]
  entity:
    queries:
      owned:
        blueprint: service
        filter: {property: owner, op: eq, value: $entity}
    tabs:
      services:
        title: Services
        icon: server
        widgets:
          owned:
            type: stat
            title: Services
            value: {query: owned}
            link: {query: owned}
          critical:
            type: stat
            title: Critical
            value:
              query:
                blueprint: service
                filter:
                  and:
                    - {property: owner, op: eq, value: $entity}
                    - {property: tier, op: eq, value: critical}
            link: {query: owned, filter: {property: tier, op: eq, value: critical}}
          readiness:
            type: scorecard
            title: Readiness
            scorecard: service-readiness
            query: owned
            link: {path: /scorecards/service-readiness}
          services:
            type: table
            title: Services
            query: owned
            columns: [readiness, tier, language, lastDeployedAt]
            link: {query: owned}
$yaml$),
  ('service', $yaml$kind: Blueprint
metadata:
  name: service
  title: Service
  description: A deployable unit of software.
  icon: server
  color: blue
spec:
  properties:
    language:
      title: Language
      type: string
      enum: [go, python, typescript, java]
    tier:
      title: Tier
      description: How much depends on the service; critical services page on-call.
      type: string
      enum: [critical, standard, experimental]
      required: true
      display:
        as: badge
        colors: {critical: rose, standard: indigo, experimental: gray}
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
      display:
        empty: No channel
    ownerOncall:
      title: Owner On-call
      lookup: owner.oncall
    critical:
      title: Critical
      type: boolean
      compute: .tier == "critical"
      display:
        hidden: true
    readiness:
      title: Readiness
      type: scorecard
      scorecard: service-readiness
  catalog:
    columns: [language, tier, lastDeployedAt, owner, readiness]
    queries:
      services:
        blueprint: service
      critical:
        filter: {property: critical, op: eq, value: true}
    tabs:
      overview:
        title: Overview
        icon: layout-dashboard
        widgets:
          byTier:
            type: donut
            title: By Tier
            query: services
            groupBy: tier
            link: {query: services, group: true}
          byLanguage:
            type: bar
            title: By Language
            width: 4
            query: services
            groupBy: language
            link: {query: services, group: true}
          readiness:
            type: scorecard
            title: Readiness
            width: 4
            scorecard: service-readiness
            link: {path: /scorecards/service-readiness}
          critical:
            type: table
            title: Critical Services
            query: critical
            columns: [readiness, owner, language, lastDeployedAt]
            sort: -lastDeployedAt
            link: {query: critical}
  entity:
    header:
      badges: [tier]
      link: repository
    sections:
      - title: Ownership
        properties: [owner, ownerSlack, ownerOncall]
      - title: Delivery
        properties: [language, lastDeployedAt, dependsOn, tags, readiness]
    queries:
      deployments:
        blueprint: deployment
        filter: {property: service, op: eq, value: $entity}
      failed:
        blueprint: deployment
        filter:
          and:
            - {property: service, op: eq, value: $entity}
            - {property: status, op: eq, value: failed}
      production:
        blueprint: deployment
        filter:
          and:
            - {property: service, op: eq, value: $entity}
            - {property: environment, op: eq, value: production}
    tabs:
      deployments:
        title: Deployments
        icon: rocket
        widgets:
          total:
            type: stat
            title: Deployments
            icon: rocket
            color: orange
            value: {query: deployments}
            link: {query: deployments}
          succeeded:
            type: stat
            title: Success Rate
            icon: circle-check
            color: emerald
            value:
              query: deployments
              share: {property: status, op: eq, value: succeeded}
          failed:
            type: stat
            title: Failed
            icon: circle-x
            color: red
            value: {query: failed}
            link: {query: failed}
          production:
            type: stat
            title: To Production
            icon: globe
            color: blue
            value: {query: production}
            link: {query: production}
          recent:
            type: table
            title: Recent
            width: 9
            query: deployments
            columns: [environment, status, startedAt]
            sort: -startedAt
            limit: 5
            link: {query: deployments}
          byStatus:
            type: donut
            title: By Status
            width: 3
            query: deployments
            groupBy: status
            link: {query: deployments, group: true}
$yaml$),
  ('user', $yaml$kind: Blueprint
metadata:
  name: user
  title: User
  description: Someone who can sign in to the portal.
  icon: user
  color: sky
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
  color: teal
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
  color: indigo
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
  color: emerald
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
    lastRunOutcome:
      title: Outcome
      type: string
      enum: [succeeded, warnings, failed]
      display:
        as: badge
        colors: {succeeded: emerald, warnings: amber, failed: red}
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
  color: orange
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
      lookup: service.owner.title
  catalog:
    queries:
      deployments:
        blueprint: deployment
      failed:
        filter: {property: status, op: eq, value: failed}
    tabs:
      overview:
        title: Overview
        icon: layout-dashboard
        widgets:
          byStatus:
            type: donut
            title: By Status
            width: 3
            query: deployments
            groupBy: status
            link: {query: deployments, group: true}
          byService:
            type: bar
            title: By Service
            width: 8
            query: deployments
            groupBy: service
            link: {query: deployments, group: true}
          failed:
            type: table
            title: Failed
            query: failed
            columns: [service, environment, version, startedAt]
            sort: -startedAt
            link: {query: failed}
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
          lastRunOutcome: .lastRunOutcome
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

-- Example scorecards. Checks are jq over each entity, flat as paths read
-- it: its properties, relations and lookups beside identifier and title.
-- The service and team blueprints hold each one's results in a property.
WITH example (identifier, source) AS (VALUES
  ('service-readiness', $yaml$kind: Scorecard
metadata:
  name: service-readiness
  title: Service Readiness
  description: What a service needs before people can rely on it.
  icon: shield-check
  color: amber
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
          check: .owner != null
          fix: Set the owning team in the service registry.
        hasRepository:
          title: Has a repository
          check: .repository != null
          fix: Add the repository URL to the service registry.
    - name: silver
      title: Silver
      color: sky
      criteria:
        ownerOnSlack:
          title: Owner has a Slack channel
          check: .ownerSlack != null
          fix: Give the owning team a Slack channel in the team directory.
        tagged:
          title: Tagged
          check: (.tags // []) | length > 0
          fix: Tag the service in the service registry.
        criticalOnCall:
          title: Owner is on call
          when: .critical
          check: .ownerOncall != null
          fix: Critical services need an owning team with an on-call rotation.
    - name: gold
      title: Gold
      color: amber
      criteria:
        deployedRecently:
          title: Deployed in the last 6 hours
          check: (.lastDeployedAt // "1970-01-01T00:00:00Z" | fromdate) > (now - 6 * 3600)
          fix: Ship small changes often.
        dependenciesKnown:
          title: Dependencies are listed
          check: .dependsOn != null
          fix: List what the service calls in the service registry.
$yaml$),
  ('team-basics', $yaml$kind: Scorecard
metadata:
  name: team-basics
  title: Team Basics
  description: How to reach a team.
  icon: contact
  color: lime
spec:
  blueprint: team
  tiers:
    - name: reachable
      title: Reachable
      color: emerald
      criteria:
        slack:
          title: Has a Slack channel
          check: .slackChannel != null
        oncall:
          title: Has an on-call rotation
          check: .oncall != null
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
-- their own catalog and entity pages. A dashboard's inputs narrow only the
-- queries that compare with them as $input.<id>, and an unset input drops
-- its conditions. Services' team input starts at the teams whose members
-- include whoever is looking, found by their email, which is a user
-- entity's identifier, and at every team for someone on none. Deployments
-- have no team of their own, so their queries follow their service's.
WITH example (identifier, source) AS (VALUES
  ('services', $yaml$kind: Dashboard
metadata:
  name: services
  title: Services
  icon: server
  color: blue
spec:
  order: 1
  inputs:
    team:
      title: Team
      type: relation
      relation: team
      multiple: true
      default:
        query:
          blueprint: team
          filter: {property: members, op: contains, value: $me}
        otherwise: everything
    tier:
      title: Tier
      type: enum
      values: [critical, standard, experimental]
      multiple: true
  queries:
    services:
      blueprint: service
      filter:
        and:
          - {property: owner, op: in, values: $input.team}
          - {property: tier, op: in, values: $input.tier}
    teams:
      blueprint: team
      filter: {property: identifier, op: in, values: $input.team}
    deployments:
      blueprint: deployment
      filter:
        and:
          - {property: service.owner, op: in, values: $input.team}
          - {property: service.tier, op: in, values: $input.tier}
    failedRecently:
      blueprint: deployment
      filter:
        and:
          - {property: status, op: eq, value: failed}
          - {property: startedAt, op: within, value: 7d}
          - {property: service.owner, op: in, values: $input.team}
          - {property: service.tier, op: in, values: $input.tier}
  widgets:
    services:
      type: stat
      title: Services
      icon: server
      color: blue
      value: {query: services}
      link: {query: services}
    critical:
      type: stat
      title: Critical Share
      description: Critical services out of all services
      icon: siren
      color: orange
      value:
        query: services
        share: {property: tier, op: eq, value: critical}
      link: {query: services, filter: {property: tier, op: eq, value: critical}}
    teams:
      type: stat
      title: Teams
      icon: users
      color: violet
      value: {query: teams}
      link: {query: teams}
    failed:
      type: stat
      title: Failed Deployments
      description: Last 7 days
      icon: circle-x
      color: red
      value: {query: failedRecently}
      link: {query: failedRecently}
    readiness:
      type: scorecard
      title: Service Readiness
      scorecard: service-readiness
      query: services
      link: {path: /scorecards/service-readiness}
    byOwner:
      type: bar
      title: Services by Owner
      query: services
      groupBy: owner
      link: {query: services, group: true}
    list:
      type: table
      title: Services
      query: services
      columns:
        - readiness
        - {title: Failing Checks, path: readiness.failed}
        - tier
        - lastDeployedAt
      sort: -lastDeployedAt
      limit: 8
      link: {query: services}
    deployments:
      type: table
      title: Recent Deployments
      query: deployments
      columns: [service, environment, status, startedAt]
      sort: -startedAt
      limit: 8
      link: {query: deployments}
$yaml$),
  ('portal', $yaml$kind: Dashboard
metadata:
  name: portal
  title: Portal
  description: What the portal holds and where it comes from.
  icon: database
  color: cyan
spec:
  order: 2
  visibility: {roles: [builder, admin]}
  queries:
    activeUsers:
      blueprint: user
      filter: {property: active, op: eq, value: true}
    integrations:
      blueprint: integration
    failing:
      blueprint: integration
      filter: {property: lastRunOutcome, op: eq, value: failed}
  widgets:
    users:
      type: stat
      title: Active Users
      icon: users
      color: violet
      value: {query: activeUsers}
      link: {query: activeUsers}
    blueprints:
      type: stat
      title: Blueprints
      icon: shapes
      color: blue
      value: {query: {blueprint: blueprint}}
      link: {path: /blueprints}
    integrations:
      type: stat
      title: Integrations
      icon: plug
      color: teal
      value: {query: integrations}
      link: {path: /integrations}
    failing:
      type: stat
      title: Failing Integrations
      icon: circle-x
      color: red
      value: {query: failing}
      link: {query: failing}
    list:
      type: table
      title: Integrations
      query: integrations
      columns: [connector, lastRunOutcome, entities, lastRunAt]
      sort: -entities
      rowLink: {path: '/integrations/{{ .identifier }}'}
      link: {query: integrations}
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'dashboard', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);
