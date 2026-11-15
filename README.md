# Golden Portal

Golden Portal is a developer portal. It brings dashboards, scorecards, and golden paths to developers in one place.

## Agents

AI assistants connect to the portal over MCP, signing in with the portal's own OAuth. There are two servers, at the portal's public URL (`GP_BASE_URL`):

- `/api/mcp` is for everyone. Its tools read the portal's pages, each whole in one call: Home's dashboards, the catalog, an entity's page with its tabs and scorecards, Self-Service, runs and approvals. It runs actions and workflows as the user, and follows their runs.
- `/api/mcp/builder` is for builders, and refuses members. Its tools change the portal's configuration: blueprints and their sources, connections and their syncs, scorecards and dashboards, through drafts that are validated, previewed for impact and then published.

## License

[MIT](LICENSE)
