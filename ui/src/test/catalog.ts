import type { Blueprint, Entity, EntityPage } from "@/features/catalog";
import type { Handler } from "./api";

const tableDefaults = { sort: { by: "title", direction: "asc" as const }, filters: [] };

const displayDefaults = { badges: [], groups: [] };

export const team: Blueprint = {
  identifier: "team",
  title: "Team",
  description: "A group of people who own software",
  icon: "users",
  entity_count: 2,
  properties: [
    {
      key: "email",
      title: "Email",
      type: "string",
      format: "email",
      display: "email",
      hidden: false,
      sortable: true,
    },
  ],
  relations: [],
  inverses: [{ source: "service", source_title: "Service", relation: "owner", title: "Services" }],
  derived: [],
  display: displayDefaults,
  table: { ...tableDefaults, columns: ["email"] },
  columns: ["email"],
  fed_by: [],
};

export const service: Blueprint = {
  identifier: "service",
  title: "Service",
  description: "A deployable service",
  icon: "server",
  entity_count: 1,
  properties: [
    {
      key: "tier",
      title: "Tier",
      type: "string",
      enum: ["1", "2", "3"],
      display: "badge",
      hidden: false,
      sortable: true,
      filter_kind: "enum",
    },
    {
      key: "repositoryUrl",
      title: "Repository",
      type: "string",
      format: "url",
      display: "link",
      hidden: false,
      sortable: true,
    },
    {
      key: "public",
      title: "Public",
      type: "boolean",
      display: "icon",
      hidden: false,
      sortable: true,
      filter_kind: "boolean",
    },
    {
      key: "launched",
      title: "Launched",
      type: "string",
      format: "date",
      display: "relative",
      hidden: false,
      sortable: true,
      filter_kind: "date",
    },
  ],
  relations: [
    {
      key: "owner",
      title: "Owner",
      target: "team",
      target_title: "Team",
      many: false,
    },
  ],
  inverses: [],
  derived: [],
  display: displayDefaults,
  table: { ...tableDefaults, columns: ["tier", "repositoryUrl"] },
  columns: ["tier", "repositoryUrl"],
  fed_by: [{ identifier: "ci", title: "CI", connector: "push" }],
};

export const ledger: Entity = {
  blueprint: "service",
  identifier: "ledger",
  title: "Ledger",
  properties: {
    tier: "1",
    repositoryUrl: "https://gitlab.example.test/acme/ledger",
    public: false,
    launched: "2026-03-02",
  },
  integration: { identifier: "ci", title: "CI", manual: false },
  relations: [
    {
      key: "owner",
      title: "Owner",
      target: "team",
      many: false,
      entities: [{ blueprint: "team", identifier: "payments", title: "Payments" }],
    },
  ],
  incoming: [],
  derived: {},
  created_at: "2026-10-20T09:00:00Z",
  updated_at: "2026-10-21T09:00:00Z",
};

export const paymentsApi: Entity = {
  ...ledger,
  identifier: "payments-api",
  title: "Payments API",
  properties: { tier: "2" },
  integration: { identifier: "manual", title: "Manual", manual: true },
};

export const servicePage: EntityPage = {
  entities: [
    {
      identifier: "ledger",
      title: "Ledger",
      integration: ledger.integration,
      properties: ledger.properties,
      relations: { owner: [{ blueprint: "team", identifier: "payments", title: "Payments" }] },
      derived: {},
      updated_at: ledger.updated_at,
    },
    {
      identifier: "payments-api",
      title: "Payments API",
      integration: paymentsApi.integration,
      properties: paymentsApi.properties,
      relations: {},
      derived: {},
      updated_at: paymentsApi.updated_at,
    },
  ],
  offset: 0,
  limit: 25,
  total: 2,
  sort: "title",
  dir: "asc",
  integrations: [
    { identifier: "ci", title: "CI", manual: false, count: 1 },
    { identifier: "manual", title: "Manual", manual: true, count: 1 },
  ],
};

export const catalogApi: Record<string, Handler> = {
  "GET /api/v1/catalog/blueprints": () => Response.json({ data: { blueprints: [service, team] } }),
  "GET /api/v1/catalog/changes": () => Response.json({ data: { changes: [] } }),
  "GET /api/v1/catalog/blueprints/service": () => Response.json({ data: service }),
  "GET /api/v1/catalog/blueprints/team": () => Response.json({ data: team }),
  "GET /api/v1/catalog/blueprints/service/entities": () => Response.json({ data: servicePage }),
  "GET /api/v1/catalog/blueprints/service/entities/ledger": () => Response.json({ data: ledger }),
  "GET /api/v1/catalog/blueprints/service/entities/payments-api": () =>
    Response.json({ data: paymentsApi }),
};
