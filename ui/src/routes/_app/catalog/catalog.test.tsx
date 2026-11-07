import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint } from "@/features/blueprints";
import type { EntityDetail, EntityPage, Filter } from "@/features/catalog";
import { readUiState, writeUiState } from "@/hooks/use-ui-state";
import { acmeConfig, ada, fail, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

// instant is the start of a day, as the date filter writes it.
const instant = (day: string) => new Date(`${day}T00:00:00`).toISOString();

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
      description: "A deployable unit.",
      properties: [
        { identifier: "tier", title: "Tier", type: "string", enum: ["critical", "standard"] },
        { identifier: "owner", title: "Owner", type: "relation", relation: "team" },
        { identifier: "ownerSlack", title: "Owner Slack", type: "", lookup: "owner.slack" },
        { identifier: "readiness", title: "Readiness", type: "scorecard", scorecard: "readiness" },
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
  properties: { tier: "critical", owner: "payments", ownerSlack: "#payments" },
  related: { owner: [{ identifier: "payments", title: "Payments" }] },
  integration: "registry",
  createdAt: "2026-10-24T10:00:00Z",
  updatedAt: "2026-10-24T10:00:00Z",
  incoming: [
    {
      blueprint: "deployment",
      blueprintTitle: "Deployment",
      property: "service",
      propertyTitle: "Service",
      entities: [{ identifier: "d1", title: "Deploy 1" }],
      total: 1,
    },
  ],
};

const page: EntityPage = { items: [ledger], total: 1 };

const readiness = {
  identifier: "readiness",
  title: "Readiness",
  draft: null,
  createdAt: "2026-10-24T10:00:00Z",
  published: {
    version: 1,
    source: "",
    publishedAt: "2026-10-24T10:00:00Z",
    publishedBy: "",
    spec: {
      identifier: "readiness",
      title: "Readiness",
      blueprint: "service",
      tiers: [{ name: "gold", title: "Gold", color: "amber", criteria: [] }],
    },
  },
};

describe("catalog", () => {
  it("shows scorecard tiers, filters them on the server and lets columns be hidden", async () => {
    const user = userEvent.setup();
    const scored = {
      ...ledger,
      properties: { ...ledger.properties, readiness: { tier: "gold", score: 100 } },
    };
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok({ items: [scored], total: 1 }),
      "GET scorecards/readiness": ok(readiness),
      "GET catalog/service/facets": ok([]),
    });
    renderRoute("/catalog/service");
    // A tier links to the result it comes from, as a relation to its entity.
    expect(await screen.findByRole("link", { name: "Gold" })).toHaveAttribute(
      "href",
      "/catalog/service/ledger?tab=readiness",
    );
    expect(screen.getByRole("columnheader", { name: "Readiness" })).toBeInTheDocument();
    await user.click(
      within(screen.getByRole("columnheader", { name: /Readiness/ })).getByRole("button"),
    );
    await vi.waitFor(() =>
      expect(new URL(requests["GET catalog/service"].url).searchParams.get("sort")).toBe(
        "readiness",
      ),
    );
    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Readiness" }));
    await user.click(await screen.findByRole("option", { name: "Gold" }));
    await vi.waitFor(() =>
      expect(
        JSON.parse(
          new URL(requests["GET catalog/service"].url).searchParams.get("filter") ?? "null",
        ),
      ).toEqual({ and: [{ property: "readiness", op: "eq", value: "gold" }] }),
    );
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "Columns" }));
    await user.click(await screen.findByRole("menuitemcheckbox", { name: "Readiness" }));
    expect(screen.queryByRole("columnheader", { name: "Readiness" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("menuitem", { name: "Reset to defaults" }));
    expect(await screen.findByRole("columnheader", { name: /Readiness/ })).toBeInTheDocument();
  });

  it("offers no columns to choose while a blueprint has no entities", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok({ items: [], total: 0 }),
      "GET scorecards/readiness": ok(readiness),
    });
    renderRoute("/catalog/service");
    expect(await screen.findByText("No service entities yet")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Columns" })).not.toBeInTheDocument();
  });

  it("lists blueprints with their entity counts", async () => {
    stubApi({
      ...signedInMember,
      "GET catalog": ok([
        { identifier: "service", title: "Service", description: "Units", entities: 1200 },
        { identifier: "team", title: "Team", entities: 4 },
      ]),
    });
    renderRoute("/catalog");

    const main = await screen.findByRole("main");
    const link = await within(main).findByRole("link", { name: "Service" });
    expect(link).toHaveAttribute("href", "/catalog/service");
    expect(
      within(link.closest("[data-slot=card]") as HTMLElement).getByText("1.2K"),
    ).toBeInTheDocument();
    // The sidebar links here. A top-level page has nothing above it, and the
    // header names it, so it has no trail.
    const links = screen.getAllByRole("link", { name: "Catalog" });
    expect(links.some((link) => link.getAttribute("href") === "/catalog")).toBe(true);
    expect(within(main).queryByRole("navigation", { name: "breadcrumb" })).not.toBeInTheDocument();
    const header = main.querySelector<HTMLElement>('[data-slot="page-header"]');
    expect(header?.querySelector('[data-slot="page-crumbs"]')).toBeEmptyDOMElement();
  });

  it("keeps the navigation when a blueprint doesn't exist", async () => {
    stubApi({ ...signedInMember, "GET blueprints/gone": fail(404, "blueprint not found") });
    renderRoute("/catalog/gone");

    expect(await screen.findByText("Not found")).toBeVisible();
    expect(screen.getByRole("link", { name: "Back to Catalog" })).toHaveAttribute(
      "href",
      "/catalog",
    );
    expect(screen.getByRole("link", { name: "Scorecards" })).toBeInTheDocument();
    // With no header to lead, the page still starts with the trigger that
    // opens the sidebar on phones.
    const trail = screen.getByRole("main").querySelector<HTMLElement>('[data-slot="page-crumbs"]')
      ?.parentElement as HTMLElement;
    expect(within(trail).getByRole("button", { name: "Toggle Sidebar" })).toBeInTheDocument();
  });

  it("shows a failed page of entities in the table and tries again", async () => {
    const user = userEvent.setup();
    let calls = 0;
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": () =>
        ++calls === 1 ? fail(400, "the filter names an unknown property")() : ok(page)(),
      "GET catalog/service/facets": ok([]),
    });
    renderRoute("/catalog/service");

    expect(await screen.findByText("the filter names an unknown property")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: /Ledger/ })).toBeVisible();
  });

  it("filters a blueprint's entities on the server", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([{ value: "payments", title: "Payments", count: 1 }]),
      "GET catalog": ok([{ identifier: "service", title: "Service", entities: 3 }]),
    });
    const { router } = renderRoute("/catalog/service");
    // Counts are the Entities tab's and the table's, not the description's.
    expect(await screen.findByText("A deployable unit.")).toBeInTheDocument();
    expect(screen.queryByText(/3 entities/)).not.toBeInTheDocument();

    const row = (await screen.findByRole("link", { name: /Ledger/ })).closest("tr") as HTMLElement;
    expect(within(row).getByText("Critical")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: "Payments" })).toHaveAttribute(
      "href",
      "/catalog/team/payments",
    );
    expect(within(row).getByText("#payments")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Owner" }));
    await user.click(await screen.findByRole("button", { name: "Back to fields" }));
    await user.click(await screen.findByRole("option", { name: "Tier" }));
    await user.click(await screen.findByRole("option", { name: "Critical" }));

    await vi.waitFor(() => {
      const url = new URL(requests["GET catalog/service"].url);
      expect(JSON.parse(url.searchParams.get("filter") ?? "null")).toEqual({
        and: [{ property: "tier", op: "eq", value: "critical" }],
      });
    });
    expect(router.state.location.search).toMatchObject({
      filter: [{ property: "tier", op: "eq", value: "critical" }],
    });
    expect(router.state.location.search).not.toHaveProperty("f");
    await user.keyboard("{Escape}");
    expect(screen.getByRole("button", { name: "Tier: Critical" })).toBeInTheDocument();
  });

  it("keeps filters as chips in the search box to edit, remove and clear", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([{ value: "payments", title: "Payments", count: 1 }]),
    });
    const search = (value: unknown) => encodeURIComponent(JSON.stringify(value));
    const { router } = renderRoute(
      `/catalog/service?filter=${search([
        { property: "tier", op: "in", values: ["critical"] },
        { property: "owner", op: "eq", value: "payments" },
        { property: "updatedAt", op: "gte", value: instant("2026-10-01") },
        { property: "updatedAt", op: "lt", value: instant("2026-10-16") },
      ])}`,
    );

    expect(await screen.findByRole("button", { name: "Tier: Critical" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Owner: Payments" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Updated: Oct 1 - Oct 15" })).toBeInTheDocument();
    // Headers only sort; filters live in the box.
    expect(
      within(screen.getByRole("columnheader", { name: /Tier/ })).getAllByRole("button"),
    ).toHaveLength(1);

    await user.click(screen.getByRole("button", { name: "Tier: Critical" }));
    await user.click(await screen.findByRole("option", { name: "Standard" }));
    await vi.waitFor(() =>
      expect(router.state.location.search.filter?.slice(0, 2)).toEqual([
        { property: "tier", op: "in", values: ["critical", "standard"] },
        { property: "owner", op: "eq", value: "payments" },
      ]),
    );
    expect(
      await screen.findByRole("button", { name: "Tier: Critical, Standard" }),
    ).toBeInTheDocument();
    await user.keyboard("{Escape}");

    await user.click(screen.getByRole("button", { name: "Remove Owner filter" }));
    await vi.waitFor(() =>
      expect(
        (router.state.location.search.filter as Filter[] | undefined)?.map(
          (c) => "property" in c && c.property,
        ),
      ).toEqual(["tier", "updatedAt", "updatedAt"]),
    );
    expect(screen.queryByRole("button", { name: /^Owner:/ })).not.toBeInTheDocument();

    const box = screen.getByRole("searchbox", { name: "Search service" });
    await user.click(box);
    await user.keyboard("{Backspace}");
    await vi.waitFor(() =>
      expect(router.state.location.search.filter).toEqual([
        { property: "tier", op: "in", values: ["critical", "standard"] },
      ]),
    );
    await vi.waitFor(() => {
      const filter = JSON.parse(
        new URL(requests["GET catalog/service"].url).searchParams.get("filter") ?? "null",
      );
      expect(filter).toEqual({
        and: [{ property: "tier", op: "in", values: ["critical", "standard"] }],
      });
    });

    await user.type(box, "ledger");
    await vi.waitFor(() => expect(router.state.location.search.q).toBe("ledger"));
    await user.click(screen.getByRole("button", { name: "Clear search and filters" }));
    await vi.waitFor(() => {
      expect(router.state.location.search.filter).toBeUndefined();
      expect(router.state.location.search.q).toBeUndefined();
    });
    expect(screen.queryByRole("button", { name: /^Tier:/ })).not.toBeInTheDocument();
    expect(box).toHaveValue("");
  });

  it("narrows by what a widget's link brings along, until it's removed", async () => {
    const user = userEvent.setup();
    const team: Blueprint = {
      ...service,
      identifier: "team",
      title: "Team",
      published: service.published && {
        ...service.published,
        spec: {
          identifier: "team",
          title: "Team",
          properties: [
            { identifier: "slack", title: "Slack", type: "string" },
            { identifier: "lead", title: "Lead", type: "string" },
          ],
        },
      },
    };
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET blueprints/team": ok(team),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([]),
    });
    const search = (value: unknown) => encodeURIComponent(JSON.stringify(value));
    const slack = { property: "owner.slack", op: "exists" };
    const lead = { property: "owner.lead", op: "in", values: ["ada", "grace"] };
    const version = { property: "version", op: "in", values: ["1.2"] };
    const { router } = renderRoute(`/catalog/service?filter=${search([version, slack, lead])}`);

    // A property without a filter of its own gets one while it's filtered.
    expect(await screen.findByRole("button", { name: "version: 1.2" })).toBeInTheDocument();
    // What the menu can't say is a chip too, named by the properties it follows.
    expect(await screen.findByText("Owner › Slack:")).toBeInTheDocument();
    expect(screen.getByText("Any Value")).toBeInTheDocument();
    expect(screen.getByText("Owner › Lead:")).toBeInTheDocument();
    expect(screen.getByText("ada, grace")).toBeInTheDocument();
    const filter = () =>
      JSON.parse(new URL(requests["GET catalog/service"].url).searchParams.get("filter") ?? "null");
    await vi.waitFor(() => expect(filter()).toEqual({ and: [version, slack, lead] }));

    await user.click(screen.getByRole("button", { name: "Remove Owner › Slack filter" }));
    await vi.waitFor(() => expect(router.state.location.search.filter).toEqual([version, lead]));
    await vi.waitFor(() => expect(filter()).toEqual({ and: [version, lead] }));
  });

  it("shows a link that repeats a condition once", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([]),
    });
    const tier = { property: "tier", op: "eq", value: "critical" };
    renderRoute(`/catalog/service?filter=${encodeURIComponent(JSON.stringify([tier, tier]))}`);

    expect(await screen.findByRole("button", { name: "Tier: Critical" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /^Remove .* filter$/ })).toHaveLength(1);
  });

  it("shows what the filter menu can't edit as chips, which only remove", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([]),
    });
    const either = {
      or: [
        { property: "tier", op: "eq", value: "critical" },
        { property: "tier", op: "eq", value: "standard" },
      ],
    };
    const tier = { property: "tier", op: "in", values: ["critical", "standard"] };
    const { router } = renderRoute(
      `/catalog/service?filter=${encodeURIComponent(JSON.stringify([either, tier, { property: "tier", op: "ne", value: "standard" }]))}`,
    );

    expect(await screen.findByText("Any Of:")).toBeInTheDocument();
    expect(await screen.findByText("Tier: Critical or Tier: Standard")).toBeInTheDocument();
    // The first condition on a property is the one that's editable.
    expect(screen.getByRole("button", { name: "Tier: Critical, Standard" })).toBeInTheDocument();
    expect(screen.getByText("Not Standard")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Tier: Not Standard" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Remove Any Of filter" }));
    await vi.waitFor(() =>
      expect(router.state.location.search.filter).toEqual([
        tier,
        { property: "tier", op: "ne", value: "standard" },
      ]),
    );
  });

  it("edits a negated chip with Is and Is Not", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([]),
    });
    const filter = [{ property: "tier", op: "ne", value: "critical" }];
    const { router } = renderRoute(
      `/catalog/service?filter=${encodeURIComponent(JSON.stringify(filter))}`,
    );

    await user.click(await screen.findByRole("button", { name: "Tier: Not Critical" }));
    expect(screen.getByRole("radio", { name: "Is Not" })).toBeChecked();
    await user.click(await screen.findByRole("option", { name: "Standard" }));
    await vi.waitFor(() =>
      expect(router.state.location.search.filter).toEqual([
        { not: { property: "tier", op: "in", values: ["critical", "standard"] } },
      ]),
    );
    expect(
      await screen.findByRole("button", { name: "Tier: Not Critical, Standard" }),
    ).toBeVisible();

    await user.click(screen.getByRole("radio", { name: "Is" }));
    await vi.waitFor(() =>
      expect(router.state.location.search.filter).toEqual([
        { property: "tier", op: "in", values: ["critical", "standard"] },
      ]),
    );
    await vi.waitFor(() =>
      expect(
        JSON.parse(
          new URL(requests["GET catalog/service"].url).searchParams.get("filter") ?? "null",
        ),
      ).toEqual({ and: [{ property: "tier", op: "in", values: ["critical", "standard"] }] }),
    );
  });

  it("says a link's negated conditions as chips", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([]),
    });
    const search = (value: unknown) => encodeURIComponent(JSON.stringify(value));
    const filter = [
      { property: "archived", op: "ne", value: true },
      { not: { property: "pipeline", op: "in", values: ["none", "manual"] } },
      { not: { property: "docs", op: "exists" } },
      { not: { property: "owner.slack", op: "contains", value: "dev" } },
    ];
    renderRoute(`/catalog/service?filter=${search(filter)}`);

    expect(await screen.findByText("archived:")).toBeInTheDocument();
    expect(screen.getByText("Not Yes")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "pipeline: Not none, manual" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "docs: No Value" })).toBeInTheDocument();
    expect(await screen.findByText("Lacks dev")).toBeInTheDocument();
  });

  it("filters entities by when they were updated", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([]),
    });
    const { router } = renderRoute("/catalog/service");

    await user.click(await screen.findByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Updated" }));
    await user.click(await screen.findByRole("button", { name: "Older than 90 days" }));

    await vi.waitFor(() => {
      const filter = JSON.parse(
        new URL(requests["GET catalog/service"].url).searchParams.get("filter") ?? "null",
      );
      expect(filter.and).toEqual([
        { property: "updatedAt", op: "lt", value: expect.stringMatching(/^\d{4}-\d{2}-\d{2}T/) },
      ]);
    });
    expect(router.state.location.search.filter).toEqual([
      { property: "updatedAt", op: "lt", value: expect.stringMatching(/^\d{4}-\d{2}-\d{2}T/) },
    ]);
    await user.keyboard("{Escape}");
    expect(screen.getByRole("button", { name: "Updated: Older than 90 days" })).toBeInTheDocument();
  });

  it("filters entities to a range of days picked on the calendar", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok(page),
      "GET catalog/service/facets": ok([]),
    });
    // A link may bound a day by its date alone.
    const search = encodeURIComponent(
      JSON.stringify([{ property: "updatedAt", op: "gte", value: "2026-10-01" }]),
    );
    const { router } = renderRoute(`/catalog/service?filter=${search}`);

    await user.click(await screen.findByRole("button", { name: "Updated: From Oct 1" }));
    const days = await screen.findAllByRole("button", { name: /October 15th, 2026/ });
    await user.click(days[0]);
    // An edit writes the picker's own instants.
    await vi.waitFor(() =>
      expect(router.state.location.search.filter).toEqual([
        { property: "updatedAt", op: "gte", value: instant("2026-10-01") },
        { property: "updatedAt", op: "lt", value: instant("2026-10-16") },
      ]),
    );
  });

  it("says an unpublished blueprint isn't published, under its header", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok({ ...service, published: null }),
      "GET catalog/service": ok({ items: [], total: 0 }),
    });
    renderRoute("/catalog/service");

    expect(await screen.findByText("Service isn't published yet")).toBeInTheDocument();
    const trail = within(screen.getByRole("navigation", { name: "breadcrumb" }));
    expect(trail.getByRole("link", { name: "Catalog" })).toHaveAttribute("href", "/catalog");
    // Members can't open the blueprint, so there's nothing to do but wait.
    expect(screen.queryByRole("link", { name: "View Blueprint" })).not.toBeInTheDocument();
  });

  it("links builders to the entity's integration", async () => {
    stubApi({
      "GET config": acmeConfig,
      "GET me": ok({ ...ada, role: "builder" }),
      "GET blueprints/service": ok(service),
      "GET catalog/service/entities/ledger": ok(ledger),
      "GET catalog/service/scorecards": ok([]),
    });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("link", { name: "registry" })).toHaveAttribute(
      "href",
      "/integrations/registry",
    );
  });

  it("leads every crumb of an entity's trail with its icon", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok({ ...service, icon: "server", color: "blue" }),
      "GET catalog/service/entities/ledger": ok(ledger),
      "GET catalog/service/scorecards": ok([]),
    });
    renderRoute("/catalog/service/ledger");

    const heading = await screen.findByRole("heading", { level: 1, name: "Ledger" });
    const header = heading.closest<HTMLElement>('[data-slot="page-header"]') as HTMLElement;
    const crumb = (name: string) =>
      within(header).getByRole("link", { name }).parentElement?.querySelector("span.size-6");
    expect(crumb("Catalog")).toHaveClass("bg-gray-500/15");
    // The blueprint's crumb is tinted as the blueprint is.
    await waitFor(() => expect(crumb("Service")?.querySelector(".lucide-server")).not.toBeNull());
    expect(crumb("Service")).toHaveClass("bg-blue-500/15");
  });

  it("opens an entity on the tab last opened for its blueprint", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service/entities/ledger": ok(ledger),
      "GET catalog/service/scorecards": ok([]),
    });
    writeUiState("entityTabs", { service: "yaml", team: "relations" });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("tab", { name: "YAML", selected: true })).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("tab", { name: /Relations/ }));
    expect(readUiState("entityTabs", {})).toEqual({ service: "relations", team: "relations" });
  });

  it("lays out an entity page with the blueprint's header and sections", async () => {
    const spec = service.published?.spec;
    if (!spec) throw new Error("service is published");
    const laidOut: Blueprint = {
      ...service,
      published: {
        ...service.published,
        spec: {
          ...spec,
          properties: [
            ...spec.properties,
            { identifier: "summary", title: "Summary", type: "string" },
            { identifier: "url", title: "URL", type: "string", format: "uri" },
            { identifier: "pager", title: "Pager", type: "string", display: { empty: "No pager" } },
          ],
          entity: {
            header: { subtitle: "summary", badges: ["tier"], link: "url" },
            sections: [{ title: "Ownership", properties: ["owner", "pager"] }],
            tabs: [],
          },
        },
      } as Blueprint["published"],
    };
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(laidOut),
      "GET catalog/service/entities/ledger": ok({
        ...ledger,
        properties: {
          ...ledger.properties,
          summary: "Moves money",
          url: "https://ledger.example.com/x",
        },
      }),
      "GET catalog/service/scorecards": ok([]),
    });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByText("Moves money")).toBeVisible();
    expect(screen.getByRole("link", { name: "Open" })).toHaveAttribute(
      "href",
      "https://ledger.example.com/x",
    );
    const ownership = screen.getByText("Ownership").closest("[data-slot=card]") as HTMLElement;
    expect(within(ownership).getByRole("link", { name: "Payments" })).toBeInTheDocument();
    expect(within(ownership).getByText("No pager")).toBeInTheDocument();
    const other = screen.getByText("Other").closest("[data-slot=card]") as HTMLElement;
    expect(within(other).getByText("Owner Slack")).toBeInTheDocument();
    expect(within(other).queryByText("Tier")).not.toBeInTheDocument();
    expect(within(other).queryByText("Summary")).not.toBeInTheDocument();
  });

  it("leaves no subtitle placeholder when the entity has none", async () => {
    const spec = service.published?.spec;
    if (!spec) throw new Error("service is published");
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok({
        ...service,
        published: {
          ...service.published,
          spec: {
            ...spec,
            properties: [
              ...spec.properties,
              { identifier: "summary", title: "Summary", type: "string" },
            ],
            entity: { header: { subtitle: "summary" }, tabs: [] },
          },
        } as Blueprint["published"],
      }),
      "GET catalog/service/entities/ledger": ok({
        ...ledger,
        properties: { ...ledger.properties, summary: "" },
      }),
      "GET catalog/service/scorecards": ok([]),
    });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("heading", { name: "Ledger" })).toBeInTheDocument();
    expect(screen.queryByText("None")).not.toBeInTheDocument();
  });

  it("lists the first few related entities of a group and shows the rest on request", async () => {
    const user = userEvent.setup();
    const teams = Array.from({ length: 8 }, (_, i) => ({
      identifier: `t${i}`,
      title: `platform/team-${i}`,
    }));
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service/entities/ledger": ok({ ...ledger, related: { owner: teams } }),
      "GET catalog/service/scorecards": ok([]),
      "GET catalog": ok([{ identifier: "team", title: "Team", entities: 8 }]),
    });
    renderRoute("/catalog/service/ledger?tab=relations");

    // Relations group by the blueprint they reach, a row for each relation.
    const tab = await screen.findByRole("tabpanel");
    const owners = within(tab).getByText("Owner").closest("dt")?.nextElementSibling as HTMLElement;
    expect(await within(tab).findByText("Team")).toBeInTheDocument();
    expect(within(owners).getAllByRole("link")).toHaveLength(6);
    await user.click(within(owners).getByRole("button", { name: "Show 2 more" }));
    expect(within(owners).getAllByRole("link")).toHaveLength(8);
    expect(within(owners).getAllByText("platform/")[0]).toHaveClass("text-muted-foreground");
  });

  it("shows an entity with its relations both ways", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET catalog/service/entities/ledger": ok(ledger),
      "GET catalog/service/scorecards": ok([]),
    });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("heading", { name: "Ledger" })).toBeInTheDocument();
    await vi.waitFor(() => expect(document.title).toBe("Ledger | Acme"));
    // Members can't open integration pages, so they see its name unlinked.
    expect(screen.getByText("registry")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "registry" })).not.toBeInTheDocument();
    expect(screen.getByText("#payments")).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /Relations/ }));
    expect(await screen.findByRole("link", { name: "Deploy 1" })).toHaveAttribute(
      "href",
      "/catalog/deployment/d1",
    );

    await user.click(screen.getByRole("tab", { name: "YAML" }));
    expect(await screen.findByText(/identifier: ledger/)).toBeInTheDocument();
  });
});
