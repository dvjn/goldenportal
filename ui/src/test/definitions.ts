import type { Definitions, Plan } from "@/features/definitions";
import type { Handler } from "./api";

export const teamYAML = `kind: Blueprint
identifier: team
title: Team
properties:
  email:
    title: Email
    type: string
`;

export const serviceYAML = `kind: Blueprint
identifier: service
title: Service
relations:
  owner:
    title: Owner
    target: team
`;

export const definitions: Definitions = {
  revision: 1,
  documents: [
    { kind: "Blueprint", identifier: "service", title: "Service", yaml: serviceYAML },
    { kind: "Blueprint", identifier: "team", title: "Team", yaml: teamYAML },
    {
      kind: "Integration",
      identifier: "ci",
      title: "CI",
      yaml: "kind: Integration\nidentifier: ci\ntitle: CI\nconnector: push\n",
    },
  ],
};

export const plan: Plan = {
  id: "plan-1",
  expires_at: "2026-10-22T09:15:00Z",
  base_revision: 1,
  summary: { added: 0, changed: 1, removed: 1 },
  changes: [
    {
      kind: "Blueprint",
      identifier: "team",
      title: "Team",
      action: "change",
      fields: [{ path: "properties.email.title", before: "Email", after: "Team email" }],
    },
    { kind: "Integration", identifier: "ci", title: "CI", action: "remove" },
  ],
  impact: {
    deleted_entities: [
      {
        integration: "ci",
        count: 12,
        examples: [{ blueprint: "service", identifier: "ledger", title: "Ledger" }],
      },
    ],
    cleared_values: [],
    dropped_links: [
      {
        blueprint: "service",
        name: "owner",
        count: 1,
        examples: [{ blueprint: "service", identifier: "payments-api", title: "Payments API" }],
      },
    ],
  },
};

export const definitionsApi: Record<string, Handler> = {
  "GET /api/v1/definitions": () => Response.json({ data: definitions }),
  "GET /api/v1/definitions/schema": () => Response.json({ data: { type: "object" } }),
};
