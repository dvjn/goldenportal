import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Scorecard, ScorecardSpec, Summary } from "@/features/scorecards";
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
        { identifier: "hasOwner", title: "Has an owner", check: ".properties.owner != null" },
      ],
    },
    {
      name: "silver",
      title: "Silver",
      criteria: [
        {
          identifier: "onCall",
          title: "Owner is on call",
          check: ".properties.ownerOncall != null",
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

const summary: Summary = {
  spec,
  entities: 3,
  evaluatedAt: "2026-10-24T10:00:00Z",
  tiers: [
    { name: "", count: 1 },
    { name: "bronze", count: 2 },
    { name: "silver", count: 0 },
  ],
  criteria: [
    { identifier: "hasOwner", passed: 2, failed: 1, skipped: 0, errored: 0 },
    { identifier: "onCall", passed: 0, failed: 2, skipped: 1, errored: 0 },
  ],
};

const member = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) };

describe("scorecards", () => {
  it("lists scorecards with their tier distribution", async () => {
    stubApi({
      ...member,
      "GET scorecards": ok([
        {
          identifier: "readiness",
          title: "Readiness",
          publishedVersion: 1,
          hasDraft: false,
          stats: { tiers: 2, criteria: 2 },
          updatedAt: "2026-10-24T10:00:00Z",
        },
      ]),
      "GET scorecards/readiness/summary": ok(summary),
    });
    renderRoute("/scorecards");

    const card = (await screen.findByText("Readiness")).closest("[data-slot=card]") as HTMLElement;
    // 2 of the 5 checks that ran pass.
    expect(await within(card).findByText("40%")).toBeInTheDocument();
    expect(within(card).getByText("entities").previousElementSibling).toHaveTextContent("3");
    expect(within(card).getByText("Bronze")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /New Scorecard/ })).not.toBeInTheDocument();
  });

  it("shows members the results without the definition", async () => {
    stubApi({
      ...member,
      "GET scorecards/readiness": ok(readiness),
      "GET scorecards/readiness/versions": ok([]),
      "GET scorecards/readiness/summary": ok(summary),
      "GET scorecards/readiness/results": ok({
        items: [
          {
            blueprint: "service",
            identifier: "ledger",
            title: "Ledger",
            evaluatedAt: "2026-10-24T10:00:00Z",
            tier: "bronze",
            score: 50,
            passed: 1,
            failed: 1,
            skipped: 0,
            errored: 0,
            criteria: { hasOwner: { status: "passed" }, onCall: { status: "failed" } },
          },
        ],
        total: 1,
      }),
    });
    renderRoute("/scorecards/readiness");

    const row = await screen.findByRole("row", { name: /Ledger/ });
    expect(within(row).getByText("50%")).toBeInTheDocument();
    expect(within(row).getByText("Owner is on call")).toBeInTheDocument();
    expect(await screen.findByText("66%")).toBeInTheDocument();
    expect(screen.getByText("40%")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: "Ledger" })).toHaveAttribute(
      "href",
      "/catalog/service/ledger?tab=readiness",
    );
    expect(screen.queryByRole("tab", { name: "Definition" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Evaluate Now" })).not.toBeInTheDocument();
  });

  it("filters the entities by tiers picked in the legend or the filter", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...member,
      "GET scorecards/readiness": ok(readiness),
      "GET scorecards/readiness/versions": ok([]),
      "GET scorecards/readiness/summary": ok(summary),
      "GET scorecards/readiness/results": ok({ items: [], total: 0 }),
    });
    const { router } = renderRoute("/scorecards/readiness");

    const legend = await screen.findByRole("button", { name: /Bronze/ });
    await user.click(legend);
    await vi.waitFor(() => {
      expect(router.state.location.search).toMatchObject({ tier: "bronze" });
      const url = new URL(requests["GET scorecards/readiness/results"].url);
      expect(url.searchParams.get("tier")).toBe("bronze");
    });

    // The filter adds tiers rather than replacing the one picked.
    await user.click(screen.getByRole("button", { name: /^Tier/ }));
    await user.click(await screen.findByRole("option", { name: "No tier" }));
    await vi.waitFor(() => {
      const url = new URL(requests["GET scorecards/readiness/results"].url);
      expect(url.searchParams.get("tier")).toBe("none,bronze");
    });
  });

  it("marks each criterion on an entity in the scorecard's order", async () => {
    stubApi({
      ...member,
      "GET blueprints/service": ok({
        identifier: "service",
        title: "Service",
        published: {
          version: 1,
          source: "",
          spec: { identifier: "service", title: "Service", properties: [] },
          publishedAt: "2026-10-24T10:00:00Z",
          publishedBy: "",
        },
        draft: null,
        createdAt: "2026-10-24T10:00:00Z",
      }),
      "GET catalog/service/entities/ledger": ok({
        blueprint: "service",
        identifier: "ledger",
        title: "Ledger",
        properties: {},
        related: {},
        integration: "registry",
        createdAt: "2026-10-24T10:00:00Z",
        updatedAt: "2026-10-24T10:00:00Z",
        incoming: [],
      }),
      "GET catalog/service/scorecards": ok([
        {
          scorecard: "readiness",
          scorecardTitle: "Readiness",
          blueprint: "service",
          identifier: "ledger",
          title: "Ledger",
          evaluatedAt: "2026-10-24T10:00:00Z",
          tier: "bronze",
          score: 50,
          passed: 1,
          failed: 1,
          skipped: 0,
          errored: 0,
          criteria: { hasOwner: { status: "passed" }, onCall: { status: "failed" } },
        },
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
    expect(screen.getByRole("list", { name: "Tiers" })).toHaveTextContent("Silver (next)");
    expect(screen.getAllByText("Add an on-call rotation.").length).toBeGreaterThan(0);
    // Each tier opens from its own row; one with something to fix starts open.
    expect(screen.getByRole("button", { name: /Silver/, expanded: true })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Bronze/, expanded: false })).toBeInTheDocument();
  });
});
