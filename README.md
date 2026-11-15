# Golden Portal

Golden Portal is a developer portal. It brings dashboards, scorecards, and golden paths to developers in one place.

## Requirements

- PostgreSQL 16 or later.
- The `pg_trgm` extension, which search uses. It's a trusted extension, so the portal creates it when its database user has `CREATE` on the database; otherwise create it once beforehand with `CREATE EXTENSION pg_trgm`.
- Recommended: `pg_stat_statements` and `auto_explain`, which show which statements are slow and why. Load both with `shared_preload_libraries = 'pg_stat_statements,auto_explain'`, log the plans of slow statements with `auto_explain.log_min_duration = '300ms'`, and run `CREATE EXTENSION pg_stat_statements` once as a superuser to read its view. The development database has them on.

The portal migrates its database when it starts. Some migrations build indexes on `entities` and `scorecard_results`, which blocks writes to them until they finish, so a large catalog may take a while to start after an upgrade.

The portal also indexes what its published configuration sorts and filters entities by, on blueprints with at least 50,000 entities (`ENTITY_INDEX_MIN_ROWS`). It builds and drops these indexes in the background, without blocking writes, after each publish, as it starts and every hour (`ENTITY_INDEX_SCHEDULE`, a cron expression). A build waits for transactions under way to end, for up to an hour. The indexes are named `gpx_*` and listed, with why each exists and how its last build or drop went, in the `entity_indexes` table. Each one slows writes to its blueprint, so `ENTITY_INDEX_MAX_PER_BLUEPRINT` (8) and `ENTITY_INDEX_MAX` (64) cap how many there are; setting either to 0 turns them off and drops those built.

## Agents

AI assistants connect to the portal over MCP, signing in with the portal's own OAuth. There are two servers, at the portal's public URL (`GP_BASE_URL`):

- `/api/mcp` is for everyone. Its tools read the portal's pages, each whole in one call: Home's dashboards, the catalog, an entity's page with its tabs and scorecards, Self-Service, runs and approvals. It runs actions and workflows as the user, and follows their runs.
- `/api/mcp/builder` is for builders, and refuses members. Its tools change the portal's configuration: blueprints and their sources, connections and their syncs, scorecards and dashboards, through drafts that are validated, previewed for impact and then published.

## License

[MIT](LICENSE)
