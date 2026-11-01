import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint } from "@/features/blueprints";
import type { EntityDetail, EntityPage } from "@/features/catalog";
import { readUiState, writeUiState } from "@/hooks/use-ui-state";
import { acmeConfig, ada, fail, ok, stubApi } from "@/test/api";
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

const page: EntityPage = { entities: [ledger], total: 1 };

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
      "GET entities": ok({ entities: [scored], total: 1 }),
      "GET scorecards/readiness": ok(readiness),
      "GET entities/facets": ok([]),
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
      expect(new URL(requests["GET entities"].url).searchParams.get("sort")).toBe("readiness"),
    );
    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Readiness" }));
    await user.click(await screen.findByRole("option", { name: "Gold" }));
    await vi.waitFor(() =>
      expect(
        JSON.parse(new URL(requests["GET entities"].url).searchParams.get("filter") ?? "null"),
      ).toEqual({ and: [{ property: "readiness", op: "in", values: ["gold"] }] }),
    );
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "Columns" }));
    await user.click(await screen.findByRole("menuitemcheckbox", { name: "Readiness" }));
    expect(screen.queryByRole("columnheader", { name: "Readiness" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("menuitem", { name: "Reset to defaults" }));
    expect(await screen.findByRole("columnheader", { name: /Readiness/ })).toBeInTheDocument();
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
    // The sidebar links here, and the bar names the top-level page alone.
    const links = screen.getAllByRole("link", { name: "Catalog" });
    expect(links.some((link) => link.getAttribute("href") === "/catalog")).toBe(true);
    const bar = document.querySelector<HTMLElement>('[data-slot="page-bar"]');
    const crumbs = within(bar as HTMLElement).getByRole("navigation", { name: "breadcrumb" });
    expect(within(crumbs).getAllByRole("listitem")).toHaveLength(1);
    expect(within(crumbs).getByRole("link", { name: "Catalog" })).toHaveAttribute(
      "aria-current",
      "page",
    );
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
  });

  it("shows a failed page of entities in the table and tries again", async () => {
    const user = userEvent.setup();
    let calls = 0;
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities": () =>
        ++calls === 1 ? fail(400, "the filter names an unknown property")() : ok(page)(),
      "GET entities/facets": ok([]),
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
      "GET entities": ok(page),
      "GET entities/facets": ok([{ value: "payments", title: "Payments", count: 1 }]),
      "GET catalog": ok([{ identifier: "service", title: "Service", entities: 3 }]),
    });
    const { router } = renderRoute("/catalog/service");
    // Counts are the Entities tab's and the table's, not the description's.
    expect(await screen.findByText("A deployable unit.")).toBeInTheDocument();
    expect(screen.queryByText(/3 entities/)).not.toBeInTheDocument();

    const row = (await screen.findByRole("link", { name: /Ledger/ })).closest("tr") as HTMLElement;
    expect(within(row).getByText("critical")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: "Payments" })).toHaveAttribute(
      "href",
      "/catalog/team/payments",
    );
    expect(within(row).getByText("#payments")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Owner" }));
    await user.click(await screen.findByRole("button", { name: "Back to fields" }));
    await user.click(await screen.findByRole("option", { name: "Tier" }));
    await user.click(await screen.findByRole("option", { name: "critical" }));

    await vi.waitFor(() => {
      const url = new URL(requests["GET entities"].url);
      expect(JSON.parse(url.searchParams.get("filter") ?? "null")).toEqual({
        and: [{ property: "tier", op: "in", values: ["critical"] }],
      });
    });
    expect(router.state.location.search).toMatchObject({ f: { tier: ["critical"] } });
    await user.keyboard("{Escape}");
    expect(screen.getByRole("button", { name: "Tier: critical" })).toBeInTheDocument();
  });

  it("keeps filters as chips in the search box to edit, remove and clear", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities": ok(page),
      "GET entities/facets": ok([{ value: "payments", title: "Payments", count: 1 }]),
    });
    const search = (value: unknown) => encodeURIComponent(JSON.stringify(value));
    const { router } = renderRoute(
      `/catalog/service?f=${search({ tier: ["critical"], owner: ["payments"] })}&d=${search({
        updatedAt: { from: "2026-10-01", to: "2026-10-15" },
      })}`,
    );

    expect(await screen.findByRole("button", { name: "Tier: critical" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Owner: Payments" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Updated: Oct 1 - Oct 15" })).toBeInTheDocument();
    // Headers only sort; filters live in the box.
    expect(
      within(screen.getByRole("columnheader", { name: /Tier/ })).getAllByRole("button"),
    ).toHaveLength(1);

    await user.click(screen.getByRole("button", { name: "Tier: critical" }));
    await user.click(await screen.findByRole("option", { name: "standard" }));
    await vi.waitFor(() =>
      expect(router.state.location.search).toMatchObject({
        f: { tier: ["critical", "standard"], owner: ["payments"] },
      }),
    );
    expect(
      await screen.findByRole("button", { name: "Tier: critical, standard" }),
    ).toBeInTheDocument();
    await user.keyboard("{Escape}");

    await user.click(screen.getByRole("button", { name: "Remove Owner filter" }));
    await vi.waitFor(() =>
      expect(router.state.location.search.f).toEqual({ tier: ["critical", "standard"] }),
    );
    expect(screen.queryByRole("button", { name: /^Owner:/ })).not.toBeInTheDocument();

    const box = screen.getByRole("searchbox", { name: "Search service" });
    await user.click(box);
    await user.keyboard("{Backspace}");
    await vi.waitFor(() => expect(router.state.location.search.d).toBeUndefined());
    expect(router.state.location.search.f).toEqual({ tier: ["critical", "standard"] });
    await vi.waitFor(() => {
      const filter = JSON.parse(
        new URL(requests["GET entities"].url).searchParams.get("filter") ?? "null",
      );
      expect(filter).toEqual({
        and: [{ property: "tier", op: "in", values: ["critical", "standard"] }],
      });
    });

    await user.type(box, "ledger");
    await vi.waitFor(() => expect(router.state.location.search.q).toBe("ledger"));
    await user.click(screen.getByRole("button", { name: "Clear search and filters" }));
    await vi.waitFor(() => {
      expect(router.state.location.search).not.toHaveProperty("f");
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
      "GET entities": ok(page),
      "GET entities/facets": ok([]),
    });
    const search = (value: unknown) => encodeURIComponent(JSON.stringify(value));
    const slack = { property: "owner.slack", op: "exists" };
    const lead = { property: "owner.lead", op: "in", values: ["ada", "grace"] };
    const { router } = renderRoute(
      `/catalog/service?f=${search({ version: ["1.2"] })}&where=${search({ and: [slack, lead] })}`,
    );

    // A property without a filter of its own gets one while it's filtered.
    expect(await screen.findByRole("button", { name: "version: 1.2" })).toBeInTheDocument();
    // Each condition of where is a chip, named by the properties it follows.
    expect(await screen.findByText("Owner › Slack:")).toBeInTheDocument();
    expect(screen.getByText("any value")).toBeInTheDocument();
    expect(screen.getByText("Owner › Lead:")).toBeInTheDocument();
    expect(screen.getByText("ada, grace")).toBeInTheDocument();
    const filter = () =>
      JSON.parse(new URL(requests["GET entities"].url).searchParams.get("filter") ?? "null");
    await vi.waitFor(() =>
      expect(filter()).toEqual({
        and: [{ property: "version", op: "in", values: ["1.2"] }, { and: [slack, lead] }],
      }),
    );

    await user.click(screen.getByRole("button", { name: "Remove Owner › Slack filter" }));
    await vi.waitFor(() => expect(router.state.location.search.where).toEqual(lead));
    await vi.waitFor(() =>
      expect(filter()).toEqual({
        and: [{ property: "version", op: "in", values: ["1.2"] }, lead],
      }),
    );
  });

  it("filters entities by when they were updated", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities": ok(page),
      "GET entities/facets": ok([]),
    });
    const { router } = renderRoute("/catalog/service");

    await user.click(await screen.findByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Updated" }));
    await user.click(await screen.findByRole("button", { name: "Older than 90 days" }));

    await vi.waitFor(() => {
      const filter = JSON.parse(
        new URL(requests["GET entities"].url).searchParams.get("filter") ?? "null",
      );
      expect(filter.and).toEqual([
        { property: "updatedAt", op: "lt", value: expect.stringMatching(/^\d{4}-\d{2}-\d{2}T/) },
      ]);
    });
    expect(router.state.location.search.d).toEqual({
      updatedAt: { to: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/) },
    });
    await user.keyboard("{Escape}");
    expect(screen.getByRole("button", { name: "Updated: Older than 90 days" })).toBeInTheDocument();
  });

  it("filters entities to a range of days picked on the calendar", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities": ok(page),
      "GET entities/facets": ok([]),
    });
    const search = encodeURIComponent(JSON.stringify({ updatedAt: { from: "2026-10-01" } }));
    const { router } = renderRoute(`/catalog/service?d=${search}`);

    await user.click(await screen.findByRole("button", { name: "Updated: From Oct 1" }));
    const days = await screen.findAllByRole("button", { name: /October 15th, 2026/ });
    await user.click(days[0]);
    await vi.waitFor(() =>
      expect(router.state.location.search.d).toEqual({
        updatedAt: { from: "2026-10-01", to: "2026-10-15" },
      }),
    );
  });

  it("says an unpublished blueprint isn't published, under its header", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok({ ...service, published: null }),
      "GET entities": ok({ entities: [], total: 0 }),
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
      "GET entities/service/ledger": ok(ledger),
      "GET entity-scorecards": ok([]),
    });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("link", { name: "registry" })).toHaveAttribute(
      "href",
      "/integrations/registry",
    );
  });

  it("opens an entity on the tab last opened for its blueprint", async () => {
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities/service/ledger": ok(ledger),
      "GET entity-scorecards": ok([]),
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
      "GET entities/service/ledger": ok({
        ...ledger,
        properties: {
          ...ledger.properties,
          summary: "Moves money",
          url: "https://ledger.example.com/x",
        },
      }),
      "GET entity-scorecards": ok([]),
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

  it("lists the first few related entities of a group and shows the rest on request", async () => {
    const user = userEvent.setup();
    const teams = Array.from({ length: 8 }, (_, i) => ({
      identifier: `t${i}`,
      title: `platform/team-${i}`,
    }));
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities/service/ledger": ok({ ...ledger, related: { owner: teams } }),
      "GET entity-scorecards": ok([]),
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
      "GET entities/service/ledger": ok(ledger),
      "GET entity-scorecards": ok([]),
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
