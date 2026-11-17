# Dashboards

Widgets show on Home, where each Dashboard is a tab, and on a blueprint's catalog and entity pages.

## Where Widgets Go

- **Home**: a `Dashboard` document's `spec.widgets`.
- **Catalog page**: a blueprint's `spec.catalog.tabs`, after the Entities tab.
- **Entity page**: a blueprint's `spec.entity.tabs`, after Relations and YAML. A tab named `overview` replaces the built-in one; other tabs can take a `when` filter.
- Widgets flow left to right on a 12-column grid, in order.

```yaml
# On the team blueprint: a tab listing the team's repositories.
spec:
  entity:
    queries:
      owned:
        blueprint: repository
        filter:
          property: owner
          op: eq
          value: $entity
    tabs:
      repositories:
        title: Repositories
        widgets:
          count:
            type: stat
            title: Repositories
            value:
              query: owned
            link:
              query: owned
          list:
            type: table
            title: Repositories
            query: owned
            columns:
              - path: ci
              - path: lastActivityAt
```

## Queries

- Pages name queries once, as a `blueprint` and optional `filter`; widgets refer to them by name.
- On a blueprint's pages, `blueprint` defaults to its own, and a widget without `query` shows all its entities.
- On Home, every query names a blueprint and every widget showing entities names a query.

## Filters

A comparison `{property, op, value}`, or `and`, `or`, `not` over filters. `property` is a path, such as `owner.title` or `readiness.checks.hasOwner`.

| `op` | Matches |
|---|---|
| `eq`, `ne`, `gt`, `gte`, `lt`, `lte` | compared with `value` |
| `in` | any of `values`, a list or a variable |
| `exists` | has a value |
| `contains` | a list item, a related entity, or text |
| `within` | a date in the last `value`, such as `7d` |

- `$me` is the viewer's email; `$me.name`, `$me.role` and `$me.groups` their details.
- `$entity` is the page's entity, on entity pages.
- `$input.<id>` is a Home input. `$today` is the date.

## Inputs

```yaml
inputs:
  team:
    type: relation  # or enum, with values, or string
    relation: team
    multiple: true
    default:
      query:
        blueprint: team
        filter:
          property: title
          op: in
          values: $me.groups
      otherwise: everything # if the query finds nothing; default nothing
```

- Inputs sit above a Home dashboard. While one is unset, conditions on it drop.
- Compare a `multiple` input with `op: in` and `values: $input.<id>`.
- A default query can use `$me`, not `$entity` or inputs.

## Widgets

Each widget answers one question about a query's entities. With queries `repos` (every repository), `failing` (those failing CI) and `pipelines`:

```yaml
widgets:
  # One number: how many repositories
  total:
    type: stat
    title: Repositories
    value:
      query: repos
    link:
      query: repos
  # A share: what percent pass CI, shown with "n of N"
  passing:
    type: stat
    title: CI Passing
    value:
      query: repos
      share:
        property: ci
        op: eq
        value: success
  # A status as one stacked bar
  status:
    type: split
    title: CI Status
    query: repos
    groupBy: ci
  # Part of a whole, a few groups
  byTeam:
    type: donut
    title: Repositories by Team
    query: repos
    groupBy: owner
  # Compare groups, or a measure per group
  failures:
    type: bar
    title: Failure Share by Team
    query: pipelines
    groupBy: team
    measure:
      share:
        property: status
        op: eq
        value: failed
  # A value per day
  perDay:
    type: trend
    title: Pipelines Per Day
    query: pipelines
    date: triggeredAt
  # Several measures per group
  teams:
    type: matrix
    title: CI by Team
    query: repos
    groupBy: owner
    measures:
      - title: Passing
        share:
          property: ci
          op: eq
          value: success
  # The entities to act on
  todo:
    type: table
    title: Failing CI
    query: failing
    columns:
      - path: owner
      - path: lastActivityAt
  # How entities spread across a scorecard's tiers
  tiers:
    type: scorecard
    title: Readiness
    scorecard: readiness
    query: repos
  # Guidance, which can greet the viewer
  help:
    type: markdown
    text: "Stuck, {{ $me.name }}? Ask in #platform."
  # Actions people can run from here
  fixes:
    type: actions
    title: Fix It
    actions:
      - restart
```

- Entity pages also take `properties`, which shows the entity's own fields.
- Default widths: `stat` 3; `donut` and `actions` 4; `matrix` and `table` 12; the rest 6. `width` overrides it.
- A stat's `value`, or a bar's or trend's `measure`, counts entities unless it names one of `distinct`, `sum`, `avg`, `min`, `max`, `p50`, `p90`, `p95`, `p99`, `share`.
- `share` is the percent matching a filter, such as `{query: pipelines, share: {property: status, op: eq, value: success}}`.
- `link` opens a query's entities (`query`), a `url` or a portal `path`. `group: true` opens each bar's or slice's own entities.
- `url` and `path` are templates whose `{{ }}` hold jq, such as `{{ $input.team | join(",") }}`.
- A table's `rowLink` opens a URL per row, such as `{url: "{{ url }}/-/pipelines"}`.
- A property's `display` (`as`, `colors`, `unit`, `filter: true`...) on the blueprint sets how a property looks everywhere.

## Home Tabs

- `order` sorts tabs. `group: {title}` puts dashboards under one tab with a menu.
- `visibility` and `defaultFor` take `roles` or `groups`: who sees the tab, and whose Home opens on it.
- Titles can hold `{{ $me.name }}`.
