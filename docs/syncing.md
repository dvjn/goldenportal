# Syncing

Syncing keeps the catalog in step with your systems. Each sync of a connection reads its resources, and every source on them turns what it read into entities.

## How Often

The GitLab connection from Setup syncs every 15 minutes:

```yaml
spec:
  sync:
    interval: 15m   # default 15m, minimum 1m
    complete: 24h   # default: every sync, minimum: the interval
```

- To sync right away: **Connections › GitLab › Sync Now**.

## Keeping Up With Changes

Most syncs read only what changed since the last one, so they stay quick. They update the entities whose objects changed, and remove those the system says were deleted.

Now and then, a complete sync reads everything and also removes entities whose objects are gone. `complete` sets how often. The first sync, and the first after you change a source or the connection, are always complete.

## Systems Without a Connector

When no connector fits, `http` reads any JSON API. You name each resource and say where it lives; everything above works the same.

```yaml
spec:
  connector:
    type: http
    config:
      resources:
        services:
          url: https://registry.example.com/api/services?since={{ $since }}
          items: .services[]
          headers:
            Authorization:
              env: GP_SECRET_REGISTRY_TOKEN
  sync:
    interval: 5m
```

| Key | What it does |
|---|---|
| `url` | Where to read; `{{ $since }}` in it makes syncs read only what changed. |
| `items` | jq picking a page's objects, such as `.services[]`. |
| `next` | jq giving the next page's URL, or null on the last page. |
| `deleted` | jq that's true when an object was deleted. |
| `headers` | Request headers, such as a token from `{env: GP_SECRET_...}`. |
| `maxPages` | Default 100; a sync fails past it. |

## Systems That Send Updates

Some systems would rather tell the portal when something changes, such as a deploy tool after each deploy. Give an `http` resource a `push` side and they post to the portal instead of waiting for a sync; their objects go through the same sources.

1. Add the `push` side: jq over the request as `{body, headers, query}`.

   ```yaml
   spec:
     connector:
       type: http
       config:
         resources:
           deployments:
             push:
               items: .body.deployments[]
   ```

2. Publish, then create a token under **Connections › (connection) › Push**.
3. The system posts JSON to the portal:

   ```sh
   curl -X POST "$PORTAL_URL/api/ingest/deploys" \
     -H "Authorization: Bearer $INGEST_TOKEN" -H "Content-Type: application/json" \
     --data '{"deployments": [{"id": "42", "service": "api", "version": "1.4.0", "environment": "production"}]}'
   ```

| Request | Does |
|---|---|
| `POST /api/ingest/<connection>` | Writes what the body carries, and deletes what it marks deleted. |
| `POST /api/ingest/<connection>?mode=complete` | Also deletes what the body no longer carries. |
| `DELETE /api/ingest/<connection>/entities/<blueprint>/<identifier>` | Deletes one entity. |

Systems that sign their requests, such as webhook senders, can push without a token:

```yaml
config:
  push:
    signature:
      header: X-Hub-Signature-256
      algorithm: sha256  # sha1, sha256 or sha512
      prefix: sha256=
      secret:
        env: GP_SECRET_HOOK
```

## Fresh Between Syncs

When the portal has webhooks turned on, GitLab syncs add a webhook to each project, so changes show without waiting for the next sync. The token then needs the `api` scope and Maintainer on each project.

## Keeping History in Check

Some blueprints grow with every sync, such as pipelines. Retention, on the blueprint, deletes what's old:

```yaml
retention:
  by: triggeredAt      # a date-time property
  keep: 30d  # minimum 1d
  rules:
    - filter:
        property: ref
        op: eq
        value: main
      keep: 90d        # the longest matching rule wins
```

- Rows whose `by` is older than `keep` are deleted daily.
- The blueprint's sources need `mode: upsert`, so complete syncs leave the old rows to retention.

## Troubleshooting

| Symptom | Check |
|---|---|
| A sync failed | **Connections › (connection) › Runs** |
| Entities are missing | The source's `filter` |
| Deleted entities are still there | They go on the next complete sync |
