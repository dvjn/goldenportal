import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint, BlueprintSpec } from "@/features/blueprints";
import type { EntityDetail } from "@/features/catalog";
import type { DashboardSpec } from "@/features/dashboards";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const signedInMember = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) };

const service: Blueprint = {
  identifier: "service",
  title: "Service",
  published: {
    version: 1,
    source: "",
    spec: {
      identifier: "service",
      title: "Service",
      properties: [
        {
          identifier: "tier",
          title: "Tier",
          type: "string",
          enum: ["critical", "standard"],
          display: { colors: { critical: "red" } },
        },
        { identifier: "owner", title: "Owner", type: "relation", relation: "team" },
        {
          identifier: "coverage",
          title: "Coverage",
          type: "number",
          display: { as: "progress", unit: "%" },
        },
        {
          identifier: "critical",
          title: "Critical",
          type: "boolean",
          compute: ".",
          display: { hidden: true },
        },
      ],
    },
    publishedAt: "2026-10-24T10:00:00Z",
    publishedBy: "",
  },
  draft: null,
  createdAt: "2026-10-24T10:00:00Z",
};

const ledger: EntityDetail = {
  blueprint: "service",
  identifier: "ledger",
  title: "Ledger",
  properties: { tier: "critical", owner: "payments", coverage: 72, critical: true },
  related: { owner: [{ identifier: "payments", title: "Payments" }] },
  integration: "registry",
  createdAt: "2026-10-24T10:00:00Z",
  updatedAt: "2026-10-24T10:00:00Z",
  incoming: [],
};

const overview: DashboardSpec = {
  identifier: "overview",
  title: "Overview",
  order: 1,
  widgets: [
    {
      identifier: "critical",
      type: "stat",
      title: "Critical Services",
      width: 3,
      blueprint: "service",
      aggregate: "count",
      filter: { property: "tier", op: "eq", value: "critical" },
    },
    {
      identifier: "byOwner",
      type: "bar",
      title: "By Owner",
      width: 6,
      blueprint: "service",
      groupBy: "owner",
    },
  ],
};

const welcome: DashboardSpec = {
  identifier: "welcome",
  title: "Welcome",
  order: 2,
  widgets: [
    { identifier: "hello", type: "markdown", width: 6, text: "Fix data **at its source**." },
  ],
};

// withTabs gives the service blueprint tabs on its pages.
function withTabs(pages: Pick<BlueprintSpec, "catalog" | "entity">): Blueprint {
  const published = service.published as NonNullable<Blueprint["published"]>;
  return { ...service, published: { ...published, spec: { ...published.spec, ...pages } } };
}

describe("dashboards and page tabs", () => {
  it("shows Home's dashboards as tabs, each widget linking into the catalog", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInMember,
      "GET home": ok([overview, welcome]),
      "GET blueprints/service": ok(service),
      "GET dashboards/overview/widgets/critical": ok({ value: 3 }),
      "GET dashboards/overview/widgets/byOwner": ok({
        groups: [{ value: "payments", title: "Payments", count: 2 }],
        none: 1,
      }),
    });
    renderRoute("/");

    expect(
      await screen.findByRole("tab", { name: "Overview", selected: true }),
    ).toBeInTheDocument();
    const count = await screen.findByRole("link", { name: "3" });
    expect(count.getAttribute("href")).toMatch(/^\/catalog\/service\?view=entities&f=/);
    expect(decodeURIComponent(count.getAttribute("href") ?? "")).toContain('"tier":["critical"]');

    const payments = await screen.findByRole("link", { name: "Payments" });
    expect(decodeURIComponent(payments.getAttribute("href") ?? "")).toContain(
      '"owner":["payments"]',
    );
    expect(screen.getByText("None")).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "Welcome" }));
    expect(await screen.findByText("at its source")).toBeInTheDocument();
  });

  it("opens a blueprint's catalog page on its own tabs, with the entities a tab away", async () => {
    const user = userEvent.setup();
    const laidOut = withTabs({
      catalog: {
        tabs: [
          {
            identifier: "overview",
            title: "Overview",
            widgets: [
              {
                identifier: "critical",
                type: "table",
                title: "Critical",
                width: 12,
                blueprint: "service",
                columns: ["tier", "coverage"],
                filter: { property: "critical", op: "eq", value: true },
              },
            ],
          },
        ],
      },
    });
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(laidOut),
      "GET entities": ok({ entities: [ledger], total: 1 }),
      "GET entities/facets": ok([]),
      "GET blueprints/service/widgets/catalog/overview/critical": ok({
        entities: [ledger],
        total: 1,
      }),
    });
    renderRoute("/catalog/service");

    expect(
      await screen.findByRole("tab", { name: /Overview/, selected: true }),
    ).toBeInTheDocument();
    const row = (await screen.findByRole("link", { name: "Ledger" })).closest("tr") as HTMLElement;
    expect(within(row).getByText("72%")).toBeInTheDocument();
    expect(within(row).getByText("critical")).toHaveClass("bg-red-500/15");

    await user.click(screen.getByRole("tab", { name: /Entities/ }));
    expect(await screen.findByRole("searchbox", { name: "Search service" })).toBeInTheDocument();
  });

  it("lets a blueprint replace an entity's overview", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(
        withTabs({
          entity: {
            tabs: [
              {
                identifier: "overview",
                title: "Summary",
                widgets: [
                  {
                    identifier: "about",
                    type: "markdown",
                    width: 12,
                    text: "Owned by **payments**.",
                  },
                ],
              },
            ],
          },
        }),
      ),
      "GET entities/service/ledger": ok(ledger),
      "GET entity-scorecards": ok([]),
    });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("tab", { name: "Summary", selected: true })).toBeInTheDocument();
    expect(await screen.findByText("payments")).toBeInTheDocument();
    expect(screen.queryByText("Coverage")).not.toBeInTheDocument();
  });

  it("scopes a blueprint's entity tabs to the entity, after its overview", async () => {
    const user = userEvent.setup();
    const laidOut = withTabs({
      entity: {
        tabs: [
          {
            identifier: "deployments",
            title: "Deployments",
            widgets: [
              {
                identifier: "total",
                type: "stat",
                title: "Deployments",
                width: 3,
                blueprint: "deployment",
                aggregate: "count",
                filter: { property: "service", op: "eq", value: "$entity" },
              },
            ],
          },
        ],
      },
    });
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(laidOut),
      "GET blueprints/deployment": ok({ ...service, identifier: "deployment" }),
      "GET entities/service/ledger": ok(ledger),
      "GET entity-scorecards": ok([]),
      "GET blueprints/service/widgets/entity/deployments/total": ok({ value: 12 }),
    });
    renderRoute("/catalog/service/ledger");

    // The overview leaves out hidden properties.
    expect(await screen.findByText("Coverage")).toBeInTheDocument();
    expect(screen.queryByText("Critical")).not.toBeInTheDocument();

    const tabs = screen.getAllByRole("tab").map((tab) => tab.textContent);
    expect(tabs.slice(0, 2)).toEqual(["Overview", "Deployments"]);
    await user.click(screen.getByRole("tab", { name: "Deployments" }));

    const total = await screen.findByRole("link", { name: "12" });
    expect(decodeURIComponent(total.getAttribute("href") ?? "")).toContain('"service":["ledger"]');
    const url = new URL(requests["GET blueprints/service/widgets/entity/deployments/total"].url);
    expect(url.searchParams.get("entity")).toBe("ledger");
  });
});
