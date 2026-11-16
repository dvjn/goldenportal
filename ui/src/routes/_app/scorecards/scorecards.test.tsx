import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint } from "@/features/blueprints";
import type { EntityResult, Scorecard, ScorecardSpec, Summary } from "@/features/scorecards";
import { acmeConfig, ada, fail, ok, stubApi } from "@/test/api";
import { createTestQueryClient, renderRoute } from "@/test/render";

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
  publishedFacts: publishedVersion > 0 ? { blueprint } : undefined,
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
    // 2 of the 5 checks that ran pass.
    expect(await screen.findByText("40%")).toBeInTheDocument();
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
    expect(screen.getByRole("columnheader", { name: "Score" })).toHaveAttribute(
      "aria-sort",
      "descending",
    );
    expect(screen.getByRole("columnheader", { name: "Tier" })).toHaveAttribute("aria-sort", "none");

    // Unsorted is worst tier first, so tiers sort best first, then worst first.
    const sorted = async (sort: string) =>
      vi.waitFor(() => {
        expect(router.state.location.search).toMatchObject({ view: "scorecard-readiness", sort });
        expect(resultsParam(requests, "sort")).toBe(sort);
      });
    await user.click(screen.getByRole("button", { name: "Tier" }));
    await sorted("-tier");
    expect(screen.getByRole("columnheader", { name: "Tier" })).toHaveAttribute(
      "aria-sort",
      "descending",
    );
    await user.click(screen.getByRole("button", { name: "Tier" }));
    await sorted("tier");
    await user.click(screen.getByRole("button", { name: "Failed" }));
    await sorted("-failed");
  });

  it("links each criterion's counts to the entities tab, filtered to their status", async () => {
    const user = userEvent.setup();
    const requests = stubApi(catalogRoutes);
    const { router } = renderRoute("/catalog/service?view=scorecard-readiness");

    expect(
      await screen.findByRole("link", { name: "Owner is on call: 1 skipped" }),
    ).toBeInTheDocument();
    // A count of none has no entities to open.
    expect(screen.queryByRole("link", { name: "Owner is on call: 0 passed" })).toBeNull();
    await user.click(screen.getByRole("link", { name: "Owner is on call: 2 failed" }));
    const failing = { property: "readiness.checks.onCall", op: "eq", value: "failed" };
    await vi.waitFor(() => {
      expect(router.state.location.search).toEqual({ view: "entities", filter: [failing] });
      expect(new URL(requests["GET catalog/service"].url).searchParams.get("filter")).toBe(
        JSON.stringify({ and: [failing] }),
      );
    });
    expect(screen.getByRole("tab", { name: /Entities/ })).toHaveAttribute("aria-selected", "true");
    expect(
      await screen.findByRole("button", { name: "Readiness › Owner is on call: Failed" }),
    ).toBeVisible();
  });

  it("links each tier's count to the entities tab, filtered to that tier", async () => {
    const user = userEvent.setup();
    stubApi(catalogRoutes);
    const { router } = renderRoute(
      "/catalog/service?view=scorecard-readiness&cols=readiness&size=50&sort=-score",
    );

    expect(await screen.findByRole("link", { name: "Bronze: 2" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Silver: 0" })).toBeNull();
    await user.click(screen.getByRole("link", { name: "No Tier: 1" }));
    // The viewer's columns and page size come along; the sort was the tab's.
    await vi.waitFor(() =>
      expect(router.state.location.search).toEqual({
        view: "entities",
        filter: [{ property: "readiness", op: "eq", value: "" }],
        cols: "readiness",
        size: 50,
      }),
    );
  });

  it("shows the whole scorecard, with no search and without the entities tab's filter", async () => {
    const user = userEvent.setup();
    const requests = stubApi(catalogRoutes);
    const owned = { property: "readiness.checks.hasOwner", op: "eq", value: "failed" };
    const { router } = renderRoute(
      `/catalog/service?view=scorecard-readiness&q=led&filter=${encodeURIComponent(JSON.stringify([owned]))}`,
    );

    await screen.findByRole("row", { name: /Ledger/ });
    expect(await screen.findByText("40%")).toBeInTheDocument();
    expect(resultsParam(requests, "filter")).toBeNull();
    expect(resultsParam(requests, "q")).toBeNull();
    expect(summaryParam(requests, "filter")).toBeNull();
    expect(screen.queryByRole("searchbox")).toBeNull();
    expect(screen.queryByRole("button", { name: /Readiness ›/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Filter" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Clear Filters" })).toBeNull();

    // A count's link lists what it counted, in place of the entities' filter.
    await user.click(screen.getByRole("link", { name: "Bronze: 2" }));
    await vi.waitFor(() =>
      expect(router.state.location.search).toEqual({
        view: "entities",
        filter: [{ property: "readiness", op: "eq", value: "bronze" }],
      }),
    );
  });

  it("says when it can't count the entities", async () => {
    stubApi({
      ...catalogRoutes,
      "GET scorecards/readiness/summary": fail(400, "the results aren't readable"),
    });
    renderRoute("/catalog/service?view=scorecard-readiness");

    expect(await screen.findAllByText("Can't count these entities.")).toHaveLength(2);
    expect(screen.queryByText(/0 passed/)).not.toBeInTheDocument();
    expect(await screen.findByRole("row", { name: /Ledger/ })).toBeVisible();
  });

  it("goes back to the first page of results when the server turns a cursor down", async () => {
    stubApi({
      ...catalogRoutes,
      "GET scorecards/readiness/results": (request) =>
        new URL(request.url).searchParams.has("cursor")
          ? fail(400, "the cursor isn't one this list gave; start from the first page")()
          : ok({ items: [ledger], total: 1 })(),
    });
    const { router } = renderRoute("/catalog/service?view=scorecard-readiness&cursor=stale");

    expect(await screen.findByRole("row", { name: /Ledger/ })).toBeVisible();
    expect(router.state.location.search).not.toHaveProperty("cursor");
  });

  it("keeps the entities tab's filter for coming back to, leaving the search behind", async () => {
    const user = userEvent.setup();
    const requests = stubApi(catalogRoutes);
    const filter = [{ property: "readiness.checks.hasOwner", op: "eq", value: "failed" }];
    const { router } = renderRoute(
      `/catalog/service?q=led&filter=${encodeURIComponent(JSON.stringify(filter))}`,
    );
    const chip = { name: "Readiness › Has an owner: Failed" };

    expect(await screen.findByRole("button", chip)).toBeVisible();
    await user.click(screen.getByRole("tab", { name: "Readiness" }));
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ view: "scorecard-readiness", filter }),
    );
    await screen.findByRole("row", { name: /Ledger/ });
    expect(resultsParam(requests, "q")).toBeNull();
    expect(resultsParam(requests, "filter")).toBeNull();
    expect(screen.queryByRole("button", chip)).toBeNull();

    await user.click(screen.getByRole("tab", { name: /Entities/ }));
    await waitFor(() => expect(router.state.location.search).toEqual({ view: "entities", filter }));
    expect(await screen.findByRole("button", chip)).toBeVisible();
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
    expect(screen.queryByText("67%")).not.toBeInTheDocument();
  });

  it("refreshes results wherever they show once evaluated on demand", async () => {
    const user = userEvent.setup();
    stubApi({ ...buildRoutes, "POST scorecards/readiness/evaluate": ok({ entities: 3 }) });
    const queryClient = createTestQueryClient();
    const cached = [
      ["widgets", { dashboard: "teams" }, {}],
      ["catalog", "entities", { blueprint: "service" }],
      ["entity-scorecards", "service", "ledger"],
    ];
    for (const key of cached) queryClient.setQueryData(key, {});
    renderRoute("/blueprints/service/scorecards/readiness", queryClient);

    await user.click(await screen.findByRole("button", { name: "Evaluate Now" }));
    expect(await screen.findByText("Evaluated 3 entities")).toBeInTheDocument();
    for (const key of cached) {
      expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
    }
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
