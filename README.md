# Golden Portal

Golden Portal brings your whole developer platform into a single portal, with golden paths for developers: the paved way to get things done, and the standards to meet along it. Developers get one place to see what they own, how it's doing and what to fix, and to do routine work themselves.

It's built on a catalog of your repositories, services, teams and anything else you choose, kept in step with the systems that hold them.

## Features

- **Catalog**: model what you care about as blueprints, such as repositories, services, workloads or incidents, and fill them from GitLab, SonarQube, authentik or any JSON API, pulled on a schedule or pushed to the portal.
- **Self-service**: actions and workflows with forms, approvals and steps that call your systems and write back to the catalog, such as requesting access or scaffolding a service.
- **Scorecards**: tiers of checks, such as production readiness, with a fix shown beside each failing check.
- **Dashboards**: Home tabs and entity page tabs of stats, charts and tables, each number linking to the entities behind it.
- **Configuration as YAML**: every connection, blueprint, scorecard, dashboard and action is a document, drafted, previewed and published in the portal.
- **Agents**: AI assistants read the portal and run actions over MCP, signed in as the user.

## Quick Start

The portal runs as one container next to PostgreSQL, and signs people in with any OpenID Connect provider.

```sh
docker run -p 8000:8000 \
  -e DB_DIALECT=postgres -e DB_HOST=db.example.com -e DB_PORT=5432 \
  -e DB_NAME=goldenportal -e DB_USER=goldenportal -e DB_PASSWORD=... \
  -e GP_BASE_URL=https://portal.example.com \
  -e GP_OIDC_ISSUER=https://sso.example.com -e GP_OIDC_CLIENT_ID=... -e GP_OIDC_CLIENT_SECRET=... \
  -e GP_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
  ghcr.io/dvjn/goldenportal:latest
```

- The portal migrates its database as it starts.
- Sign in as `GP_BOOTSTRAP_ADMIN_EMAIL` to become the first admin, then follow [Setup](docs/setup.md).
- Secrets that connections and actions use are environment variables starting with `GP_SECRET_`.

## Documentation

- [How It Works](docs/how-it-works.md): the concepts, on one page.
- [Setup](docs/setup.md): from a first connection to a dashboard and a scorecard.
- [Builder Docs](docs/README.md): syncing, blueprints, connectors, scorecards, dashboards, actions and workflows.

## Development

With [mise](https://mise.jdx.dev) and Docker:

```sh
mise install        # tools
mise run dev        # PostgreSQL, a mock sign-in and APIs, the API and UI with reload, and seed data
mise run test       # unit tests
mise run test:e2e   # tests against a database
mise run lint
```

## Requirements

- PostgreSQL 16 or later, with the `pg_trgm` extension, which the portal creates when its database user has `CREATE` on the database.
- An OpenID Connect provider for sign-in.

## Agents

AI assistants connect to the portal over MCP, signing in with the portal's own OAuth. There are two servers, at the portal's public URL (`GP_BASE_URL`):

- `/api/mcp` is for everyone. Its tools read the portal's pages, each whole in one call: Home's dashboards, the catalog, an entity's page with its tabs and scorecards, Self-Service, runs and approvals. It runs actions and workflows as the user, and follows their runs.
- `/api/mcp/builder` is for builders, and refuses members. Its tools change the portal's configuration: blueprints and their sources, connections and their syncs, scorecards and dashboards, through drafts that are validated, previewed for impact and then published.

## License

[MIT](LICENSE)
