# Setup

From a system to a catalog, a dashboard and a scorecard. The snippets follow GitLab repositories through each step.

## 1. Connect a System

**Connections › New Connection**. Pick a connector, point it at the system and set how often it syncs.

```yaml
kind: Connection
metadata:
  identifier: gitlab
  title: GitLab
spec:
  connector:
    type: gitlab
    config:
      url: https://gitlab.acme.example
      token:
        env: GP_SECRET_GITLAB_TOKEN
      groups:
        - acme
  sync:
    interval: 15m
```

- No connector for the system? `http` pulls any JSON API, or receives pushes. See [Syncing](syncing.md).
- [Connectors](connectors/README.md) lists what each one reads.

## 2. Model What You Want

**Blueprints › New Blueprint**. A blueprint is a kind of entity and the properties each one has.

```yaml
kind: Blueprint
metadata:
  identifier: repository
  title: Repository
spec:
  properties:
    ci:
      title: CI
      type: string
    url:
      title: URL
      type: string
      format: uri
```

## 3. Add a Source

Add a source to the blueprint: the connection and resource it reads, and how to fill each field from one object of that resource. Each value is a [jq](https://jqlang.org/manual/) expression; `.name` reads the object's `name`.

```yaml
sources:
  - connection: gitlab
    resource: projects
    entity:
      identifier: .path_with_namespace
      title: .name
      properties:
        ci: ._latestPipeline.status
        url: .web_url
```

- Publish, then **Connections › GitLab › Sync Now**. Repositories show in **Catalog**.
- See [Blueprints](blueprints.md) for properties, sources and jq tips.

## 4. Link Entities

A relation links an entity to an entity of another blueprint, by that entity's identifier. Say a `team` blueprint holds teams such as `payments`, and each team's projects sit in a GitLab group of the same name, such as `acme/payments/api`. Add an `owner` relation to the repository blueprint, and fill it from the project's group:

```yaml
kind: Blueprint
metadata:
  identifier: repository
  title: Repository
spec:
  properties:
    ci:
      title: CI
      type: string
    owner:  # holds a team's identifier
      relation: team
  sources:
    - connection: gitlab
      resource: projects
      entity:
        identifier: .path_with_namespace
        title: .name
        properties:
          ci: ._latestPipeline.status
          owner: .namespace.path  # "payments" for acme/payments/api
```

- Repository `acme/payments/api` now links to team `payments`.
- Filters, columns and widgets can follow the link, such as `owner.title` for the team's name.

## 5. Add a Dashboard

**Dashboards › New Dashboard** adds a Home tab. Name queries once; widgets count, chart or list them, and link to the entities behind each number.

```yaml
kind: Dashboard
metadata:
  identifier: repositories
  title: Repositories
spec:
  queries:
    failing:
      blueprint: repository
      filter:
        property: ci
        op: eq
        value: failed
  widgets:
    failing:
      type: stat
      title: Failing CI
      value:
        query: failing
      link:
        query: failing
```

- A blueprint can also add widget tabs to its own catalog and entity pages. See [Dashboards](dashboards.md).

## 6. Score Them

**Blueprints › Repository › Scorecards › New Scorecard**. Tiers go lowest first; an entity reaches a tier by passing its checks and every tier's below it.

```yaml
kind: Scorecard
metadata:
  identifier: readiness
  title: Readiness
spec:
  blueprint: repository
  tiers:
    - name: bronze
      criteria:
        hasOwner:
          title: Has an Owner
          check:
            property: owner
            op: exists
    - name: silver
      criteria:
        ciPasses:
          title: CI Passes
          check:
            property: ci
            op: eq
            value: success
```

- **Readiness › Evaluate Now**, or wait for the next periodic run. See [Scorecards](scorecards.md).
