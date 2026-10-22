import type { IntegrationDetail, IntegrationSummary, Run } from "@/features/integrations";
import type { Handler } from "./api";

export const snapshotRun: Run = {
  id: 3,
  trigger: "push",
  actor: "pipelines",
  kind: "service",
  mode: "snapshot",
  status: "partial",
  started_at: "2026-10-25T09:01:00Z",
  finished_at: "2026-10-25T09:01:01Z",
  counts: { fetched: 1, upserted: 1, deleted: 0, skipped: 0 },
  warnings: [
    "checkout: dropped the owner link to team billing, which does not exist",
    "kept 2 service entities that the snapshot leaves out",
  ],
};

export const upsertRun: Run = {
  id: 1,
  trigger: "push",
  actor: "pipelines",
  kind: "service",
  mode: "upsert",
  status: "succeeded",
  started_at: "2026-10-25T09:00:00Z",
  finished_at: "2026-10-25T09:00:01Z",
  counts: { fetched: 3, upserted: 3, deleted: 0, skipped: 0 },
  warnings: [],
};

export const failedSync: Run = {
  id: 2,
  trigger: "manual",
  actor: "bob@example.test",
  status: "failed",
  started_at: "2026-10-25T09:00:00Z",
  finished_at: "2026-10-25T09:00:02Z",
  counts: { fetched: 0, upserted: 0, deleted: 0, skipped: 0 },
  warnings: [],
  error: "could not reach GitLab at https://gitlab.example.test: no such host",
};

export const manual: IntegrationSummary = {
  identifier: "manual",
  title: "Manual",
  description: "Entities people create and edit in the portal",
  connector: "manual",
  manual: true,
  pull: false,
  entity_count: 4,
};

export const ci: IntegrationDetail = {
  identifier: "ci",
  title: "CI",
  description: "Acme's pipelines push the services they deploy",
  connector: "push",
  manual: false,
  pull: false,
  entity_count: 3,
  last_run: snapshotRun,
  entities: [{ blueprint: "service", title: "Service", count: 3 }],
  kinds: [{ kind: "service", blueprint: "service", blueprint_title: "Service", canonical: false }],
  secrets: [],
  ingest_url: "https://portal.example.test/api/v1/ingest/ci",
};

export const gitlab: IntegrationDetail = {
  identifier: "gitlab",
  title: "GitLab",
  connector: "gitlab",
  manual: false,
  pull: true,
  interval: "1h",
  entity_count: 0,
  last_run: failedSync,
  next_run_at: "2099-10-25T10:00:00Z",
  entities: [],
  kinds: [
    { kind: "project", blueprint: "repository", blueprint_title: "Repository", canonical: false },
  ],
  secrets: [{ name: "token", variable: "GITLAB_TOKEN" }],
};

export const integrationsApi: Record<string, Handler> = {
  "GET /api/v1/integrations": () => Response.json({ data: { integrations: [manual, ci, gitlab] } }),
  "GET /api/v1/integrations/ci": () => Response.json({ data: ci }),
  "GET /api/v1/integrations/ci/runs": () =>
    Response.json({ data: { runs: [snapshotRun, upsertRun] } }),
  "GET /api/v1/integrations/gitlab": () => Response.json({ data: gitlab }),
  "GET /api/v1/integrations/gitlab/runs": () => Response.json({ data: { runs: [failedSync] } }),
};
