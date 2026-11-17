# Blueprints

A blueprint is a kind of entity, such as repository or team: the properties each entity has, and the sources that fill them.

## Properties

```yaml
properties:
  ci:  # stored: a source writes it
    title: CI
    type: string
  owner:  # relation: another blueprint's entities
    relation: team
  team:  # lookup: read through relations
    lookup: owner.title
  stale:  # computed: jq over the entity
    type: boolean
    compute: .ci == null
  readiness:  # scorecard: the tier reached
    type: scorecard
    scorecard: readiness
```

- Types: `string`, `number`, `integer`, `boolean`, `array`, `object`.
- A string's `format`: `date`, `date-time`, `uri`, `email`, `markdown`.
- `many: true` makes a relation hold several entities.
- `display` sets how a property shows everywhere, such as `{as: badge, colors: {failed: red}}`.
- Every entity also has `identifier` and `title`.
- Computed properties see the entity flat: `.owner`, not `.properties.owner`.

## Sources

```yaml
sources:
  - connection: gitlab
    resource: projects
    filter: .marked_for_deletion_on == null
    entity:
      identifier: .path_with_namespace
      title: .name
      properties:
        owner: .namespace.path  # a relation takes identifiers
        ci: ._latestPipeline.status
```

- Each source reads one resource of one connection. Each value is a jq expression over one object of it; see [Writing jq](#writing-jq).
- Each object carries the resource's fields, such as `.web_url` on a GitLab project. Fields starting with `_` are extras the connector looks up for you, such as `._latestPipeline`. [Connectors](connectors/README.md) lists them.
- `filter` keeps the objects it's true for. It never stops a deletion.
- `entity.identifier` is required. `entity.title` defaults to it.
- `entity.properties` fills stored properties and relations. No result leaves a property empty.

## Writing jq

Source values, source filters and computed properties are written in [jq](https://jqlang.org/manual/), a small language for picking and shaping JSON. A few patterns cover most needs:

- Only the first result counts: wrap a stream in `[ ]` to keep all of it.
- Quote what YAML would read as a list or mapping: `'[.labels[].name]'`.
- Fall back with `//`: `.name // .tag_name`.
- Combine fields: `._project + "!" + (.iid | tostring)`.
- Slugify: `.name | ascii_downcase | gsub("[^a-z0-9]+"; "-")`.

## Modes

- `reconcile`, the default: complete syncs delete entities the resource no longer has.
- `upsert`: deletes only what objects mark deleted; otherwise rows go by retention.
- A blueprint's sources from one connection share a mode. With `retention`, all must be `upsert`.
