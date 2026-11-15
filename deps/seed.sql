-- Development seed data: the people in deps/mock/mock.yaml with their groups,
-- so the portal has users, groups and roles before anyone signs in, plus example
-- blueprints and the connections the mock in deps.dev.yaml feeds.
-- Every insert skips rows that already exist, so this is safe to run again.
-- psql sets :issuer to GP_OIDC_ISSUER, the mock sign-in uses the email as the
-- subject, and :team_token is the mock's token for pushing to the team directory.

INSERT INTO groups (name)
VALUES ('platform-engineers'), ('payments'), ('search')
ON CONFLICT (name) DO NOTHING;

INSERT INTO users (oidc_issuer, oidc_subject, email, display_name)
VALUES
  (:'issuer', 'admin@example.com', 'admin@example.com', 'Dev Admin'),
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
  identifier: team
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
  sources:
    - connection: team-directory
      resource: teams
      entity:
        identifier: .slug
        title: .name
        properties:
          slackChannel: .links.slack
          oncall: .links.oncall
          members: .members
  catalog:
    columns: [slackChannel, members, basics, updatedAt]
  entity:
    queries:
      owned:
        blueprint: service
        filter: {property: owner, op: eq, value: $entity}
      criticalOwned:
        blueprint: service
        filter:
          and:
            - {property: owner, op: eq, value: $entity}
            - {property: tier, op: eq, value: critical}
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
            value: {query: criticalOwned}
            link: {query: owned, filter: {property: tier, op: eq, value: critical}}
          readiness:
            type: scorecard
            title: Readiness
            scorecard: service-readiness
            query: owned
            link: {path: /catalog/service?view=scorecard-service-readiness}
          services:
            type: table
            title: Services
            query: owned
            columns: [{path: readiness}, {path: tier}, {path: language}, {path: lastDeployedAt}]
            link: {query: owned}
$yaml$),
  ('service', $yaml$kind: Blueprint
metadata:
  identifier: service
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
    tagged:
      title: Tagged
      type: boolean
      compute: (.tags // []) | length > 0
      display:
        hidden: true
    readiness:
      title: Readiness
      type: scorecard
      scorecard: service-readiness
  sources:
    - connection: service-registry
      resource: services
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
            link: {path: /catalog/service?view=scorecard-service-readiness}
          critical:
            type: table
            title: Critical Services
            query: critical
            columns: [{path: readiness}, {path: owner}, {path: language}, {path: lastDeployedAt}]
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
      incidents:
        blueprint: incident
        filter: {property: service, op: eq, value: $entity}
      openIncidents:
        blueprint: incident
        filter:
          and:
            - {property: service, op: eq, value: $entity}
            - {property: status, op: eq, value: open}
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
            columns: [{path: environment}, {path: status}, {path: startedAt}]
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
      incidents:
        title: Incidents
        icon: siren
        widgets:
          open:
            type: stat
            title: Open
            icon: siren
            color: rose
            value: {query: openIncidents}
            link: {query: openIncidents}
          total:
            type: stat
            title: Incidents
            icon: history
            color: gray
            value: {query: incidents}
            link: {query: incidents}
          respond:
            type: actions
            title: Respond
            width: 6
            actions: [open-incident, restart, scale]
            workflows: [restart-services]
          bySeverity:
            type: donut
            title: By Severity
            query: incidents
            groupBy: severity
            link: {query: incidents, group: true}
          list:
            type: table
            title: Incidents
            width: 8
            query: incidents
            columns: [{path: severity}, {path: status}, {path: openedBy}, {path: openedAt}, {path: resolvedAt}]
            sort: -openedAt
            limit: 8
            link: {query: incidents}
$yaml$),
  ('user', $yaml$kind: Blueprint
metadata:
  identifier: user
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
  sources:
    - connection: portal
      resource: users
      entity:
        identifier: .email
        title: .displayName
        properties:
          email: .email
          active: .active
          lastSignedIn: .lastSignedInAt
          groups: .groups
$yaml$),
  ('group', $yaml$kind: Blueprint
metadata:
  identifier: group
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
  sources:
    - connection: portal
      resource: groups
      entity:
        identifier: .name
        title: .name
        properties:
          members: .members
$yaml$),
  ('blueprint', $yaml$kind: Blueprint
metadata:
  identifier: blueprint
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
  sources:
    - connection: portal
      resource: blueprints
      entity:
        identifier: .identifier
        title: .title
        properties:
          description: .description
          version: .version
          entities: .entities
          relatesTo: .relations
$yaml$),
  ('connection', $yaml$kind: Blueprint
metadata:
  identifier: connection
  title: Connection
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
  sources:
    - connection: portal
      resource: connections
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
  ('incident', $yaml$kind: Blueprint
metadata:
  identifier: incident
  title: Incident
  description: Something broken that people are fixing. The Open incident action adds them.
  icon: siren
  color: rose
spec:
  properties:
    severity:
      title: Severity
      type: string
      enum: [sev1, sev2, sev3]
      required: true
      display:
        as: badge
        colors: {sev1: rose, sev2: orange, sev3: amber}
    status:
      title: Status
      type: string
      enum: [open, resolved]
      required: true
      display:
        as: badge
        colors: {open: rose, resolved: emerald}
    service:
      title: Service
      relation: service
    channel:
      title: Channel
      type: string
      format: uri
    openedBy:
      title: Opened By
      relation: user
    openedAt:
      title: Opened
      type: string
      format: date-time
    resolvedAt:
      title: Resolved
      type: string
      format: date-time
  sources:
    - connection: incidents
      resource: incidents
      entity:
        identifier: .id
        title: .summary
        properties:
          severity: .severity
          status: .status
          service: .service
          channel: .channel
  catalog:
    columns: [severity, status, service, openedBy, openedAt]
$yaml$),
  ('action', $yaml$kind: Blueprint
metadata:
  identifier: action
  title: Action
  description: Something people run on an entity from its page, such as a restart. The Portal connection keeps these up to date.
  icon: zap
  color: amber
spec:
  properties:
    description:
      title: Description
      type: string
    runsOn:
      title: Runs On
      relation: blueprint
    version:
      title: Version
      type: integer
    steps:
      title: Steps
      type: integer
    approvals:
      title: Approval Steps
      type: integer
    runs:
      title: Runs
      description: In the last 30 days.
      type: integer
    lastRunAt:
      title: Last Run
      type: string
      format: date-time
    lastRunStatus:
      title: Last Run Status
      type: string
      enum: [queued, running, waiting, succeeded, failed, rejected, cancelled, interrupted]
      display:
        as: badge
        colors: {queued: gray, running: blue, waiting: indigo, succeeded: emerald, failed: red, rejected: rose, cancelled: gray, interrupted: amber}
  sources:
    - connection: portal
      resource: actions
      entity:
        identifier: .identifier
        title: .title
        properties:
          description: .description
          runsOn: .blueprint
          version: .version
          steps: .steps
          approvals: .approvals
          runs: .runs
          lastRunAt: .lastRunAt
          lastRunStatus: .lastRunStatus
  catalog:
    columns: [runsOn, steps, approvals, runs, lastRunStatus, lastRunAt]
$yaml$),
  ('run', $yaml$kind: Blueprint
metadata:
  identifier: run
  title: Run
  description: One run of an action in the last 30 days, without its inputs or outputs. The Portal connection keeps these up to date.
  icon: play
  color: sky
spec:
  properties:
    action:
      title: Action
      relation: action
    entityBlueprint:
      title: Entity Blueprint
      relation: blueprint
    entity:
      title: Entity
      type: string
    requester:
      title: Requested By
      relation: user
    via:
      title: Via
      type: string
      enum: [web, mcp]
      display:
        as: badge
        colors: {web: gray, mcp: violet}
    status:
      title: Status
      type: string
      enum: [queued, running, waiting, succeeded, failed, rejected, cancelled, interrupted]
      required: true
      display:
        as: badge
        colors: {queued: gray, running: blue, waiting: indigo, succeeded: emerald, failed: red, rejected: rose, cancelled: gray, interrupted: amber}
    startedAt:
      title: Started
      type: string
      format: date-time
    finishedAt:
      title: Finished
      type: string
      format: date-time
    seconds:
      title: Took
      type: number
      display:
        unit: s
  sources:
    - connection: portal
      resource: runs
      entity:
        identifier: .id
        title: .title + " on " + .entityTitle
        properties:
          action: .action
          entityBlueprint: .blueprint
          entity: .entityTitle
          requester: .requester
          via: .via
          status: .status
          startedAt: .createdAt
          finishedAt: .finishedAt
          seconds: .seconds
  catalog:
    columns: [action, entity, requester, status, startedAt, seconds]
$yaml$),
  ('deployment', $yaml$kind: Blueprint
metadata:
  identifier: deployment
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
    seconds:
      title: Took
      type: number
      compute: if .finishedAt then (.finishedAt | fromdateiso8601) - (.startedAt | fromdateiso8601) else null end
      display:
        unit: s
    approvalRequired:
      title: Approval Required
      description: Whether its environment's rules ask for approval before it runs; empty where GitLab says nothing of approvals.
      type: boolean
    approvedBy:
      title: Approved By
      description: Who approved it.
      type: array
      items:
        type: string
    approvedAt:
      title: Approved
      description: When the last approval it needed was given; empty while it waits for one, or if one was a rejection.
      type: string
      format: date-time
    approvalSeconds:
      title: Waited for Approval
      description: From when it was created, as its job became ready to run, until it was approved.
      type: number
      display:
        unit: s
    waitSeconds:
      title: Waited to Start
      description: From when it was created until its job started, approval, a manual play and the runner queue included.
      type: number
      display:
        unit: s
    owner:
      title: Owner
      lookup: service.owner.title
  sources:
    - connection: deploy-tracker
      resource: deployments
      mode: upsert
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
    # GitLab's deployments of the services, beside the deploy tracker's.
    - connection: gitlab
      resource: deployments
      filter: .environment.name == "production" or .environment.name == "staging"
      mode: upsert
      entity:
        identifier: '"gitlab-" + (.id | tostring)'
        title: (._gp.project | split("/") | last) + " " + .sha[0:8]
        properties:
          service: ._gp.project | split("/") | last
          environment: .environment.name
          version: .sha[0:8]
          status: '{"success": "succeeded", "failed": "failed", "canceled": "failed"}[.status] // "running"'
          startedAt: .created_at
          finishedAt: if .status | IN("success", "failed", "canceled") then .finished_at // .updated_at else null end
          approvalRequired: ._gp.approvalRequired
          approvedBy: ._gp.approvals | values | map(select(.status == "approved") | .user.username)
          approvedAt: ._gp.approvedAt
          approvalSeconds: ._gp.approvalSeconds
          waitSeconds: ._gp.waitSeconds
  # The mocks have a fortnight of history, so a week shows older rows
  # leaving the catalog, but for production's, kept a month.
  retention:
    by: startedAt
    keep: 7d
    rules:
      - filter: {property: environment, op: eq, value: production}
        keep: 30d
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
            columns: [{path: service}, {path: environment}, {path: version}, {path: startedAt}]
            sort: -startedAt
            link: {query: failed}
$yaml$),
  ('repository', $yaml$kind: Blueprint
metadata:
  identifier: repository
  title: Repository
  description: A source repository.
  icon: folder-git-2
  color: gray
spec:
  properties:
    service:
      title: Service
      description: The service built from it, which shares its name.
      relation: service
    description:
      title: Description
      type: string
    visibility:
      title: Visibility
      type: string
      enum: [public, internal, private]
      display:
        as: badge
        colors: {public: amber, internal: indigo, private: gray}
    defaultBranch:
      title: Default Branch
      type: string
    archived:
      title: Archived
      type: boolean
    ci:
      title: CI
      description: How the default branch's latest pipeline went.
      type: string
      enum: [created, waiting_for_resource, preparing, pending, running, success, failed, canceled, skipped, manual, scheduled]
      display:
        as: badge
        colors: {running: sky, pending: gray, success: emerald, failed: red, canceled: gray, manual: amber}
    ciValid:
      title: CI Config Valid
      description: Whether the default branch's CI configuration is valid, or empty without one.
      type: boolean
    ciStages:
      title: CI Stages
      type: array
      items:
        type: string
    defaultBranchProtected:
      title: Default Branch Protected
      type: boolean
    url:
      title: URL
      type: string
      format: uri
    lastActivityAt:
      title: Last Activity
      type: string
      format: date-time
  sources:
    # A project marked for deletion is on its way out already, and one
    # without a repository has nothing to build.
    - connection: gitlab
      resource: projects
      filter: .marked_for_deletion_on == null and .empty_repo != true
      entity:
        identifier: .path_with_namespace
        title: .name
        properties:
          service: .path
          description: .description
          visibility: .visibility
          defaultBranch: .default_branch
          archived: .archived
          ci: ._gp.latestPipeline.status
          ciValid: ._gp.ciConfig.valid
          ciStages: '[._gp.ciConfig.jobs[]?.stage] | unique'
          defaultBranchProtected: ._gp.defaultBranch.protected
          url: .web_url
          lastActivityAt: .last_activity_at
  catalog:
    columns: [service, visibility, ci, defaultBranchProtected, lastActivityAt]
$yaml$),
  ('merge-request', $yaml$kind: Blueprint
metadata:
  identifier: merge-request
  title: Merge Request
  description: A proposed change to a repository.
  icon: git-pull-request
  color: violet
spec:
  properties:
    repo:
      title: Repository
      relation: repository
      required: true
    state:
      title: State
      type: string
      enum: [opened, draft, merged, closed, locked]
      display:
        as: badge
        colors: {opened: emerald, draft: gray, merged: violet, closed: rose, locked: amber}
    author:
      title: Author
      type: string
    branch:
      title: Branch
      type: string
    url:
      title: URL
      type: string
      format: uri
    openedAt:
      title: Opened
      type: string
      format: date-time
    closedAt:
      title: Closed
      description: When it was merged or closed, empty while it's open.
      type: string
      format: date-time
    lastActivityAt:
      title: Last Activity
      type: string
      format: date-time
  sources:
    # Merge requests, pipelines and deployments pile up, so syncs read what
    # changed since the last one, and their blueprints' retention deletes
    # the old ones.
    - connection: gitlab
      resource: merge_requests
      mode: upsert
      entity:
        identifier: ._gp.project + "!" + (.iid | tostring)
        title: .title
        properties:
          repo: ._gp.project
          state: if .state == "opened" and .draft then "draft" else .state end
          author: .author.username
          branch: .source_branch
          url: .web_url
          openedAt: .created_at
          closedAt: .merged_at // .closed_at
          lastActivityAt: .updated_at
  # Open merge requests have no closedAt, so they're kept however old.
  retention:
    by: closedAt
    keep: 7d
  catalog:
    columns: [repo, state, author, lastActivityAt]
$yaml$),
  ('namespace', $yaml$kind: Blueprint
metadata:
  identifier: namespace
  title: Namespace
  description: A group of repositories, such as a team's.
  icon: folder-tree
  color: gray
spec:
  properties:
    parent:
      title: Parent
      relation: namespace
    description:
      title: Description
      type: string
    visibility:
      title: Visibility
      type: string
      enum: [public, internal, private]
    url:
      title: URL
      type: string
      format: uri
  sources:
    - connection: gitlab
      resource: groups
      entity:
        identifier: .full_path
        title: .name
        properties:
          parent: .full_path | split("/") | .[:-1] | join("/") | select(. != "")
          description: .description
          visibility: .visibility
          url: .web_url
  catalog:
    columns: [parent, visibility]
$yaml$),
  ('pipeline', $yaml$kind: Blueprint
metadata:
  identifier: pipeline
  title: Pipeline
  description: A CI/CD pipeline run of a repository.
  icon: workflow
  color: teal
spec:
  properties:
    repo:
      title: Repository
      relation: repository
      required: true
    ref:
      title: Ref
      type: string
    source:
      title: Trigger
      type: string
    status:
      title: Status
      type: string
      enum: [created, waiting_for_resource, preparing, pending, running, success, failed, canceled, skipped, manual, scheduled]
      display:
        as: badge
        colors:
          created: gray
          waiting_for_resource: gray
          preparing: gray
          pending: gray
          running: sky
          success: emerald
          failed: red
          canceled: gray
          skipped: gray
          manual: amber
          scheduled: gray
    triggeredAt:
      title: Triggered
      type: string
      format: date-time
    url:
      title: URL
      type: string
      format: uri
  sources:
    - connection: gitlab
      resource: pipelines
      mode: upsert
      entity:
        identifier: .id
        title: '(._gp.project | split("/") | last) + " " + .ref + " #" + (.iid // .id | tostring)'
        properties:
          repo: ._gp.project
          ref: .ref
          source: .source
          status: .status
          triggeredAt: .created_at
          url: .web_url
  retention:
    by: triggeredAt
    keep: 7d
  catalog:
    columns: [repo, ref, status, triggeredAt]
$yaml$),
  ('job-attempt', $yaml$kind: Blueprint
metadata:
  identifier: job-attempt
  title: Job Attempt
  description: One run of a CI job in a pipeline; retrying the job runs another.
  icon: play
  color: teal
spec:
  properties:
    pipeline:
      title: Pipeline
      relation: pipeline
      required: true
    repo:
      title: Repository
      relation: repository
      required: true
    runner:
      title: Runner
      relation: runner
    name:
      title: Job
      type: string
    stage:
      title: Stage
      type: string
    status:
      title: Status
      type: string
      enum: [created, waiting_for_resource, preparing, pending, running, success, failed, canceling, canceled, skipped, manual, scheduled]
      display:
        as: badge
        colors: {running: sky, pending: gray, success: emerald, failed: red, canceled: gray, skipped: gray, manual: amber}
    failureReason:
      title: Failure Reason
      type: string
    allowFailure:
      title: Allowed to Fail
      type: boolean
    attempt:
      title: Attempt
      type: number
    retried:
      title: Retried
      description: Whether a later attempt of the same job replaced it.
      type: boolean
    seconds:
      title: Took
      type: number
      display:
        unit: s
    queuedSeconds:
      title: Queued
      type: number
      display:
        unit: s
    waitSeconds:
      title: Waited
      description: From when it was created, with its pipeline or on a retry, until it queued for a runner. Includes waiting for earlier stages or needed jobs, for a manual play, a delay or a resource group; excludes the runner queue and the run.
      type: number
      display:
        unit: s
    stageWaitSeconds:
      title: Waited for Earlier Jobs
      description: The part of the wait until the jobs it waited for, in earlier stages or that it needs, had finished; all of it for a job that isn't manual.
      type: number
      display:
        unit: s
    playWaitSeconds:
      title: Waited for Play
      description: For a manual job, from when the jobs it waited for had finished until someone played it, deployment approval included; 0 for any other job.
      type: number
      display:
        unit: s
    manual:
      title: Manual
      description: Whether it's a manual job, which someone plays.
      type: boolean
    playedBy:
      title: Played By
      description: Who played it, for a manual job.
      type: string
    tags:
      title: Runner Tags
      description: The tags the job asked its runner to have.
      type: array
      items:
        type: string
    ref:
      title: Ref
      type: string
    commit:
      title: Commit
      type: string
    addedAt:
      title: Job Created
      description: When GitLab created it, with its pipeline or on a retry.
      type: string
      format: date-time
    queuedAt:
      title: Job Queued
      description: When it queued for a runner, once what it waited for was done.
      type: string
      format: date-time
    startedAt:
      title: Started
      type: string
      format: date-time
    finishedAt:
      title: Finished
      type: string
      format: date-time
    triggeredAt:
      title: Triggered
      description: When its pipeline was triggered, so it leaves with its pipeline.
      type: string
      format: date-time
    url:
      title: URL
      type: string
      format: uri
  sources:
    # Every attempt of every job, numbered under _gp, and each job as its
    # latest attempt left it.
    - connection: gitlab
      resource: job_attempts
      mode: upsert
      entity:
        identifier: .id
        title: '.name + " #" + (._gp.attempt | tostring)'
        properties:
          pipeline: .pipeline.id | tostring
          repo: ._gp.project
          runner: .runner.id | values | tostring
          name: .name
          stage: .stage
          status: .status
          failureReason: .failure_reason
          allowFailure: .allow_failure
          attempt: ._gp.attempt
          retried: ._gp.retried
          seconds: .duration
          queuedSeconds: .queued_duration
          addedAt: .created_at
          queuedAt: ._gp.queuedAt
          waitSeconds: ._gp.waitSeconds
          stageWaitSeconds: ._gp.stageWaitSeconds
          playWaitSeconds: ._gp.playWaitSeconds
          manual: ._gp.manual
          playedBy: ._gp.playedBy.username
          tags: .tag_list
          ref: .ref
          commit: .commit.id
          startedAt: .started_at
          finishedAt: .finished_at
          triggeredAt: .pipeline.created_at
          url: .web_url
  retention:
    by: triggeredAt
    keep: 7d
  catalog:
    columns: [name, repo, status, attempt, seconds, runner, startedAt]
$yaml$),
  ('job', $yaml$kind: Blueprint
metadata:
  identifier: job
  title: Job
  description: A CI job in a pipeline, as its latest attempt left it, with how many attempts it took.
  icon: list-checks
  color: teal
spec:
  properties:
    pipeline:
      title: Pipeline
      relation: pipeline
      required: true
    repo:
      title: Repository
      relation: repository
      required: true
    runner:
      title: Runner
      relation: runner
    name:
      title: Job
      type: string
    stage:
      title: Stage
      type: string
    status:
      title: Status
      type: string
      enum: [created, waiting_for_resource, preparing, pending, running, success, failed, canceling, canceled, skipped, manual, scheduled]
      display:
        as: badge
        colors: {running: sky, pending: gray, success: emerald, failed: red, canceled: gray, skipped: gray, manual: amber}
    failureReason:
      title: Failure Reason
      type: string
    attempts:
      title: Attempts
      type: number
    failedAttempts:
      title: Failed Attempts
      type: number
    flaky:
      title: Flaky
      description: Whether it failed and then passed when retried in the same pipeline.
      type: boolean
    seconds:
      title: Took
      type: number
      display:
        unit: s
    queuedSeconds:
      title: Queued
      type: number
      display:
        unit: s
    waitSeconds:
      title: Waited
      description: From when it was created, with its pipeline or on a retry, until it queued for a runner. Includes waiting for earlier stages or needed jobs, for a manual play, a delay or a resource group; excludes the runner queue and the run.
      type: number
      display:
        unit: s
    stageWaitSeconds:
      title: Waited for Earlier Jobs
      description: The part of the wait until the jobs it waited for, in earlier stages or that it needs, had finished; all of it for a job that isn't manual.
      type: number
      display:
        unit: s
    playWaitSeconds:
      title: Waited for Play
      description: For a manual job, from when the jobs it waited for had finished until someone played it, deployment approval included; 0 for any other job.
      type: number
      display:
        unit: s
    manual:
      title: Manual
      description: Whether it's a manual job, which someone plays.
      type: boolean
    playedBy:
      title: Played By
      description: Who played it, for a manual job.
      type: string
    addedAt:
      title: Job Created
      description: When GitLab created it, with its pipeline or on a retry.
      type: string
      format: date-time
    queuedAt:
      title: Job Queued
      description: When it queued for a runner, once what it waited for was done.
      type: string
      format: date-time
    finishedAt:
      title: Finished
      type: string
      format: date-time
    triggeredAt:
      title: Triggered
      description: When its pipeline was triggered, so it leaves with its pipeline.
      type: string
      format: date-time
    url:
      title: URL
      type: string
      format: uri
  sources:
    - connection: gitlab
      resource: jobs
      mode: upsert
      entity:
        identifier: (.pipeline.id | tostring) + ":" + .name
        title: .name
        properties:
          pipeline: .pipeline.id | tostring
          repo: ._gp.project
          runner: .runner.id | values | tostring
          name: .name
          stage: .stage
          status: .status
          failureReason: .failure_reason
          attempts: ._gp.attempts
          failedAttempts: ._gp.failedAttempts
          flaky: ._gp.flaky
          seconds: .duration
          queuedSeconds: .queued_duration
          addedAt: .created_at
          queuedAt: ._gp.queuedAt
          waitSeconds: ._gp.waitSeconds
          stageWaitSeconds: ._gp.stageWaitSeconds
          playWaitSeconds: ._gp.playWaitSeconds
          manual: ._gp.manual
          playedBy: ._gp.playedBy.username
          finishedAt: .finished_at
          triggeredAt: .pipeline.created_at
          url: .web_url
  retention:
    by: triggeredAt
    keep: 7d
  catalog:
    columns: [name, repo, stage, status, attempts, flaky, seconds, waitSeconds, finishedAt]
$yaml$),
  ('runner', $yaml$kind: Blueprint
metadata:
  identifier: runner
  title: Runner
  description: A CI runner that jobs ran on, as GitLab describes it, with the jobs it ran.
  icon: server
  color: gray
spec:
  properties:
    type:
      title: Type
      type: string
      enum: [instance, group, project]
    status:
      title: Status
      type: string
      enum: [online, offline, stale, never_contacted]
      display:
        as: badge
        colors: {online: emerald, offline: gray, stale: amber, never_contacted: gray}
    paused:
      title: Paused
      type: boolean
    jobExecution:
      title: Job Execution
      description: Whether it was running jobs when last read.
      type: string
      enum: [active, idle]
    shared:
      title: Shared
      type: boolean
    tags:
      title: Tags
      type: array
      items:
        type: string
    runUntagged:
      title: Runs Untagged Jobs
      type: boolean
    locked:
      title: Locked to Its Projects
      type: boolean
    accessLevel:
      title: Access Level
      description: Whether it runs jobs only for protected branches and tags.
      type: string
      enum: [not_protected, ref_protected]
    maximumTimeout:
      title: Maximum Timeout
      type: number
      display:
        unit: s
    maintenanceNote:
      title: Maintenance Note
      type: string
    groups:
      title: Groups
      relation: namespace
      many: true
    repositories:
      title: Repositories
      relation: repository
      many: true
    version:
      title: Version
      type: string
    revision:
      title: Revision
      type: string
    platform:
      title: Platform
      type: string
    architecture:
      title: Architecture
      type: string
    registeredBy:
      title: Registered By
      type: string
    registeredAt:
      title: Registered
      type: string
      format: date-time
    contactedAt:
      title: Last Contact
      type: string
      format: date-time
    lastJobAt:
      title: Last Job
      type: string
      format: date-time
    url:
      title: URL
      type: string
      format: uri
  sources:
    # Each runner the attempts ran on, read on its own, or as its newest
    # attempt found it when GitLab won't say more.
    - connection: gitlab
      resource: runners
      mode: upsert
      entity:
        identifier: .id
        title: ._gp.title
        properties:
          type: .runner_type | rtrimstr("_type")
          status: .status
          paused: .paused
          jobExecution: .job_execution_status
          shared: .is_shared
          tags: .tag_list
          runUntagged: .run_untagged
          locked: .locked
          accessLevel: .access_level
          maximumTimeout: .maximum_timeout
          maintenanceNote: .maintenance_note
          groups: ._gp.groups
          repositories: ._gp.projects
          version: .version
          revision: .revision
          platform: .platform
          architecture: .architecture
          registeredBy: .created_by.username
          registeredAt: .created_at
          contactedAt: .contacted_at
          lastJobAt: ._gp.lastJobAt
          url: ._gp.url
  # A runner is read only when jobs ran on it, so one that ran none for a
  # while leaves.
  retention:
    by: lastJobAt
    keep: 7d
  catalog:
    columns: [type, status, tags, version, platform, lastJobAt]
  entity:
    header:
      badges: [status, type]
      link: url
    sections:
      - title: Scheduling
        properties: [paused, jobExecution, shared, tags, runUntagged, locked, accessLevel, maximumTimeout]
      - title: Ownership
        properties: [groups, repositories, registeredBy, registeredAt, maintenanceNote]
      - title: Software
        properties: [version, revision, platform, architecture, contactedAt, lastJobAt]
    queries:
      attempts:
        blueprint: job-attempt
        filter: {property: runner, op: eq, value: $entity}
      finished:
        blueprint: job-attempt
        filter:
          and:
            - {property: runner, op: eq, value: $entity}
            - {property: status, op: in, values: [success, failed]}
      failed:
        blueprint: job-attempt
        filter:
          and:
            - {property: runner, op: eq, value: $entity}
            - {property: status, op: eq, value: failed}
    tabs:
      jobs:
        title: Jobs
        icon: play
        widgets:
          attempts:
            type: stat
            title: Job Attempts
            description: Ran on it, last 7 days.
            icon: play
            color: teal
            value: {query: attempts}
            link: {query: attempts}
          failing:
            type: stat
            title: Failed
            description: Of finished attempts, last 7 days.
            icon: circle-x
            color: red
            value: {query: finished, share: {property: status, op: eq, value: failed}}
            link: {query: failed}
          queued:
            type: stat
            title: P95 Queue Time
            description: How long 95% of attempts waited for it at most, last 7 days.
            icon: hourglass
            color: amber
            value: {query: attempts, p95: queuedSeconds}
            link: {query: attempts}
          took:
            type: stat
            title: P95 Job Time
            description: How long 95% of attempts ran at most, last 7 days.
            icon: timer
            color: orange
            value: {query: finished, p95: seconds}
            link: {query: finished}
          perDay:
            type: trend
            title: Job Attempts per Day
            description: By when their pipeline was triggered, each UTC day.
            query: attempts
            date: triggeredAt
            range: 7d
            link: {query: attempts, group: true}
          queuedPerDay:
            type: trend
            title: P95 Queue Time per Day
            description: How long 95% of attempts waited for it at most, each UTC day.
            query: attempts
            date: triggeredAt
            range: 7d
            measure: {p95: queuedSeconds, unit: s}
            link: {query: attempts, group: true}
          byJob:
            type: matrix
            width: 6
            title: By Job
            description: Finished attempts, last 7 days.
            query: finished
            groupBy: name
            limit: 8
            sort: -Attempts
            measures:
              - {title: Attempts}
              - {title: Passed, share: {property: status, op: eq, value: success}}
              - {title: P95 Time, p95: seconds, unit: s}
          byRepository:
            type: matrix
            width: 6
            title: By Repository
            description: Finished attempts, last 7 days.
            query: finished
            groupBy: repo
            limit: 8
            sort: -Attempts
            measures:
              - {title: Attempts}
              - {title: Passed, share: {property: status, op: eq, value: success}}
              - {title: P95 Time, p95: seconds, unit: s}
          recentFailures:
            type: table
            title: Recent Failed Attempts
            query: failed
            columns: [{path: name}, {path: repo}, {path: failureReason}, {path: attempt}, {path: seconds}, {path: startedAt}]
            sort: -startedAt
            limit: 10
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

-- Example connections: the Team Directory, which the mock pushes to with a
-- token, all of it as a complete push and single teams between; the Service
-- Registry, pulled every half hour and kept current between by events the
-- registry signs; Incidents and Workflows, which the example actions write;
-- the Portal, which syncs the portal's own objects; the Deploy Tracker,
-- pulled incrementally every five minutes and completely every hour; and
-- GitLab, which reads the mock's acme group with an access token, and is
-- kept current between syncs by the webhook its syncs set up on each
-- project.
WITH example (identifier, source) AS (VALUES
  ('team-directory', $yaml$kind: Connection
metadata:
  identifier: team-directory
  title: Team Directory
  description: Teams and their channels, from the team directory.
spec:
  connector:
    type: http
    config:
      resources:
        teams:
          push:
            items: .body.teams[]
$yaml$),
  ('service-registry', $yaml$kind: Connection
metadata:
  identifier: service-registry
  title: Service Registry
  description: Services from the deployment registry. Retired ones are left out.
spec:
  connector:
    type: http
    config:
      push:
        signature:
          header: X-Registry-Signature
          algorithm: sha256
          prefix: sha256=
          secret: {env: GP_SECRET_SERVICE_REGISTRY_SIGNING}
      resources:
        services:
          url: http://localhost:47800/registry/services
          items: .services[]
          push:
            items: .body.service
            deleted: .body.action == "deleted"
  sync:
    interval: 30m
$yaml$),
  ('incidents', $yaml$kind: Connection
metadata:
  identifier: incidents
  title: Incidents
  description: Incidents, which the Open incident and Resolve actions write.
spec:
  connector:
    type: http
    config:
      resources:
        incidents:
          push:
            items: .body.incidents[]
$yaml$),
  ('portal', $yaml$kind: Connection
metadata:
  identifier: portal
  title: Portal
  description: The portal's own users, groups, blueprints, connections, actions and recent runs.
spec:
  connector:
    type: portal
  sync:
    interval: 5m
$yaml$),
  ('deploy-tracker', $yaml$kind: Connection
metadata:
  identifier: deploy-tracker
  title: Deploy Tracker
  description: Deployments, pulled from the deploy tracker's API.
spec:
  connector:
    type: http
    config:
      resources:
        deployments:
          # $since makes the tracker answer with what changed, and the
          # deployments that dropped out, marked deleted.
          url: http://localhost:47800/deploy-tracker/deployments?since={{ $since }}
          next: '$headers.link // "" | capture("<(?<url>[^>]+)>; rel=\"next\"").url'
          items: .deployments[]
          deleted: .deleted == true
          headers:
            Authorization: {env: GP_SECRET_DEPLOY_TRACKER}
  sync:
    interval: 5m
    complete: 1h
$yaml$),
  ('gitlab', $yaml$kind: Connection
metadata:
  identifier: gitlab
  title: GitLab
  description: Groups, projects with their default branches and CI configs, merge requests, pipelines with their jobs and runners, and deployments from the acme group on GitLab.
spec:
  connector:
    type: gitlab
    config:
      url: http://localhost:47800/gitlab
      token: {env: GP_SECRET_GITLAB_TOKEN}
      groups: [acme]
      excludeArchived: true
  sync:
    interval: 30m
    complete: 24h
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'connection', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);

INSERT INTO ingestion_tokens (connection_id, name, prefix, secret_hash)
SELECT d.id, 'Mock Pusher', left(t.token, 12), sha256(convert_to(t.token, 'UTF8'))
FROM (VALUES
  ('team-directory', :'team_token')
) AS t (identifier, token)
JOIN config_documents d ON d.kind = 'connection' AND d.identifier = t.identifier
ON CONFLICT (secret_hash) DO NOTHING;

-- Example scorecards. Checks are jq over each entity, flat as paths read
-- it: its properties, relations and lookups beside identifier and title.
-- The service and team blueprints hold each one's results in a property.
WITH example (identifier, source) AS (VALUES
  ('service-readiness', $yaml$kind: Scorecard
metadata:
  identifier: service-readiness
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
          check: {property: owner, op: exists}
          fix: Set the owning team in the service registry.
        hasRepository:
          title: Has a repository
          check: {property: repository, op: exists}
          fix: Add the repository URL to the service registry.
    - name: silver
      title: Silver
      color: sky
      criteria:
        ownerOnSlack:
          title: Owner has a Slack channel
          check: {property: ownerSlack, op: exists}
          fix: Give the owning team a Slack channel in the team directory.
        tagged:
          title: Tagged
          check: {property: tagged, op: eq, value: true}
          fix: Tag the service in the service registry.
        criticalOnCall:
          title: Owner is on call
          when: {property: critical, op: eq, value: true}
          check: {property: ownerOncall, op: exists}
          fix: Critical services need an owning team with an on-call rotation.
    - name: gold
      title: Gold
      color: amber
      criteria:
        deployedRecently:
          title: Deployed in the last 6 hours
          check: {property: lastDeployedAt, op: within, value: 6h}
          fix: Ship small changes often.
        dependenciesKnown:
          title: Dependencies are listed
          check: {property: dependsOn, op: exists}
          fix: List what the service calls in the service registry.
$yaml$),
  ('team-basics', $yaml$kind: Scorecard
metadata:
  identifier: team-basics
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
          check: {property: slackChannel, op: exists}
        oncall:
          title: Has an on-call rotation
          check: {property: oncall, op: exists}
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
  identifier: services
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
      link: {path: /catalog/service?view=scorecard-service-readiness}
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
        - {path: readiness}
        - {title: Failing Checks, path: readiness.failed}
        - {path: tier}
        - {path: lastDeployedAt}
      sort: -lastDeployedAt
      limit: 8
      link: {query: services}
    deployments:
      type: table
      title: Recent Deployments
      query: deployments
      columns: [{path: service}, {path: environment}, {path: status}, {path: startedAt}]
      sort: -startedAt
      limit: 8
      link: {query: deployments}
$yaml$),
  ('operations', $yaml$kind: Dashboard
metadata:
  identifier: operations
  title: Operations
  description: Incidents, and the actions people run on services.
  icon: activity
  color: rose
spec:
  order: 2
  queries:
    incidents:
      blueprint: incident
    open:
      blueprint: incident
      filter: {property: status, op: eq, value: open}
    openSev1:
      blueprint: incident
      filter:
        and:
          - {property: status, op: eq, value: open}
          - {property: severity, op: eq, value: sev1}
    runs:
      blueprint: run
      filter: {property: startedAt, op: within, value: 7d}
    waiting:
      blueprint: run
      filter: {property: status, op: eq, value: waiting}
    failed:
      blueprint: run
      filter:
        and:
          - {property: status, op: in, values: [failed, rejected, interrupted]}
          - {property: startedAt, op: within, value: 7d}
    actions:
      blueprint: action
    deployments:
      blueprint: deployment
      filter: {property: startedAt, op: within, value: 14d}
  widgets:
    open:
      type: stat
      title: Open Incidents
      icon: siren
      color: rose
      value: {query: open}
      link: {query: open}
    sev1:
      type: stat
      title: Open Sev1
      icon: flame
      color: red
      value: {query: openSev1}
      link: {query: openSev1}
    waiting:
      type: stat
      title: Waiting for Approval
      icon: hourglass
      color: indigo
      value: {query: waiting}
      link: {query: waiting}
    failed:
      type: stat
      title: Failed Runs
      description: Failed, rejected or interrupted, last 7 days
      icon: circle-x
      color: red
      value: {query: failed}
      link: {query: failed}
    respond:
      type: actions
      title: Respond
      actions: [open-incident, scale, restart]
      workflows: [restart-services]
    bySeverity:
      type: donut
      width: 4
      title: Incidents by Severity
      query: incidents
      groupBy: severity
      link: {query: incidents, group: true}
    byStatus:
      type: donut
      width: 4
      title: Runs by Status
      query: runs
      groupBy: status
      link: {query: runs, group: true}
    byService:
      type: bar
      width: 6
      title: Incidents by Service
      query: incidents
      groupBy: service
      link: {query: incidents, group: true}
    byAction:
      type: bar
      width: 6
      title: Runs by Action
      query: runs
      groupBy: action
      link: {query: runs, group: true}
    openList:
      type: table
      width: 6
      title: Open Incidents
      query: open
      columns: [{path: severity}, {path: service}, {path: openedBy}, {path: openedAt}]
      sort: -openedAt
      limit: 8
      link: {query: open}
    actionList:
      type: table
      width: 6
      title: Actions
      query: actions
      columns: [{path: runsOn}, {path: approvals}, {path: runs}, {path: lastRunStatus}]
      sort: -runs
      link: {query: actions}
    recent:
      type: table
      title: Recent Runs
      query: runs
      columns: [{path: action}, {path: entity}, {path: requester}, {path: status}, {path: startedAt}]
      sort: -startedAt
      limit: 8
      link: {query: runs}
    deployTime:
      type: stat
      title: P95 Deploy Time
      description: How long 95% of finished deployments took at most, last 14 days.
      icon: timer
      color: orange
      value: {query: deployments, p95: seconds}
      link: {query: deployments}
    perDay:
      type: trend
      width: 9
      title: Deployments per Day
      description: Started each UTC day, last 14 days.
      query: deployments
      date: startedAt
      range: 14d
      link: {query: deployments, group: true}
    failing:
      type: bar
      title: Failure Share by Service
      description: Of finished deployments, last 14 days.
      query: deployments
      groupBy: service
      measure:
        filter: {property: status, op: ne, value: running}
        share: {property: status, op: eq, value: failed}
      sort: -measure
      link: {query: deployments, group: true}
    byEnvironment:
      type: matrix
      width: 6
      title: By Environment
      description: Last 14 days.
      query: deployments
      groupBy: environment
      measures:
        - {title: Deployments}
        - {title: Succeeded, filter: {property: status, op: ne, value: running}, share: {property: status, op: eq, value: succeeded}}
        - {title: P95 Time, p95: seconds}
$yaml$),
  ('portal', $yaml$kind: Dashboard
metadata:
  identifier: portal
  title: Portal
  description: What the portal holds and where it comes from.
  icon: database
  color: cyan
spec:
  order: 3
  visibility: {roles: [builder, admin]}
  queries:
    activeUsers:
      blueprint: user
      filter: {property: active, op: eq, value: true}
    blueprints:
      blueprint: blueprint
    connections:
      blueprint: connection
    failing:
      blueprint: connection
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
      value: {query: blueprints}
      link: {path: /blueprints}
    connections:
      type: stat
      title: Connections
      icon: plug
      color: teal
      value: {query: connections}
      link: {path: /connections}
    failing:
      type: stat
      title: Failing Connections
      icon: circle-x
      color: red
      value: {query: failing}
      link: {query: failing}
    list:
      type: table
      title: Connections
      query: connections
      columns: [{path: connector}, {path: lastRunOutcome}, {path: entities}, {path: lastRunAt}]
      sort: -entities
      rowLink: {path: '/connections/{{ identifier }}'}
      link: {query: connections}
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'dashboard', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);

-- Example actions, which call the mock ops API in deps.dev.yaml. Restart is
-- for builders and the service's owners; the others are for everyone. Scale
-- waits for a platform engineer or another of the service's owners to
-- approve the change. Open incident
-- and Resolve write incidents to the catalog as the Incidents connection.
WITH example (identifier, source) AS (VALUES
  ('restart', $yaml$kind: Action
metadata:
  identifier: restart
  title: Restart
  description: Restarts the service in one environment.
  icon: rotate-cw
  color: amber
spec:
  blueprint: service
  group: Operate
  run:
    roles: [builder]
    filter: {property: owner.members, op: contains, value: $me}
  inputs:
    environment:
      title: Environment
      type: string
      enum: [production, staging]
      required: true
    reason:
      title: Reason
      description: Why it needs a restart, for the people watching the service.
      type: string
      minLength: 5
      required: true
  steps:
    - id: restart
      title: Restart the service
      http:
        method: POST
        url: http://localhost:47800/ops/services/{{ $entity }}/restart
        headers:
          Authorization: {env: GP_SECRET_OPS}
        body:
          environment: $input.environment
          reason: $input.reason
          requestedBy: $me
          run: $run.url
        output:
          job: .body.id
      retry: {attempts: 2, backoff: 3s}
    - output:
        summary: "Restarted {{ $entity.title }} in {{ $input.environment }}"
        links:
          Job: http://localhost:47800/ops/jobs/{{ $step.restart.job }}
  outputs:
    job: $step.restart.job
$yaml$),
  ('scale', $yaml$kind: Action
metadata:
  identifier: scale
  title: Scale
  description: Sets how many replicas the service runs in one environment.
  icon: scaling
  color: sky
spec:
  blueprint: service
  group: Operate
  scope: {property: tier, op: ne, value: experimental}
  run:
    roles: [member]
  inputs:
    environment:
      title: Environment
      type: string
      enum: [production, staging]
      required: true
    replicas:
      title: Replicas
      type: integer
      minimum: 1
      maximum: 20
      required: true
  steps:
    - id: approve
      title: Approve the change
      approval:
        approvers:
          groups: [platform-engineers]
          filter: {property: owner.members, op: contains, value: $me}
        require: 1
        timeout: 24h
        message: "Scale {{ $entity.title }} to {{ $input.replicas }} replicas in {{ $input.environment }}?"
        review:
          - title: Change
            markdown: |-
              | Service | Environment | Replicas | Tier | Owner |
              | --- | --- | --- | --- | --- |
              | {{ $entity.title }} | {{ $input.environment }} | **{{ $input.replicas }}** | {{ $entity.tier }} | {{ $entity.owner }} |

              Requested by {{ $me.name }} ({{ $me.email }}).
    - id: scale
      title: Scale the service
      http:
        method: POST
        url: http://localhost:47800/ops/services/{{ $entity }}/scale
        headers:
          Authorization: {env: GP_SECRET_OPS}
        body:
          environment: $input.environment
          replicas: $input.replicas
        output:
          replicas: .body.replicas
      retry: {attempts: 3, backoff: 2s}
    - output:
        summary: "{{ $entity.title }} runs {{ $step.scale.replicas }} replicas in {{ $input.environment }}"
  outputs:
    replicas: $step.scale.replicas
    approvedBy: $step.approve.approvedBy
$yaml$),
  ('open-incident', $yaml$kind: Action
metadata:
  identifier: open-incident
  title: Open incident
  description: Opens an incident on the service with its own channel, and adds it to the catalog.
  icon: siren
  color: rose
spec:
  blueprint: service
  group: Respond
  run:
    roles: [member]
  inputs:
    severity:
      title: Severity
      type: string
      enum: [sev1, sev2, sev3]
      required: true
    summary:
      title: Summary
      description: What's broken, as people will see it.
      type: string
      minLength: 10
      required: true
  steps:
    - title: Check the service has an owner
      check:
        blueprint: service
        filter:
          and:
            - {property: identifier, op: eq, value: $entity}
            - {property: owner, op: exists}
        expect: {count: 1}
        message: "{{ $entity.title }} has no owner to page; set one in the service registry first."
    - id: create
      title: Open the incident
      http:
        method: POST
        url: http://localhost:47800/ops/incidents
        headers:
          Authorization: {env: GP_SECRET_OPS}
        body:
          service: $entity
          owner: $entity.owner
          severity: $input.severity
          summary: $input.summary
        output:
          id: .body.id
          channel: .body.channel
          at: .body.at
    - id: incident
      title: Add it to the catalog
      entity:
        operation: upsert
        connection: incidents
        blueprint: incident
        identifier: $step.create.id
        title: $input.summary
        properties:
          severity: $input.severity
          status: open
          service: $entity
          channel: $step.create.channel
          openedBy: $me
          openedAt: $step.create.at
    - output:
        summary: "Opened {{ $step.create.id }} on {{ $entity.title }}"
        links:
          Incident: $step.incident.url
          Channel: $step.create.channel
  outputs:
    incident: $step.create.id
$yaml$),
  ('resolve-incident', $yaml$kind: Action
metadata:
  identifier: resolve-incident
  title: Resolve
  description: Resolves an open incident.
  icon: circle-check
  color: emerald
spec:
  blueprint: incident
  group: Respond
  scope: {property: status, op: eq, value: open}
  run:
    roles: [member]
  steps:
    - id: resolve
      title: Resolve the incident
      http:
        method: POST
        url: http://localhost:47800/ops/incidents/{{ $entity }}/resolve
        headers:
          Authorization: {env: GP_SECRET_OPS}
        output:
          at: .body.at
    - title: Mark it resolved
      entity:
        operation: upsert
        connection: incidents
        blueprint: incident
        identifier: $entity
        properties:
          status: resolved
          resolvedAt: $step.resolve.at
    - output:
        summary: "Resolved {{ $entity.title }}"
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'action', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);

-- Example workflows, in the Self-Service catalog beside the actions of the
-- same group. Restart services waits for a platform engineer, then runs
-- the Restart action on each service picked, which checks the requester
-- may restart each one. Register a
-- service waits for a platform engineer, creates its repository and adds it
-- to the catalog as the Workflows connection.
WITH example (identifier, source) AS (VALUES
  ('restart-services', $yaml$kind: Workflow
metadata:
  identifier: restart-services
  title: Restart services
  description: Restarts several services in one environment, each only where you can restart it.
  icon: rotate-cw
  color: amber
spec:
  group: Operate
  run:
    roles: [member]
  inputs:
    services:
      title: Services
      relation: service
      many: true
      required: true
    environment:
      title: Environment
      type: string
      enum: [production, staging]
      required: true
    reason:
      title: Reason
      type: string
      minLength: 5
      required: true
  steps:
    - approval:
        approvers:
          groups: [platform-engineers]
        require: 1
        timeout: 4h
        message: "Restart these services in {{ $input.environment }}? Reason: {{ $input.reason }}"
        review:
          - title: Services
            json: $input.services
    - id: restart
      title: Restart each service
      action:
        name: restart
        entities: $input.services
        inputs:
          environment: $input.environment
          reason: $input.reason
        maxFailures: 0
    - output:
        summary: "Restarted {{ $step.restart.succeeded }} services in {{ $input.environment }}"
  outputs:
    restarted: $step.restart.succeeded
$yaml$),
  ('register-service', $yaml$kind: Workflow
metadata:
  identifier: register-service
  title: Register a service
  description: Creates a repository for a new service and registers the service, which the catalog then syncs, once a platform engineer approves.
  icon: package-plus
  color: emerald
spec:
  group: Create
  run:
    roles: [member]
  inputs:
    name:
      title: Name
      description: Lowercase letters, digits and dashes, such as billing-api.
      type: string
      pattern: ^[a-z][a-z0-9-]{2,40}$
      required: true
    title:
      title: Title
      type: string
      required: true
    owner:
      title: Owner
      relation: team
      required: true
    language:
      title: Language
      type: string
      enum: [go, python, typescript, java]
      required: true
    tier:
      title: Tier
      type: string
      enum: [critical, standard, experimental]
      required: true
  steps:
    - title: Check the name is free
      check:
        blueprint: service
        filter: {property: identifier, op: eq, value: $input.name}
        expect: {count: 0}
        message: "There's already a service called {{ $input.name }}; pick another name."
    - id: approve
      title: Platform approves
      approval:
        approvers:
          groups: [platform-engineers]
        require: 1
        timeout: 72h
        message: "Register {{ $input.title }} for {{ $input.owner }}?"
        review:
          - title: Service
            markdown: |-
              | Name | Title | Owner | Language | Tier |
              | --- | --- | --- | --- | --- |
              | `{{ $input.name }}` | {{ $input.title }} | {{ $input.owner }} | {{ $input.language }} | {{ $input.tier }} |

              Requested by {{ $me.name }} ({{ $me.email }}).
    - id: repo
      title: Create the repository
      http:
        method: POST
        url: http://localhost:47800/ops/repos
        headers:
          Authorization: {env: GP_SECRET_OPS}
        body:
          name: $input.name
          owner: $input.owner
        output:
          url: .body.url
    - id: registry
      title: Add it to the registry
      http:
        method: POST
        url: http://localhost:47800/ops/services
        headers:
          Authorization: {env: GP_SECRET_OPS}
        body:
          name: $input.name
          displayName: $input.title
          language: $input.language
          tier: $input.tier
          repo: $step.repo.url
          owner: $input.owner
    - title: Bring it into the catalog
      sync:
        connection: service-registry
        resources: [services]
        mode: incremental
    - output:
        summary: "Registered {{ $input.title }}"
        links:
          Repository: $step.repo.url
  outputs:
    repository: $step.repo.url
$yaml$)
), created AS (
  INSERT INTO config_documents (kind, identifier, published_version)
  SELECT 'workflow', identifier, 1 FROM example
  ON CONFLICT (kind, identifier) DO NOTHING
  RETURNING id, identifier
)
INSERT INTO config_versions (document_id, version, source)
SELECT created.id, 1, example.source FROM created JOIN example USING (identifier);

-- Example incidents, as if the Open incident action had opened them over the
-- last two weeks, so the Operations dashboard has something to show.
INSERT INTO entities (blueprint_id, connection_id, identifier, title, properties, relations)
SELECT b.id, i.id, e.identifier, e.title,
  jsonb_strip_nulls(jsonb_build_object(
    'severity', e.severity, 'status', e.status,
    'channel', 'https://slack.example.com/archives/' || e.identifier,
    'openedAt', to_char((now() - e.opened) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
    'resolvedAt', to_char((now() - e.resolved) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))),
  jsonb_build_object('service', e.service, 'openedBy', e.opened_by)
FROM (VALUES
  ('inc-101', 'Card payments time out for some customers', 'sev1', 'resolved', 'payments-api', 'ada@example.com', interval '13 days', interval '13 days' - interval '3 hours'),
  ('inc-102', 'Ledger reconciliation lags by an hour', 'sev3', 'resolved', 'ledger', 'grace@example.com', interval '9 days', interval '8 days'),
  ('inc-103', 'Search results miss new products', 'sev2', 'resolved', 'search-api', 'member@example.com', interval '6 days', interval '5 days'),
  ('inc-104', 'Checkout shows a blank page on Safari', 'sev2', 'open', 'checkout-web', 'grace@example.com', interval '2 days', NULL),
  ('inc-105', 'Indexer falls behind during the nightly import', 'sev3', 'open', 'indexer', 'member@example.com', interval '20 hours', NULL),
  ('inc-106', 'Payments API errors spike after the deploy', 'sev1', 'open', 'payments-api', 'ada@example.com', interval '35 minutes', NULL)
) AS e (identifier, title, severity, status, service, opened_by, opened, resolved)
JOIN config_documents b ON b.kind = 'blueprint' AND b.identifier = 'incident'
JOIN config_documents i ON i.kind = 'connection' AND i.identifier = 'incidents'
ON CONFLICT (blueprint_id, identifier) DO NOTHING;

-- Example runs of the Restart and Scale actions over the last week, so the
-- dashboards and run lists have history. Their ids are fixed, so this adds
-- them once. Scale runs show the approval each passed.
WITH example (n, action, service, email, status, error, summary, inputs, started, took) AS (VALUES
  (1, 'scale', 'search-api', 'member@example.com', 'succeeded', '', 'Search API runs 4 replicas in production', '{"environment": "production", "replicas": 4}', interval '6 days', 3),
  (2, 'restart', 'payments-api', 'ada@example.com', 'succeeded', '', 'Restarted Payments API in production', '{"environment": "production", "reason": "memory climbs after the deploy"}', interval '5 days', 4),
  (3, 'scale', 'checkout-web', 'grace@example.com', 'failed', 'Scale the service: localhost:47800 answered 503 Service Unavailable', '', '{"environment": "production", "replicas": 6}', interval '4 days', 9),
  (4, 'scale', 'checkout-web', 'grace@example.com', 'succeeded', '', 'Checkout runs 6 replicas in production', '{"environment": "production", "replicas": 6}', interval '4 days' - interval '10 minutes', 2),
  (5, 'restart', 'ledger', 'builder@example.com', 'succeeded', '', 'Restarted Ledger in staging', '{"environment": "staging", "reason": "pick up the new certificate"}', interval '3 days', 3),
  (6, 'restart', 'payments-api', 'ada@example.com', 'cancelled', '', '', '{"environment": "staging", "reason": "wrong environment"}', interval '2 days', 1),
  (7, 'scale', 'indexer', 'member@example.com', 'succeeded', '', 'Indexer runs 3 replicas in staging', '{"environment": "staging", "replicas": 3}', interval '20 hours', 2),
  (8, 'restart', 'payments-api', 'ada@example.com', 'succeeded', '', 'Restarted Payments API in production', '{"environment": "production", "reason": "errors spike after the deploy"}', interval '30 minutes', 4)
), runs_added AS (
  INSERT INTO runs (id, document_id, version, title, entity_id, entity_blueprint, entity_identifier, entity_title,
    requested_by, requester, via, inputs, status, summary, error, created_at, started_at, finished_at)
  SELECT md5('example-run-' || e.n)::uuid, d.id, 1, initcap(e.action), en.id, 'service', e.service,
    coalesce(en.title, e.service), u.id,
    jsonb_build_object('id', u.id, 'email', u.email, 'name', u.display_name, 'role', 'member', 'groups', '[]'::jsonb),
    'web', e.inputs::jsonb, e.status, e.summary, e.error,
    now() - e.started, now() - e.started, now() - e.started + e.took * interval '1 second'
  FROM example e
  JOIN config_documents d ON d.kind = 'action' AND d.identifier = e.action
  JOIN users u ON u.oidc_issuer = :'issuer' AND u.oidc_subject = e.email
  LEFT JOIN entities en ON en.identifier = e.service
    AND en.blueprint_id = (SELECT id FROM config_documents WHERE kind = 'blueprint' AND identifier = 'service')
  ON CONFLICT (id) DO NOTHING
  RETURNING id, title, status, error, inputs, entity_title, started_at, finished_at
)
INSERT INTO run_steps (run_id, position, step, type, title, status, attempts, output, error, detail, started_at, finished_at)
SELECT r.id, s.position, s.step, s.type, s.title,
  CASE
    WHEN s.type = 'approval' THEN 'done'
    WHEN s.type = 'http' AND r.status IN ('failed', 'cancelled') THEN 'failed'
    WHEN s.type = 'output' AND r.status <> 'succeeded' THEN 'pending'
    ELSE 'done'
  END,
  CASE WHEN s.type = 'output' AND r.status <> 'succeeded' THEN 0 WHEN s.type = 'http' AND r.status = 'failed' THEN 4 ELSE 1 END,
  CASE WHEN s.type = 'approval' THEN '{"approvedBy": ["builder@example.com"], "comments": []}'::jsonb END,
  CASE
    WHEN s.type = 'http' AND r.status = 'failed' THEN '503 Service Unavailable after 4 attempts'
    WHEN s.type = 'http' AND r.status = 'cancelled' THEN 'The run was cancelled during this step.'
    ELSE ''
  END,
  CASE WHEN s.type = 'approval' THEN jsonb_build_object('require', 1,
    'message', 'Scale ' || r.entity_title || ' to ' || (r.inputs->>'replicas') || ' replicas in ' || (r.inputs->>'environment') || '?',
    'review', '[]'::jsonb) END,
  CASE WHEN s.type = 'output' AND r.status <> 'succeeded' THEN NULL ELSE r.started_at END,
  CASE WHEN s.type = 'output' AND r.status <> 'succeeded' THEN NULL ELSE r.finished_at END
FROM runs_added r
JOIN (VALUES
  ('Scale', 0, 'approve', 'approval', 'Approve the change'),
  ('Scale', 1, 'scale', 'http', 'Scale the service'),
  ('Scale', 2, '', 'output', 'Output'),
  ('Restart', 0, 'restart', 'http', 'Restart the service'),
  ('Restart', 1, '', 'output', 'Output')
) AS s (action, position, step, type, title) ON s.action = r.title;

-- A platform engineer approved each example Scale run.
INSERT INTO run_approvals (run_id, position, user_id, email, name, decision, decided_at)
SELECT r.id, 0, u.id, u.email, u.display_name, 'approve', r.started_at
FROM runs r
JOIN users u ON u.oidc_issuer = :'issuer' AND u.oidc_subject = 'builder@example.com'
WHERE r.id IN (SELECT md5('example-run-' || n)::uuid FROM generate_series(1, 8) n) AND r.title = 'Scale'
ON CONFLICT (run_id, position, email) DO NOTHING;
