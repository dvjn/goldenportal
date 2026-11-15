import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint } from "@/features/blueprints";
import type { EntityResult, Scorecard, ScorecardSpec, Summary } from "@/features/scorecards";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const spec: ScorecardSpec = {
  identifier: "readiness",
  title: "Readiness",
  blueprint: "service",
  tiers: [
    {
      name: "bronze",
      title: "Bronze",
      criteria: [
        {
          identifier: "hasOwner",
          title: "Has an owner",
          check: { property: "owner", op: "exists" },
        },
      ],
    },
    {
      name: "silver",
      title: "Silver",
      criteria: [
        {
          identifier: "onCall",
          title: "Owner is on call",
          when: { property: "tier", op: "eq", value: "critical" },
          check: { property: "ownerOncall", op: "exists" },
          fix: "Add an on-call rotation.",
        },
      ],
    },
  ],
};

const readiness: Scorecard = {
  identifier: "readiness",
  title: "Readiness",
  published: { version: 1, source: "", spec, publishedAt: "2026-10-24T10:00:00Z", publishedBy: "" },
  draft: null,
  createdAt: "2026-10-24T10:00:00Z",
};

const listed = (identifier: string, title: string, blueprint: string, publishedVersion = 1) => ({
  identifier,
  title,
  publishedVersion,
  hasDraft: publishedVersion === 0,
  stats: { tiers: 2, criteria: 2 },
  facts: { blueprint },
  updatedAt: "2026-10-24T10:00:00Z",
});

// Only published scorecards of the blueprint get a catalog tab.
const scorecardList = ok([
  listed("readiness", "Readiness", "service"),
  listed("security", "Security", "service", 0),
  listed("team-health", "Team Health", "team"),
]);

const summary: Summary = {
  spec,
  entities: 3,
  evaluatedAt: "2026-10-24T10:00:00Z",
  evaluating: false,
  tiers: [
    { name: "", count: 1 },
    { name: "bronze", count: 2 },
    { name: "silver", count: 0 },
  ],
  criteria: [
    { identifier: "hasOwner", passed: 2, failed: 1, skipped: 0 },
    { identifier: "onCall", passed: 0, failed: 2, skipped: 1 },
  ],
};

const ledger: EntityResult = {
  blueprint: "service",
  identifier: "ledger",
  title: "Ledger",
  changedAt: "2026-10-20T10:00:00Z",
  evaluatedAt: "2026-10-24T10:00:00Z",
  tier: "bronze",
  score: 50,
  passed: 1,
  failed: 1,
  skipped: 0,
  criteria: { hasOwner: { status: "passed" }, onCall: { status: "failed" } },
};

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
        { identifier: "readiness", title: "Readiness", type: "scorecard", scorecard: "readiness" },
      ],
    },
    publishedAt: "2026-10-24T10:00:00Z",
    publishedBy: "",
  },
  draft: null,
  createdAt: "2026-10-24T10:00:00Z",
};

const member = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) };
const builder = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "builder" }) };

const catalogRoutes = {
  ...member,
  "GET blueprints/service": ok(service),
  "GET catalog/service": ok({ items: [], total: 4 }),
  "GET scorecards": scorecardList,
  "GET scorecards/readiness": ok(readiness),
  "GET scorecards/readiness/summary": ok(summary),
  "GET scorecards/readiness/results": ok({ items: [ledger], total: 1 }),
};

const resultsParam = (requests: Record<string, Request>, name: string) =>
  new URL(requests["GET scorecards/readiness/results"].url).searchParams.get(name);
const summaryParam = (requests: Record<string, Request>, name: string) =>
  new URL(requests["GET scorecards/readiness/summary"].url).searchParams.get(name);

describe("a scorecard in the catalog", () => {
  it("is a tab of its blueprint's page, with its results for members", async () => {
    const user = userEvent.setup();
    stubApi(catalogRoutes);
    const { router } = renderRoute("/catalog/service");

    const tabs = (await screen.findAllByRole("tab")).map((tab) => tab.textContent);
    expect(tabs).toEqual(["Entities4", "Readiness"]);
    expect(screen.getByRole("tab", { name: /Entities/ })).toHaveAttribute("aria-selected", "true");

    await user.click(screen.getByRole("tab", { name: "Readiness" }));
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ view: "scorecard-readiness" }),
    );
    // 2 of the 5 checks that ran pass, and 2 of the 3 entities checked have an owner.
    expect(await screen.findByText("40%")).toBeInTheDocument();
    expect(screen.getByText("66%")).toBeInTheDocument();
    const row = await screen.findByRole("row", { name: /Ledger/ });
    expect(within(row).getByText("50%")).toBeInTheDocument();
    expect(within(row).getByText("Owner is on call")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: "Ledger" })).toHaveAttribute(
      "href",
      "/catalog/service/ledger?tab=readiness",
    );
    // Building the scorecard happens elsewhere.
    expect(screen.queryByRole("button", { name: "Evaluate Now" })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Definition" })).not.toBeInTheDocument();
  });

  it("says while its results catch up with a publish, then refreshes them", async () => {
    const user = userEvent.setup();
    let summaries = 0;
    let results = 0;
    stubApi({
      ...catalogRoutes,
      "GET scorecards/readiness/summary": () =>
        Response.json({ data: { ...summary, evaluating: summaries++ === 0 } }),
      "GET scorecards/readiness/results": () => {
        results++;
        return Response.json({ data: { items: [ledger], total: 1 } });
      },
    });
    renderRoute("/catalog/service");

    await user.click(await screen.findByRole("tab", { name: "Readiness" }));
    expect(await screen.findByText("Evaluating")).toBeInTheDocument();
    const before = results;
    await waitFor(() => expect(screen.queryByText("Evaluating")).not.toBeInTheDocument(), {
      timeout: 5000,
    });
    await waitFor(() => expect(results).toBeGreaterThan(before));
  });

  it("sorts its entities on the server, leaving the blueprint's entities unsorted", async () => {
    const user = userEvent.setup();
    const requests = stubApi(catalogRoutes);
    const { router } = renderRoute("/catalog/service?view=scorecard-readiness&sort=-score");

    await screen.findByRole("row", { name: /Ledger/ });
    expect(resultsParam(requests, "sort")).toBe("-score");
    expect(new URL(requests["GET catalog/service"].url).searchParams.get("sort")).toBeNull();
    expect(screen.getByRole("button", { name: "Score, sorted descending" })).toBeInTheDocument();

    // Tiers go up the scorecard's tiers, worst first, then best first.
    const sorted = async (sort: string) =>
      vi.waitFor(() => {
        expect(router.state.location.search).toMatchObject({ view: "scorecard-readiness", sort });
        expect(resultsParam(requests, "sort")).toBe(sort);
      });
    await user.click(screen.getByRole("button", { name: "Tier" }));
    await sorted("tier");
    await user.click(screen.getByRole("button", { name: "Tier, sorted ascending" }));
    await sorted("-tier");
    await user.click(screen.getByRole("button", { name: "Failed" }));
    await sorted("-failed");
  });

  it("counts each criterion's results, each filtering to its entities in the same tab", async () => {
    const user = userEvent.setup();
    const requests = stubApi(catalogRoutes);
    const { router } = renderRoute("/catalog/service?view=scorecard-readiness");

    await user.click(await screen.findByRole("button", { name: "Owner is on call: 2 failed" }));
    const failing = { property: "readiness.checks.onCall", op: "eq", value: "failed" };
    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({
        view: "scorecard-readiness",
        filter: [failing],
      });
      expect(resultsParam(requests, "filter")).toBe(JSON.stringify({ and: [failing] }));
      expect(summaryParam(requests, "filter")).toBe(JSON.stringify({ and: [failing] }));
    });
    expect(screen.getByRole("tab", { name: "Readiness" })).toHaveAttribute("aria-selected", "true");

    // Another count of the same criterion replaces its chip.
    await user.click(screen.getByRole("button", { name: "Owner is on call: 1 skipped" }));
    await vi.waitFor(() =>
      expect(router.state.location.search).toMatchObject({
        filter: [{ ...failing, value: "skipped" }],
      }),
    );
    expect(
      screen.getByRole("button", { name: "Readiness › Owner is on call: Skipped" }),
    ).toBeInTheDocument();
    // A count of none picks its status too, so the filter can move to it.
    await user.click(screen.getByRole("button", { name: "Owner is on call: 0 passed" }));
    await vi.waitFor(() =>
      expect(router.state.location.search).toMatchObject({
        filter: [{ ...failing, value: "passed" }],
      }),
    );
    await user.click(
      screen.getByRole("button", { name: "Remove Readiness › Owner is on call filter" }),
    );
    await vi.waitFor(() => expect(resultsParam(requests, "filter")).toBeNull());
  });

  it("filters its entities by tiers picked in the legend or the filter", async () => {
    const user = userEvent.setup();
    const requests = stubApi(catalogRoutes);
    const { router } = renderRoute("/catalog/service?view=scorecard-readiness");

    await user.click(await screen.findByRole("button", { name: /Bronze/ }));
    const bronze = { property: "readiness", op: "eq", value: "bronze" };
    await vi.waitFor(() => {
      expect(router.state.location.search).toMatchObject({ filter: [bronze] });
      expect(resultsParam(requests, "filter")).toBe(JSON.stringify({ and: [bronze] }));
    });

    // The filter adds tiers rather than replacing the one picked.
    const chip = screen.getByRole("button", { name: "Remove Readiness filter" }).parentElement;
    await user.click(within(chip as HTMLElement).getByRole("button", { name: /^Readiness/ }));
    await user.click(await screen.findByRole("option", { name: "No Tier" }));
    await vi.waitFor(() => {
      const filter = JSON.parse(resultsParam(requests, "filter") ?? "null");
      expect(filter.and).toHaveLength(1);
      expect(filter.and[0]).toMatchObject({ property: "readiness", op: "in" });
      expect(filter.and[0].values).toEqual(expect.arrayContaining(["bronze", ""]));
    });
  });

  it("keeps its filter for the entities tab, leaving its search behind", async () => {
    const user = userEvent.setup();
    const requests = stubApi(catalogRoutes);
    const filter = [{ property: "readiness.checks.hasOwner", op: "eq", value: "failed" }];
    const { router } = renderRoute(
      `/catalog/service?view=scorecard-readiness&q=led&filter=${encodeURIComponent(JSON.stringify(filter))}&page=2`,
    );

    await screen.findByRole("row", { name: /Ledger/ });
    expect(resultsParam(requests, "q")).toBe("led");
    expect(screen.getByRole("button", { name: "Readiness › Has an owner: Failed" })).toBeVisible();
    await user.click(screen.getByRole("tab", { name: /Entities/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({ view: "entities", filter }));
    await vi.waitFor(() =>
      expect(new URL(requests["GET catalog/service"].url).searchParams.get("filter")).toBe(
        JSON.stringify({ and: filter }),
      ),
    );
    expect(screen.getByRole("button", { name: "Readiness › Has an owner: Failed" })).toBeVisible();
  });

  it("offers its checks among the entities tab's filters", async () => {
    const user = userEvent.setup();
    stubApi(catalogRoutes);
    const { router } = renderRoute("/catalog/service");

    await user.click(await screen.findByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Readiness › Has an owner" }));
    await user.click(await screen.findByRole("option", { name: "Skipped" }));
    await vi.waitFor(() =>
      expect(router.state.location.search).toEqual({
        filter: [{ property: "readiness.checks.hasOwner", op: "eq", value: "skipped" }],
      }),
    );
  });

  it("shows a status it doesn't know, stored before an evaluation, as skipped", async () => {
    const removed: string = "errored";
    const stale = { ...ledger.criteria, onCall: { status: removed } } as EntityResult["criteria"];
    stubApi({
      ...member,
      "GET blueprints/service": ok(service),
      "GET catalog/service/entities/ledger": ok({
        blueprint: "service",
        identifier: "ledger",
        title: "Ledger",
        properties: {},
        related: {},
        connection: "registry",
        createdAt: "2026-10-24T10:00:00Z",
        updatedAt: "2026-10-24T10:00:00Z",
        incoming: [],
      }),
      "GET catalog/service/scorecards": ok([
        { ...ledger, criteria: stale, scorecard: "readiness", scorecardTitle: "Readiness" },
      ]),
      "GET scorecards/readiness": ok(readiness),
    });
    renderRoute("/catalog/service/ledger?tab=readiness");

    const marks = within(await screen.findByRole("list", { name: "Criteria" }));
    expect(marks.getAllByRole("listitem").map((item) => item.title)).toEqual([
      expect.stringMatching(/: Passed$/),
      expect.stringMatching(/: Skipped$/),
    ]);
  });

  it("links an entity's result to the scorecard's tab, marking each criterion in order", async () => {
    stubApi({
      ...member,
      "GET blueprints/service": ok(service),
      "GET catalog/service/entities/ledger": ok({
        blueprint: "service",
        identifier: "ledger",
        title: "Ledger",
        properties: {},
        related: {},
        connection: "registry",
        createdAt: "2026-10-24T10:00:00Z",
        updatedAt: "2026-10-24T10:00:00Z",
        incoming: [],
      }),
      "GET catalog/service/scorecards": ok([
        { ...ledger, scorecard: "readiness", scorecardTitle: "Readiness" },
      ]),
      "GET scorecards/readiness": ok(readiness),
    });
    renderRoute("/catalog/service/ledger");

    const tabs = (await screen.findAllByRole("tab")).map((tab) => tab.textContent);
    expect(tabs).toEqual(["Overview", "Readiness", "Relations0", "YAML"]);
    // Only the blueprint's own header badges sit beside the title.
    expect(screen.queryByRole("link", { name: /Readiness.*Bronze/ })).not.toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("tab", { name: "Readiness" }));
    const marks = within(await screen.findByRole("list", { name: "Criteria" }));
    // Marks keep the scorecard's order, whatever each check's outcome.
    expect(marks.getAllByRole("listitem").map((item) => item.title)).toEqual([
      expect.stringMatching(/: Passed$/),
      expect.stringMatching(/: Failed$/),
    ]);
    expect(screen.getByRole("link", { name: "Readiness" })).toHaveAttribute(
      "href",
      "/catalog/service?view=scorecard-readiness",
    );
    expect(screen.getByRole("list", { name: "Tiers" })).toHaveTextContent("Silver (next)");
    expect(screen.getAllByText("Add an on-call rotation.").length).toBeGreaterThan(0);
    // Each tier opens from its own row; one with something to fix starts open.
    expect(screen.getByRole("button", { name: /Silver/, expanded: true })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Bronze/, expanded: false })).toBeInTheDocument();
  });
});

const buildRoutes = {
  ...builder,
  "GET blueprints/service": ok(service),
  "GET blueprints/service/versions": ok([]),
  "GET blueprints/service/usage": ok({ entities: 4, sources: [], writers: [], references: [] }),
  "GET scorecards": scorecardList,
  "GET scorecards/readiness": ok(readiness),
  "GET scorecards/readiness/versions": ok([]),
  "GET scorecards/readiness/summary": ok(summary),
};

describe("building a scorecard", () => {
  it("lists a blueprint's scorecards, drafts too, on its build page", async () => {
    const user = userEvent.setup();
    stubApi(buildRoutes);
    const { router } = renderRoute("/blueprints/service");

    // The scorecard property opens the scorecard it holds.
    expect(await screen.findByRole("link", { name: /readiness/ })).toHaveAttribute(
      "href",
      "/blueprints/service/scorecards/readiness",
    );
    await user.click(screen.getByRole("tab", { name: "Scorecards" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "scorecards" }));
    expect(await screen.findByRole("link", { name: "Readiness" })).toHaveAttribute(
      "href",
      "/blueprints/service/scorecards/readiness",
    );
    expect(screen.getByRole("link", { name: "Security" })).toHaveAttribute(
      "href",
      "/blueprints/service/scorecards/security",
    );
    expect(screen.queryByRole("link", { name: "Team Health" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "New Scorecard" })).toHaveAttribute(
      "href",
      "/blueprints/service/scorecards/new",
    );
  });

  it("shows the definition under its blueprint, with what builders do to it", async () => {
    stubApi(buildRoutes);
    renderRoute("/blueprints/service/scorecards/readiness");

    const header = (await screen.findByRole("heading", { level: 1, name: "Readiness" })).closest(
      "[data-slot=page-header]",
    ) as HTMLElement;
    const crumbs = within(header).getByRole("navigation", { name: "breadcrumb" });
    expect(within(crumbs).getByRole("link", { name: "Blueprints" })).toHaveAttribute(
      "href",
      "/blueprints",
    );
    expect(within(crumbs).getByRole("link", { name: "Service" })).toHaveAttribute(
      "href",
      "/blueprints/service?tab=scorecards",
    );
    const tabs = screen.getAllByRole("tab").map((tab) => tab.textContent);
    expect(tabs).toEqual(["Overview", "Definition", "Versions"]);
    expect(screen.getByRole("link", { name: "View in Catalog" })).toHaveAttribute(
      "href",
      "/catalog/service?view=scorecard-readiness",
    );
    expect(screen.getByRole("link", { name: "Export" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Evaluate Now" })).toBeInTheDocument();
    expect(screen.getByText("Add an on-call rotation.")).toBeInTheDocument();
    const about = within(screen.getByText("About").closest("[data-slot=card]") as HTMLElement);
    expect(await about.findByText("Property")).toBeInTheDocument();
    expect(about.getByText("readiness")).toBeInTheDocument();
    expect(await about.findByText("Entities")).toBeInTheDocument();
    // How entities fare is the catalog's to show.
    expect(screen.queryByText("66%")).not.toBeInTheDocument();
  });

  it("shows each criterion's check and when as the filters they are", async () => {
    stubApi(buildRoutes);
    renderRoute("/blueprints/service/scorecards/readiness");

    await userEvent.setup().hover(await screen.findByText("Owner is on call"));
    expect(await screen.findByText("ownerOncall: Any Value")).toBeVisible();
    expect(screen.getByText("tier: critical")).toBeVisible();
    expect(screen.getByText("Only When")).toBeVisible();
  });

  it("moves to the blueprint the scorecard grades", async () => {
    stubApi({ ...buildRoutes, "GET blueprints/team": ok({ ...service, identifier: "team" }) });
    const { router } = renderRoute("/blueprints/team/scorecards/readiness?tab=versions");

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/blueprints/service/scorecards/readiness"),
    );
    expect(router.state.location.search).toEqual({ tab: "versions" });
  });

  it("keeps members out", async () => {
    stubApi({ ...buildRoutes, ...member });
    renderRoute("/blueprints/service/scorecards/readiness");

    expect(await screen.findByText("You don't have access to this page")).toBeInTheDocument();
  });

  it("starts a new scorecard on its blueprint", async () => {
    let source = "";
    stubApi({
      ...builder,
      "GET blueprints/service": ok(service),
      "POST scorecards/validate": async (request) => {
        ({ source } = (await request.json()) as { source: string });
        return Response.json({ data: { spec: { ...spec, identifier: "example" }, issues: [] } });
      },
    });
    renderRoute("/blueprints/service/scorecards/new");

    expect(await screen.findByRole("heading", { level: 1, name: "New Scorecard" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Service" })).toHaveAttribute(
      "href",
      "/blueprints/service?tab=scorecards",
    );
    await waitFor(() => expect(source).toContain("  blueprint: service\n"));
  });
});

describe("a scorecard's old links", () => {
  it("open its results in the catalog", async () => {
    stubApi(catalogRoutes);
    const { router } = renderRoute("/scorecards/readiness");

    await waitFor(() => expect(router.state.location.pathname).toBe("/catalog/service"));
    expect(router.state.location.search).toEqual({ view: "scorecard-readiness" });
    expect(await screen.findByRole("tab", { name: "Readiness" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("open its definition and versions on its build page", async () => {
    stubApi(buildRoutes);
    const { router } = renderRoute("/scorecards/readiness?tab=definition");

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/blueprints/service/scorecards/readiness"),
    );
    expect(router.state.location.search).toEqual({ tab: "definition" });
  });

  it("open one not yet published on its build page", async () => {
    const draft: Scorecard = {
      ...readiness,
      published: null,
      draft: { source: "", spec, issues: [], updatedAt: "2026-10-24T10:00:00Z", updatedBy: "" },
    };
    stubApi({ ...buildRoutes, "GET scorecards/readiness": ok(draft) });
    const { router } = renderRoute("/scorecards/readiness");

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/blueprints/service/scorecards/readiness"),
    );
  });

  it("open the catalog for the list and blueprints for a new one", async () => {
    stubApi({ ...builder, "GET catalog": ok([]), "GET blueprints": ok([]) });
    const list = renderRoute("/scorecards");
    await waitFor(() => expect(list.router.state.location.pathname).toBe("/catalog"));
    list.unmount();

    const fresh = renderRoute("/scorecards/new");
    await waitFor(() => expect(fresh.router.state.location.pathname).toBe("/blueprints"));
  });
});
