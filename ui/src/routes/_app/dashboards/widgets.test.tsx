import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint, BlueprintSpec } from "@/features/blueprints";
import type { EntityDetail, Filter } from "@/features/catalog";
import type { Dashboard, HomeDashboard } from "@/features/dashboards";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

// stat finds a stat widget by its link's name, once its card shows value.
async function stat(title: RegExp, value: string) {
  const link = await screen.findByRole("link", { name: title });
  await vi.waitFor(() => expect(link.closest("[data-slot=card]")).toHaveTextContent(value));
  return link;
}

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

const services = { name: "services", blueprint: "service" };
const tierProperty = (service.published as NonNullable<Blueprint["published"]>).spec.properties[0];
const critical = { property: "tier", op: "eq", value: "critical" };

const overview: HomeDashboard = {
  identifier: "overview",
  title: "Overview",
  order: 1,
  inputs: [],
  defaults: {},
  widgets: [
    {
      identifier: "critical",
      type: "stat",
      title: "Critical Services",
      width: 3,
      value: { query: { blueprint: "service", filter: critical } },
      link: { query: { blueprint: "service", filter: critical } },
    },
    {
      identifier: "byOwner",
      type: "bar",
      title: "By Owner",
      width: 6,
      query: services,
      groupBy: "owner",
      link: { query: services, group: true },
    },
  ],
};

// loaded answers a page's data request with each widget's data.
const loaded = (data: Record<string, unknown>) =>
  ok({ widgets: Object.fromEntries(Object.entries(data).map(([id, d]) => [id, { data: d }])) });

// linked is a stat's data as the server gives it, its link filled in.
const linked = (value: number, filter?: Filter, blueprint = "service") => ({
  value,
  link: { blueprint, filter },
});

// countLike answers a dashboard's count of services as the server would,
// its link narrowed by what the request picked for team: absent is the
// default query's nothing, empty unset, and anything else those teams,
// which it names.
function countLike(request: Request) {
  const picked = new URL(request.url).searchParams;
  const and: Filter[] = [];
  const team = picked.getAll("input.team");
  if (!picked.has("input.team")) and.push({ property: "owner", op: "in" });
  else if (team.some(Boolean)) and.push({ property: "owner", op: "in", values: team });
  const tier = picked.getAll("input.tier").filter(Boolean);
  if (tier.length > 0) and.push({ property: "tier", op: "in", values: tier });
  const inputs = team.some(Boolean)
    ? { team: team.map((identifier) => ({ identifier, title: teamTitles[identifier] })) }
    : undefined;
  return Response.json({ data: { widgets: { count: { data: linked(3, { and }) } }, inputs } });
}

const teamTitles: Record<string, string> = { payments: "Payments" };

const welcome: HomeDashboard = {
  identifier: "welcome",
  title: "Welcome",
  order: 2,
  inputs: [],
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
  it("lists dashboards in Home's order with who sees them", async () => {
    stubApi({
      "GET config": acmeConfig,
      "GET me": ok(ada),
      "GET dashboards": ok([
        {
          identifier: "teams",
          title: "Teams",
          publishedVersion: 0,
          hasDraft: true,
          stats: { widgets: 1 },
          facts: { order: 2, group: { title: "Deep Dives" }, visibility: null, defaultFor: null },
          updatedAt: "2026-10-24T10:00:00Z",
        },
        {
          identifier: "services",
          title: "Services",
          publishedVersion: 3,
          hasDraft: false,
          stats: { widgets: 4 },
          facts: {
            order: 1,
            visibility: { roles: ["builder"], groups: ["payments"] },
            defaultFor: { groups: ["payments"] },
          },
          updatedAt: "2026-10-24T10:00:00Z",
        },
      ]),
    });
    renderRoute("/dashboards");

    await screen.findByText("Services");
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getAllByRole("cell")[1].textContent)).toEqual(["1", "2"]);
    const [services, teams] = rows.map((row) => within(row));
    expect(services.getByTitle("Role")).toHaveTextContent("builder");
    expect(services.getAllByTitle("Group")).toHaveLength(2);
    expect(services.getByText("Published")).toBeInTheDocument();
    expect(teams.getByText("Deep Dives")).toBeInTheDocument();
    expect(teams.getByText("Everyone")).toBeInTheDocument();
    expect(teams.getByText("Draft")).toBeInTheDocument();
    expect(teams.queryByText("Published")).not.toBeInTheDocument();
  });

  it("shows table widget tiers, linked to each result, and tells no tier from not evaluated", async () => {
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
              query: services,
              width: 12,
              columns: [{ path: "readiness" }],
            },
          ],
        },
      ]),
      "GET dashboards/overview/data": loaded({
        tiers: {
          blueprint: "service",
          total: 3,
          columns: [
            {
              title: "Readiness",
              path: "readiness",
              scorecard: {
                identifier: "readiness",
                blueprint: "service",
                tiers: [{ name: "gold", title: "Gold", color: "amber", criteria: [] }],
              },
            },
          ],
          rows: [
            {
              identifier: "gold",
              title: "Gold Service",
              values: ["gold"],
              links: [[{ identifier: "gold" }]],
            },
            {
              identifier: "none",
              title: "Unready",
              values: [""],
              links: [[{ identifier: "none" }]],
            },
            { identifier: "new", title: "New Service", values: [null], links: [null] },
          ],
        },
      }),
    });
    renderRoute("/");
    expect(await screen.findByRole("columnheader", { name: "Readiness" })).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Gold" })).toHaveAttribute(
      "href",
      "/catalog/service/gold?tab=readiness",
    );
    expect(screen.getByText("No tier")).toBeInTheDocument();
    expect(screen.getByTitle("Not evaluated")).toBeInTheDocument();
  });

  it("shows path columns with the definition of the field they end on", async () => {
    stubApi({
      ...signedInMember,
      "GET home": ok([
        {
          ...overview,
          widgets: [
            {
              identifier: "list",
              type: "table",
              title: "Deployments",
              query: { blueprint: "deployment" },
              width: 12,
              columns: [{ path: "service.owner" }, { path: "service.readiness.failed" }],
            },
          ],
        },
      ]),
      "GET dashboards/overview/data": loaded({
        list: {
          blueprint: "deployment",
          total: 1,
          columns: [
            {
              title: "Owner",
              path: "service.owner",
              property: { identifier: "owner", title: "Owner", type: "relation", relation: "team" },
            },
            { title: "Failed", path: "service.readiness.failed" },
          ],
          rows: [
            {
              identifier: "d1",
              title: "Ledger 1.2",
              values: ["payments", 2],
              links: [[{ identifier: "payments", title: "Payments" }], null],
            },
          ],
        },
      }),
    });
    renderRoute("/");
    expect(await screen.findByRole("link", { name: "Payments" })).toHaveAttribute(
      "href",
      "/catalog/team/payments",
    );
    expect(screen.getByRole("link", { name: "Ledger 1.2" })).toHaveAttribute(
      "href",
      "/catalog/deployment/d1",
    );
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("shows why one widget failed while the rest of the page loads", async () => {
    stubApi({
      ...signedInMember,
      "GET home": ok([overview]),
      "GET blueprints/service": ok(service),
      "GET dashboards/overview/data": ok({
        widgets: {
          critical: { data: linked(3, critical) },
          byOwner: { error: 'service has no property "team"' },
        },
      }),
    });
    renderRoute("/");
    expect(await screen.findByText('service has no property "team"')).toBeInTheDocument();
    await stat(/^Critical Services$/, "3");
  });

  it("says once that a dashboard couldn't load, and loads it again on Try again", async () => {
    const user = userEvent.setup();
    let reachable = false;
    stubApi({
      ...signedInMember,
      "GET home": ok([overview]),
      "GET blueprints/service": ok(service),
      "GET dashboards/overview/data": () => {
        if (!reachable) throw new TypeError("Failed to fetch");
        return Response.json({ data: { widgets: { critical: { data: linked(3, critical) } } } });
      },
    });
    renderRoute("/");

    expect(
      await screen.findByText("Can't connect", undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Can't connect")).toHaveLength(1);
    expect(screen.queryByText(/localhost/)).not.toBeInTheDocument();

    reachable = true;
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await stat(/^Critical Services$/, "3");
  });

  it("shows jq columns and opens rows where their row link goes", async () => {
    stubApi({
      ...signedInMember,
      "GET home": ok([
        {
          ...overview,
          widgets: [
            {
              identifier: "list",
              type: "table",
              title: "Services",
              query: services,
              width: 12,
              columns: [
                { title: "Failing Checks", jq: ".readiness.failing | length" },
                { path: "tier" },
                { title: "Since Deploy", jq: ".x", unit: "h" },
              ],
              rowLink: { path: "/integrations/{{ .identifier }}" },
              link: { query: services },
            },
          ],
        },
      ]),
      "GET dashboards/overview/data": loaded({
        list: {
          blueprint: "service",
          total: 40,
          columns: [
            { title: "Failing Checks", jq: ".readiness.failing | length" },
            { title: "Tier", path: "tier", property: tierProperty },
            { title: "Since Deploy", jq: ".x", unit: "h" },
          ],
          rows: [
            {
              identifier: "ledger",
              title: "Ledger",
              values: [2, "critical", 5],
              link: { href: "/integrations/ledger" },
            },
            {
              identifier: "search",
              title: "Search",
              values: [0, "standard", null],
              link: { href: "//elsewhere.example" },
            },
          ],
          link: { blueprint: "service" },
        },
      }),
    });
    renderRoute("/");

    expect(await screen.findByRole("columnheader", { name: "Failing Checks" })).toBeInTheDocument();
    const ledgerRow = screen.getByRole("link", { name: "Ledger" }).closest("tr") as HTMLElement;
    expect(within(ledgerRow).getByText("2")).toBeInTheDocument();
    expect(within(ledgerRow).getByText("5 h")).toBeInTheDocument();
    expect(within(ledgerRow).getByText("Critical")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Ledger" })).toHaveAttribute(
      "href",
      "/integrations/ledger",
    );
    // A long title is cut, whole on hover, so the columns stay in view.
    expect(within(ledgerRow).getByText("Ledger")).toHaveClass("truncate");
    expect(within(ledgerRow).getByText("Ledger")).toHaveAttribute("title", "Ledger");
    // A link that would leave the portal opens the entity instead.
    expect(screen.getByRole("link", { name: "Search" })).toHaveAttribute(
      "href",
      "/catalog/service/search",
    );
    // The link to the rest reads the same however many there are.
    expect(screen.getByRole("link", { name: "View all" })).toHaveAttribute(
      "href",
      "/catalog/service?view=entities",
    );
  });

  it("links nothing a widget doesn't say to", async () => {
    stubApi({
      ...signedInMember,
      "GET home": ok([
        {
          ...overview,
          widgets: [{ ...overview.widgets[0], link: undefined }, overview.widgets[1]],
        },
      ]),
      "GET blueprints/service": ok(service),
      "GET dashboards/overview/data": loaded({
        critical: { value: 3 },
        byOwner: { groups: [{ value: "payments", title: "Payments", count: 2 }], none: 0 },
      }),
    });
    renderRoute("/");

    expect(await screen.findByText("Payments")).toBeInTheDocument();
    expect(await screen.findByText("3")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Critical Services/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Payments" })).not.toBeInTheDocument();
  });

  it("shows a stat in the unit the server settled on", async () => {
    stubApi({
      ...signedInMember,
      "GET home": ok([{ ...overview, widgets: [overview.widgets[0]] }]),
      "GET dashboards/overview/data": loaded({
        critical: { value: 25, unit: "%", count: 1, total: 4 },
      }),
    });
    renderRoute("/");
    expect(await screen.findByText("25%")).toBeInTheDocument();
    // A share says what it's a share of.
    expect(screen.getByText("1 of 4")).toBeInTheDocument();
  });

  it("shows a split as shares of one bar, in the order the server gives, each opening its group", async () => {
    stubApi({
      ...signedInMember,
      "GET home": ok([
        {
          ...overview,
          widgets: [
            {
              identifier: "status",
              type: "split",
              title: "Status",
              width: 6,
              query: { blueprint: "service" },
              groupBy: "status",
              link: { query: { blueprint: "service" }, group: true },
            },
          ],
        },
      ]),
      "GET blueprints/service": ok(service),
      "GET dashboards/overview/data": loaded({
        status: {
          groups: [
            { value: "success", count: 3, color: "emerald", link: { blueprint: "service" } },
            { value: "failed", count: 1, color: "red", link: { blueprint: "service" } },
          ],
          none: 0,
        },
      }),
    });
    renderRoute("/");

    const bar = await screen.findByRole("img", { name: "Shares" });
    expect(bar.children).toHaveLength(2);
    const legend = within(bar.parentElement as HTMLElement).getAllByRole("listitem");
    expect(legend.map((item) => item.textContent)).toEqual(["success375%", "failed125%"]);
    expect(screen.getByRole("link", { name: "failed" })).toBeInTheDocument();
  });

  it("shows a stat compactly with its icon, and a scorecard as numbers", async () => {
    stubApi({
      ...signedInMember,
      "GET home": ok([
        {
          ...overview,
          inputs: [
            { identifier: "team", title: "Team", type: "relation", relation: "team" },
            { identifier: "tier", title: "Tier", type: "enum", values: ["critical"] },
          ],
          defaults: { tier: ["critical"] },
          widgets: [
            {
              ...overview.widgets[0],
              icon: "rocket",
              color: "red",
              description: "Services marked critical, of all services.",
            },
            {
              identifier: "readiness",
              type: "scorecard",
              title: "Readiness",
              width: 6,
              scorecard: "readiness",
            },
          ],
        },
      ]),
      "GET blueprints/service": ok(service),
      "GET dashboards/overview/data": loaded({
        critical: linked(3, critical),
        readiness: {
          tiers: [
            { name: "", count: 1 },
            { name: "gold", count: 3 },
          ],
          checks: { passed: 17, failed: 3 },
          scorecard: {
            identifier: "readiness",
            blueprint: "service",
            tiers: [{ name: "gold", title: "Gold", criteria: [] }],
          },
        },
      }),
    });
    renderRoute("/");

    const card = (await stat(/^Critical Services$/, "3")).closest(
      "[data-slot=card]",
    ) as HTMLElement;
    expect(card.querySelector(".bg-red-500\\/15 svg")).not.toBeNull();
    // Its description shows on hovering its title, above the card's link.
    expect(card).not.toHaveTextContent("Services marked critical");
    await userEvent.setup().hover(within(card).getByText("Critical Services"));
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Services marked critical, of all services.",
    );
    // Readiness leads with its checks passing, not the share at the top tier.
    expect(await screen.findByText("85%")).toBeInTheDocument();
    expect(screen.getByText("checks passing")).toBeInTheDocument();
    expect(screen.queryByText("75%")).not.toBeInTheDocument();
    expect(screen.queryByText(/reached|scored/)).not.toBeInTheDocument();
    // Home is titled with the dashboard it shows.
    expect(screen.getByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
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
    expect(await screen.findByRole("heading", { level: 1, name: "Welcome" })).toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "Welcome" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await user.click(screen.getByRole("link", { name: "Overview" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Welcome" })).not.toHaveAttribute("aria-current");
  });

  it("lists Home's dashboards under Home in the sidebar, a group folding open", async () => {
    const user = userEvent.setup();
    const devops = { title: "DevOps", icon: "infinity" };
    stubApi({
      ...signedInMember,
      "GET home": ok([
        { ...overview, widgets: [] },
        { ...welcome, group: devops },
        { ...welcome, identifier: "security", title: "Security", order: 3, group: devops },
      ]),
    });
    renderRoute("/");

    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    const fold = await screen.findByRole("button", { name: "DevOps" });
    expect(fold).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("link", { name: "Security" })).not.toBeInTheDocument();

    await user.click(fold);
    await user.click(await screen.findByRole("link", { name: "Security" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Security" })).toBeInTheDocument();
    // The trail leads back through the group, which opens its first dashboard.
    const trail = within(screen.getByRole("navigation", { name: "breadcrumb" }));
    expect(trail.getByRole("link", { name: "Home" })).toHaveAttribute("href", "/");
    expect(trail.getByRole("link", { name: "DevOps" })).toHaveAttribute("href", "/?d=welcome");
    const sidebarHome = () =>
      screen
        .getAllByRole("link", { name: "Home" })
        .find((link) => link.dataset.sidebar === "menu-button");
    expect(screen.getByRole("link", { name: "Security" })).toHaveAttribute("aria-current", "page");
    // Home itself isn't marked while one of its dashboards is.
    expect(sidebarHome()).not.toHaveAttribute("data-active", "true");

    // Folding Home away marks Home instead.
    await user.click(screen.getByRole("button", { name: "Dashboards" }));
    expect(screen.queryByRole("link", { name: "Security" })).not.toBeInTheDocument();
    expect(sidebarHome()).toHaveAttribute("data-active", "true");
  });

  it("shows Home's dashboards one at a time, each widget linking into the catalog", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInMember,
      "GET home": ok([overview, welcome]),
      "GET blueprints/service": ok(service),
      "GET dashboards/overview/data": loaded({
        critical: linked(3, critical),
        byOwner: {
          groups: [
            {
              value: "payments",
              title: "Payments",
              count: 2,
              link: {
                blueprint: "service",
                filter: { property: "owner", op: "eq", value: "payments" },
              },
            },
          ],
          none: 1,
          noneLink: { blueprint: "service", filter: { not: { property: "owner", op: "exists" } } },
        },
      }),
    });
    renderRoute("/");

    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    const count = await stat(/Services$/, "3");
    expect(count.getAttribute("href")).toMatch(/^\/catalog\/service\?view=entities&filter=/);
    expect(decodeURIComponent(count.getAttribute("href") ?? "")).toContain(
      '"property":"tier","op":"eq","value":"critical"',
    );

    const payments = await screen.findByRole("link", { name: "Payments" });
    expect(decodeURIComponent(payments.getAttribute("href") ?? "")).toContain(
      '"property":"owner","op":"eq","value":"payments"',
    );
    const none = screen.getByRole("link", { name: "None" });
    expect(decodeURIComponent(none.getAttribute("href") ?? "")).toContain(
      'filter=[{"not":{"property":"owner","op":"exists"}}]',
    );

    await user.click(screen.getByRole("link", { name: "Welcome" }));
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
                query: {
                  blueprint: "service",
                  filter: { property: "critical", op: "eq", value: true },
                },
                columns: [{ path: "tier" }, { path: "coverage" }],
              },
            ],
          },
        ],
      },
    });
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(laidOut),
      "GET catalog/service": ok({ items: [ledger], total: 1 }),
      "GET catalog/service/facets": ok([]),
      "GET catalog/service/pages/catalog/overview": loaded({
        critical: {
          blueprint: "service",
          total: 1,
          columns: [
            { title: "Tier", path: "tier", property: tierProperty },
            {
              title: "Coverage",
              path: "coverage",
              property: (service.published as NonNullable<Blueprint["published"]>).spec
                .properties[2],
            },
          ],
          rows: [{ identifier: "ledger", title: "Ledger", values: ["critical", 72] }],
        },
      }),
    });
    renderRoute("/catalog/service");

    expect(
      await screen.findByRole("tab", { name: /Overview/, selected: true }),
    ).toBeInTheDocument();
    const row = (await screen.findByRole("link", { name: "Ledger" })).closest("tr") as HTMLElement;
    expect(within(row).getByText("72%")).toBeInTheDocument();
    expect(within(row).getByText("Critical")).toHaveClass("bg-red-500/15");

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
      "GET catalog/service/entities/ledger": ok(ledger),
      "GET catalog/service/scorecards": ok([]),
      "GET catalog/service/pages/entity/overview": loaded({
        about: { text: "Owned by **payments**. Hi Ada." },
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
                value: {
                  query: {
                    blueprint: "deployment",
                    filter: { property: "service", op: "eq", value: "$entity" },
                  },
                },
                link: { query: { name: "deployments", blueprint: "deployment" } },
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
      "GET catalog/service/entities/ledger": ok(ledger),
      "GET catalog/service/scorecards": ok([]),
      "GET catalog/service/pages/entity/deployments": loaded({
        total: linked(12, { property: "service", op: "eq", value: "ledger" }, "deployment"),
      }),
    });
    renderRoute("/catalog/service/ledger");

    // The overview leaves out hidden properties.
    expect(await screen.findByText("Coverage")).toBeInTheDocument();
    expect(screen.queryByText("Critical", { selector: "dt" })).not.toBeInTheDocument();

    const tabs = screen.getAllByRole("tab").map((tab) => tab.textContent);
    expect(tabs.slice(0, 2)).toEqual(["Overview", "Deployments"]);
    await user.click(screen.getByRole("tab", { name: "Deployments" }));

    const total = await stat(/Deployments$/, "12");
    expect(decodeURIComponent(total.getAttribute("href") ?? "")).toContain(
      '"property":"service","op":"eq","value":"ledger"',
    );
    const url = new URL(requests["GET catalog/service/pages/entity/deployments"].url);
    expect(url.searchParams.get("entity")).toBe("ledger");
  });

  it("leaves out the tabs an entity doesn't meet the when of, even when the URL asks", async () => {
    const tab = (identifier: string, title: string) => ({
      identifier,
      title,
      when: { property: "tier", op: "eq", value: "critical" },
      widgets: [
        { identifier: "note", type: "markdown" as const, title: "Note", width: 6, text: "Hello" },
      ],
    });
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(
        withTabs({ entity: { tabs: [tab("incidents", "Incidents"), tab("oncall", "On-call")] } }),
      ),
      "GET catalog/service/entities/ledger": ok({ ...ledger, hiddenTabs: ["incidents"] }),
      "GET catalog/service/scorecards": ok([]),
    });
    renderRoute("/catalog/service/ledger?tab=incidents");

    expect(await screen.findByRole("tab", { name: "On-call" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Incidents" })).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");
  });

  it("moves a dashboard's inputs into the page bar with its tabs once scrolled", async () => {
    let scrolledPast: (past: boolean) => void = () => {};
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(callback: IntersectionObserverCallback) {
          scrolledPast = (past) =>
            callback(
              [
                {
                  isIntersecting: !past,
                  boundingClientRect: { top: past ? -200 : 80 },
                } as IntersectionObserverEntry,
              ],
              this as unknown as IntersectionObserver,
            );
        }
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
    const tiers: HomeDashboard = {
      identifier: "tiers",
      title: "Tiers",
      order: 1,
      defaults: { tier: ["critical"] },
      inputs: [
        {
          identifier: "tier",
          title: "Tier",
          type: "enum",
          values: ["critical", "standard"],
          multiple: true,
          default: ["critical"],
        },
      ],
      widgets: [],
    };
    stubApi({ ...signedInMember, "GET home": ok([tiers]) });
    renderRoute("/");

    // Before scrolling they're a row of the page, not a strip of their own.
    const row = await screen.findByRole("group", { name: "Inputs" });
    expect(row).not.toHaveClass("sticky");
    const bar = document.querySelector<HTMLElement>('[data-slot="page-bar"]') as HTMLElement;
    expect(bar).not.toBeVisible();
    expect(within(bar).queryByRole("group", { name: "Inputs" })).not.toBeInTheDocument();

    act(() => scrolledPast(true));
    expect(bar).toBeVisible();
    // The bar shows every control where there's room, and else one button
    // counting those set; the page's own row steps aside.
    expect(within(bar).getByRole("group", { name: "Inputs" })).toBeInTheDocument();
    expect(within(bar).getByRole("button", { name: /^Inputs/ })).toHaveTextContent("1");
    expect(row.parentElement).toHaveAttribute("inert");
  });

  it("sets a dashboard's inputs from its bar, keeping them in the URL", async () => {
    const user = userEvent.setup();
    const teams: HomeDashboard = {
      identifier: "teams",
      title: "Teams",
      order: 1,
      defaults: { tier: ["critical"] },
      inputs: [
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
          value: { query: services },
          link: { query: services },
        },
      ],
    };
    const requests = stubApi({
      ...signedInMember,
      "GET home": ok([teams]),
      "GET blueprints/service": ok(service),
      "GET catalog/team": ok({
        items: [
          { ...ledger, blueprint: "team", identifier: "identity", title: "Identity" },
          { ...ledger, blueprint: "team", identifier: "payments", title: "Payments" },
        ],
        total: 2,
      }),
      "GET dashboards/teams/data": countLike,
    });
    const { router } = renderRoute("/");

    // The default applies until someone changes it; an unset input drops
    // its condition from the link too.
    const count = await stat(/Services$/, "3");
    expect(decodeURIComponent(count.getAttribute("href") ?? "")).toContain(
      '"property":"tier","op":"in","values":["critical"]',
    );
    expect(decodeURIComponent(count.getAttribute("href") ?? "")).not.toContain("owner");
    let url = new URL(requests["GET dashboards/teams/data"].url);
    expect(url.searchParams.getAll("input.tier")).toEqual(["critical"]);
    expect(url.searchParams.getAll("input.team")).toEqual([""]);
    expect(screen.queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();
    // Each input reads as its label and what it's set to.
    const bar = screen.getByRole("group", { name: "Inputs" });
    expect(within(bar).getByRole("button", { name: "Team: Any" })).toBeInTheDocument();
    expect(within(bar).getByRole("button", { name: "Tier: critical" })).toBeInTheDocument();

    // A single-value input stays open on a pick, and a new pick replaces it.
    await user.click(screen.getByRole("button", { name: /^Team/ }));
    await user.click(await screen.findByRole("option", { name: "Payments" }));
    expect(await within(bar).findByRole("button", { name: "Team: Payments" })).toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: "Identity" }));
    expect(await within(bar).findByRole("button", { name: "Team: Identity" })).toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: "Payments" }));
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("option", { name: "Payments" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^Tier/ }));
    await user.click(await screen.findByRole("option", { name: "standard" }));
    expect(
      await within(bar).findByRole("button", { name: "Tier: critical, standard" }),
    ).toBeInTheDocument();
    expect(within(bar).getByRole("button", { name: "Team: Payments" })).toBeInTheDocument();

    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({
        team: "payments",
        tier: ["critical", "standard"],
      });
      url = new URL(requests["GET dashboards/teams/data"].url);
      expect(url.searchParams.getAll("input.team")).toEqual(["payments"]);
      expect(url.searchParams.getAll("input.tier")).toEqual(["critical", "standard"]);
    });
    const narrowed = await stat(/Services$/, "3");
    expect(decodeURIComponent(narrowed.getAttribute("href") ?? "")).toContain(
      '"property":"owner","op":"in","values":["payments"]',
    );

    await user.click(screen.getByRole("button", { name: "Reset" }));
    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({});
      url = new URL(requests["GET dashboards/teams/data"].url);
      expect(url.searchParams.getAll("input.tier")).toEqual(["critical"]);
    });
  });

  // mine starts its team input at what its default query finds for the viewer.
  const mine: HomeDashboard = {
    identifier: "mine",
    title: "Mine",
    order: 1,
    inputs: [
      {
        identifier: "team",
        title: "Team",
        type: "relation",
        relation: "team",
        multiple: true,
        defaultQuery: {
          query: {
            blueprint: "team",
            filter: { property: "members", op: "contains", value: "$me" },
          },
        },
      },
    ],
    widgets: [
      {
        identifier: "count",
        type: "stat",
        title: "Services",
        width: 3,
        value: { query: services },
        link: { query: services },
      },
    ],
    defaults: {},
  };
  const mineStubs = {
    ...signedInMember,
    "GET blueprints/service": ok(service),
    "GET catalog/team": ok({
      items: [{ ...ledger, blueprint: "team", identifier: "payments", title: "Payments" }],
      total: 1,
    }),
    "GET dashboards/mine/data": countLike,
  };

  it("starts an input at what its default query found", async () => {
    const requests = stubApi({
      ...mineStubs,
      "GET home": ok([{ ...mine, defaults: { team: ["payments"] } }]),
    });
    renderRoute("/");

    // What was found is picked, and sent as if picked by hand.
    const count = await stat(/Services$/, "3");
    expect(decodeURIComponent(count.getAttribute("href") ?? "")).toContain(
      '"property":"owner","op":"in","values":["payments"]',
    );
    const url = new URL(requests["GET dashboards/mine/data"].url);
    expect(url.searchParams.getAll("input.team")).toEqual(["payments"]);
    expect(screen.getByRole("button", { name: /^Team/ })).toHaveTextContent("Payments");
    // The data names the team, so its options load only once it's opened.
    expect(requests["GET catalog/team"]).toBeUndefined();
    expect(screen.queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();
    expect(screen.queryByText(/Nothing matched you/)).not.toBeInTheDocument();
  });

  it("says why a default query failed and shows nothing, until cleared", async () => {
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

    // The dashboard still loads, with the input at nothing.
    expect(
      await screen.findByText(
        /For Team, the default can't be found: team has no property "people", so the widgets it narrows show nothing/,
      ),
    ).toBeInTheDocument();
    expect(await screen.findByText("3")).toBeInTheDocument();
    expect(screen.queryByText(/Nothing matched you/)).not.toBeInTheDocument();
    expect(new URL(requests["GET dashboards/mine/data"].url).searchParams.has("input.team")).toBe(
      false,
    );

    await user.click(screen.getByRole("button", { name: "Show everything" }));
    await vi.waitFor(() => {
      const url = new URL(requests["GET dashboards/mine/data"].url);
      expect(url.searchParams.getAll("input.team")).toEqual([""]);
    });
    expect(screen.queryByText(/the default can't be found/)).not.toBeInTheDocument();
  });

  it("previews a dashboard with its input bar, from the draft until it's published", async () => {
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
    const { router } = renderRoute("/dashboards/mine?tab=overview");

    // The overview has the details; Home's order and who sees it.
    const about = within(
      (await screen.findByText("About")).closest("[data-slot=card]") as HTMLElement,
    );
    expect(about.getByText("Audience").nextElementSibling).toHaveTextContent("Everyone");
    expect(screen.getByRole("cell", { name: /Services.*count/ })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: /Team.*team/ })).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Preview" }));
    await vi.waitFor(() => expect(router.state.location.search).toEqual({ tab: "preview" }));

    // The view and the widgets come from the draft, filled in for the builder.
    expect(await screen.findByText("3")).toBeInTheDocument();
    expect(screen.getByText("Services of Ada Lovelace")).toBeInTheDocument();
    expect(new URL(requests["GET dashboards/mine/view"].url).searchParams.get("draft")).toBe(
      "true",
    );
    expect(screen.getByRole("button", { name: /^Team/ })).toHaveTextContent("Payments");
    let url = new URL(requests["GET dashboards/mine/data"].url);
    expect(url.searchParams.get("draft")).toBe("true");
    expect(url.searchParams.getAll("input.team")).toEqual(["payments"]);

    // Picking stays with the preview, not in the URL.
    await user.click(screen.getByRole("button", { name: /^Team/ }));
    await user.click(await screen.findByRole("option", { name: "Payments" }));
    await vi.waitFor(() => {
      url = new URL(requests["GET dashboards/mine/data"].url);
      expect(url.searchParams.getAll("input.team")).toEqual([""]);
    });
    expect(window.location.search).toBe("");
    expect(screen.getByRole("button", { name: "Reset" })).toBeInTheDocument();
  });

  it("starts an input at nothing when its default query found none, until cleared", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...mineStubs,
      "GET home": ok([{ ...mine, defaults: { team: [] } }]),
    });
    const { router } = renderRoute("/");

    // Nothing is picked, the request leaves the input to the server, which
    // finds nothing again, and there's no link, since the catalog can't
    // show nothing.
    await screen.findByText(/Nothing matched you for Team/);
    expect(await screen.findByText("3")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Services$/ })).not.toBeInTheDocument();
    let url = new URL(requests["GET dashboards/mine/data"].url);
    expect(url.searchParams.has("input.team")).toBe(false);
    expect(screen.getByRole("button", { name: "Team: None" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();

    // Clearing is a change from nothing, so it goes in the URL as "".
    await user.click(screen.getByRole("button", { name: "Show everything" }));
    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({ team: "" });
      url = new URL(requests["GET dashboards/mine/data"].url);
      expect(url.searchParams.getAll("input.team")).toEqual([""]);
    });
    expect(screen.queryByText(/Nothing matched you/)).not.toBeInTheDocument();
    const all = await stat(/Services$/, "3");
    expect(decodeURIComponent(all.getAttribute("href") ?? "")).not.toContain("owner");

    await user.click(screen.getByRole("button", { name: "Reset" }));
    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({});
      url = new URL(requests["GET dashboards/mine/data"].url);
      expect(url.searchParams.has("input.team")).toBe(false);
    });
    await screen.findByText(/Nothing matched you for Team/);
  });
});
