import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint, BlueprintSpec } from "@/features/blueprints";
import type { EntityDetail } from "@/features/catalog";
import type { Dashboard, HomeDashboard } from "@/features/dashboards";
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

const overview: HomeDashboard = {
  identifier: "overview",
  title: "Overview",
  order: 1,
  filters: [],
  defaults: {},
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

const welcome: HomeDashboard = {
  identifier: "welcome",
  title: "Welcome",
  order: 2,
  filters: [],
  defaults: {},
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
  it("shows table widget tiers and distinguishes no tier from not evaluated", async () => {
    stubApi({
      ...signedInMember,
      "GET home": ok([
        {
          ...overview,
          widgets: [
            {
              identifier: "tiers",
              type: "table",
              title: "Services",
              blueprint: "service",
              width: 12,
              columns: ["$scorecard.readiness"],
            },
          ],
        },
      ]),
      "GET blueprints/service": ok(service),
      "GET scorecard-columns": ok([
        {
          identifier: "readiness",
          title: "Readiness",
          tiers: [{ name: "gold", title: "Gold", color: "amber", criteria: [] }],
        },
      ]),
      "GET dashboards/overview/widgets/tiers": ok({
        entities: [
          {
            ...ledger,
            identifier: "gold",
            title: "Gold Service",
            scorecards: { readiness: { tier: "gold", score: 100 } },
          },
          {
            ...ledger,
            identifier: "none",
            title: "Unready",
            scorecards: { readiness: { tier: "", score: 0 } },
          },
          { ...ledger, identifier: "new", title: "New Service" },
        ],
        total: 3,
      }),
    });
    renderRoute("/");
    expect(await screen.findByRole("columnheader", { name: "Readiness" })).toBeInTheDocument();
    expect(await screen.findByText("Gold")).toBeInTheDocument();
    expect(screen.getByText("No tier")).toBeInTheDocument();
    expect(screen.getByText("Not evaluated")).toBeInTheDocument();
  });

  it("opens the preferred visible dashboard unless the URL chooses another", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInMember,
      "GET home": ok([
        { ...overview, widgets: [] },
        { ...welcome, preferred: true },
      ]),
    });
    renderRoute("/");
    expect(await screen.findByRole("tab", { name: "Welcome", selected: true })).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Overview" }));
    expect(
      await screen.findByRole("tab", { name: "Overview", selected: true }),
    ).toBeInTheDocument();
  });

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

  it("lets a blueprint replace an entity's overview, with markdown filled in for the viewer", async () => {
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
                    text: "Owned by **payments**. Hi {{ $me.name }}.",
                  },
                ],
              },
            ],
          },
        }),
      ),
      "GET entities/service/ledger": ok(ledger),
      "GET entity-scorecards": ok([]),
      "GET blueprints/service/widgets/entity/overview/about": ok({
        text: "Owned by **payments**. Hi Ada.",
      }),
    });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("tab", { name: "Summary", selected: true })).toBeInTheDocument();
    expect(await screen.findByText("payments")).toBeInTheDocument();
    expect(screen.getByText(/Hi Ada\./)).toBeInTheDocument();
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

  it("sets a dashboard's filters from its bar, keeping them in the URL", async () => {
    const user = userEvent.setup();
    const teams: HomeDashboard = {
      identifier: "teams",
      title: "Teams",
      order: 1,
      defaults: { tier: ["critical"] },
      filters: [
        { identifier: "team", title: "Team", type: "relation", relation: "team" },
        {
          identifier: "tier",
          title: "Tier",
          type: "enum",
          values: ["critical", "standard"],
          multiple: true,
          default: ["critical"],
        },
      ],
      widgets: [
        {
          identifier: "count",
          type: "stat",
          title: "Services",
          width: 3,
          blueprint: "service",
          aggregate: "count",
          filter: {
            and: [
              { property: "owner", op: "eq", value: "$filter.team" },
              { property: "tier", op: "eq", value: "$filter.tier" },
            ],
          },
        },
      ],
    };
    const requests = stubApi({
      ...signedInMember,
      "GET home": ok([teams]),
      "GET blueprints/service": ok(service),
      "GET entities": ok({
        entities: [{ ...ledger, blueprint: "team", identifier: "payments", title: "Payments" }],
        total: 1,
      }),
      "GET dashboards/teams/widgets/count": ok({ value: 3 }),
    });
    const { router } = renderRoute("/");

    // The default applies until someone changes it; an unset filter drops
    // its condition from the link too.
    const count = await screen.findByRole("link", { name: "3" });
    expect(decodeURIComponent(count.getAttribute("href") ?? "")).toContain('"tier":["critical"]');
    expect(decodeURIComponent(count.getAttribute("href") ?? "")).not.toContain("owner");
    let url = new URL(requests["GET dashboards/teams/widgets/count"].url);
    expect(url.searchParams.getAll("filter.tier")).toEqual(["critical"]);
    expect(url.searchParams.getAll("filter.team")).toEqual([""]);
    expect(screen.queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^Team/ }));
    await user.click(await screen.findByRole("option", { name: "Payments" }));
    await user.click(screen.getByRole("button", { name: /^Tier/ }));
    await user.click(await screen.findByRole("option", { name: "standard" }));

    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({
        team: "payments",
        tier: ["critical", "standard"],
      });
      url = new URL(requests["GET dashboards/teams/widgets/count"].url);
      expect(url.searchParams.getAll("filter.team")).toEqual(["payments"]);
      expect(url.searchParams.getAll("filter.tier")).toEqual(["critical", "standard"]);
    });
    const narrowed = await screen.findByRole("link", { name: "3" });
    expect(decodeURIComponent(narrowed.getAttribute("href") ?? "")).toContain(
      '"owner":["payments"]',
    );

    await user.click(screen.getByRole("button", { name: "Reset" }));
    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({});
      url = new URL(requests["GET dashboards/teams/widgets/count"].url);
      expect(url.searchParams.getAll("filter.tier")).toEqual(["critical"]);
    });
  });

  // mine starts its team filter at what a lookup finds for the viewer.
  const mine: HomeDashboard = {
    identifier: "mine",
    title: "Mine",
    order: 1,
    filters: [
      {
        identifier: "team",
        title: "Team",
        type: "relation",
        relation: "team",
        multiple: true,
        lookup: { from: "team", where: { property: "members", op: "contains", value: "$me" } },
      },
    ],
    widgets: [
      {
        identifier: "count",
        type: "stat",
        title: "Services",
        width: 3,
        blueprint: "service",
        aggregate: "count",
        filter: { property: "owner", op: "eq", value: "$filter.team" },
      },
    ],
    defaults: {},
  };
  const mineStubs = {
    ...signedInMember,
    "GET blueprints/service": ok(service),
    "GET entities": ok({
      entities: [{ ...ledger, blueprint: "team", identifier: "payments", title: "Payments" }],
      total: 1,
    }),
    "GET dashboards/mine/widgets/count": ok({ value: 3 }),
  };

  it("starts a filter at what its lookup default found", async () => {
    const requests = stubApi({
      ...mineStubs,
      "GET home": ok([{ ...mine, defaults: { team: ["payments"] } }]),
    });
    renderRoute("/");

    // What was found is picked, and sent as if picked by hand.
    const count = await screen.findByRole("link", { name: "3" });
    expect(decodeURIComponent(count.getAttribute("href") ?? "")).toContain('"owner":["payments"]');
    const url = new URL(requests["GET dashboards/mine/widgets/count"].url);
    expect(url.searchParams.getAll("filter.team")).toEqual(["payments"]);
    expect(screen.getByRole("button", { name: /^Team/ })).toHaveTextContent("Payments");
    expect(screen.queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();
    expect(screen.queryByText(/Nothing matched you/)).not.toBeInTheDocument();
  });

  it("says why a lookup default failed and shows nothing, until cleared", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...mineStubs,
      "GET home": ok([
        {
          ...mine,
          defaults: { team: [] },
          problems: { team: 'the default can\'t be found: team has no property "people"' },
        },
      ]),
    });
    renderRoute("/");

    // The dashboard still loads, with the filter at nothing.
    expect(
      await screen.findByText(
        /For Team, the default can't be found: team has no property "people", so the widgets show nothing/,
      ),
    ).toBeInTheDocument();
    expect(await screen.findByText("3")).toBeInTheDocument();
    expect(screen.queryByText(/Nothing matched you/)).not.toBeInTheDocument();
    expect(
      new URL(requests["GET dashboards/mine/widgets/count"].url).searchParams.has("filter.team"),
    ).toBe(false);

    await user.click(screen.getByRole("button", { name: "Show everything" }));
    await vi.waitFor(() => {
      const url = new URL(requests["GET dashboards/mine/widgets/count"].url);
      expect(url.searchParams.getAll("filter.team")).toEqual([""]);
    });
    expect(screen.queryByText(/the default can't be found/)).not.toBeInTheDocument();
  });

  it("previews a dashboard with its filter bar, from the draft until it's published", async () => {
    const user = userEvent.setup();
    const draft: Dashboard = {
      identifier: "mine",
      title: "Mine",
      published: null,
      draft: {
        source: "kind: Dashboard",
        spec: { ...mine, title: "Hello {{ $me.name }}" },
        issues: [],
        updatedAt: "2026-10-24T10:00:00Z",
        updatedBy: "ada@example.com",
      },
      createdAt: "2026-10-24T10:00:00Z",
    };
    const requests = stubApi({
      ...mineStubs,
      "GET me": ok(ada),
      "GET dashboards/mine": ok(draft),
      "GET dashboards/mine/versions": ok([]),
      "GET dashboards/mine/view": ok({
        ...mine,
        title: "Hello Ada Lovelace",
        widgets: [{ ...mine.widgets[0], title: "Services of Ada Lovelace" }],
        defaults: { team: ["payments"] },
      }),
    });
    renderRoute("/dashboards/mine?tab=overview");

    // The view and the widgets come from the draft, filled in for the builder.
    expect(await screen.findByText("3")).toBeInTheDocument();
    expect(screen.getByText("Services of Ada Lovelace")).toBeInTheDocument();
    expect(new URL(requests["GET dashboards/mine/view"].url).searchParams.get("draft")).toBe(
      "true",
    );
    expect(screen.getByRole("button", { name: /^Team/ })).toHaveTextContent("Payments");
    let url = new URL(requests["GET dashboards/mine/widgets/count"].url);
    expect(url.searchParams.get("draft")).toBe("true");
    expect(url.searchParams.getAll("filter.team")).toEqual(["payments"]);

    // Picking stays with the preview, not in the URL.
    await user.click(screen.getByRole("button", { name: /^Team/ }));
    await user.click(await screen.findByRole("option", { name: "Payments" }));
    await vi.waitFor(() => {
      url = new URL(requests["GET dashboards/mine/widgets/count"].url);
      expect(url.searchParams.getAll("filter.team")).toEqual([""]);
    });
    expect(window.location.search).toBe("");
    expect(screen.getByRole("button", { name: "Reset" })).toBeInTheDocument();
  });

  it("starts a filter at nothing when its lookup default found none, until cleared", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...mineStubs,
      "GET home": ok([{ ...mine, defaults: { team: [] } }]),
    });
    const { router } = renderRoute("/");

    // Nothing is picked, the request leaves the filter to the server, which
    // finds nothing again, and there's no link, since the catalog can't
    // show nothing.
    await screen.findByText(/Nothing matched you for Team/);
    expect(await screen.findByText("3")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "3" })).not.toBeInTheDocument();
    let url = new URL(requests["GET dashboards/mine/widgets/count"].url);
    expect(url.searchParams.has("filter.team")).toBe(false);
    expect(screen.getByRole("button", { name: "Team" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();

    // Clearing is a change from nothing, so it goes in the URL as "".
    await user.click(screen.getByRole("button", { name: "Show everything" }));
    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({ team: "" });
      url = new URL(requests["GET dashboards/mine/widgets/count"].url);
      expect(url.searchParams.getAll("filter.team")).toEqual([""]);
    });
    expect(screen.queryByText(/Nothing matched you/)).not.toBeInTheDocument();
    const all = await screen.findByRole("link", { name: "3" });
    expect(decodeURIComponent(all.getAttribute("href") ?? "")).not.toContain("owner");

    await user.click(screen.getByRole("button", { name: "Reset" }));
    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({});
      url = new URL(requests["GET dashboards/mine/widgets/count"].url);
      expect(url.searchParams.has("filter.team")).toBe(false);
    });
    await screen.findByText(/Nothing matched you for Team/);
  });
});
