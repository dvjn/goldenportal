import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint } from "@/features/blueprints";
import type { EntityDetail, EntityPage } from "@/features/catalog";
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

describe("catalog", () => {
  it("shows scorecard tiers, filters them on the server and lets columns be hidden", async () => {
    const user = userEvent.setup();
    const scored = { ...ledger, scorecards: { readiness: { tier: "gold", score: 100 } } };
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities": ok({ entities: [scored], total: 1 }),
      "GET scorecard-columns": ok([
        {
          identifier: "readiness",
          title: "Readiness",
          tiers: [{ name: "gold", title: "Gold", color: "amber", criteria: [] }],
        },
      ]),
      "GET entities/facets": ok([]),
    });
    renderRoute("/catalog/service");
    expect(await screen.findByText("Gold")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Readiness" })).toBeInTheDocument();
    await user.click(
      within(screen.getByRole("columnheader", { name: /Readiness/ })).getByRole("button"),
    );
    await vi.waitFor(() =>
      expect(new URL(requests["GET entities"].url).searchParams.get("sort")).toBe(
        "$scorecard.readiness",
      ),
    );
    await user.click(screen.getAllByRole("button", { name: "Readiness" })[0]);
    await user.click(await screen.findByRole("option", { name: "Gold" }));
    await vi.waitFor(() =>
      expect(
        JSON.parse(new URL(requests["GET entities"].url).searchParams.get("filter") ?? "null"),
      ).toEqual({ and: [{ property: "$scorecard.readiness", op: "in", values: ["gold"] }] }),
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

    const card = (await screen.findByText("Service")).closest("a") as HTMLElement;
    expect(card).toHaveAttribute("href", "/catalog/service");
    expect(within(card).getByText("1.2K")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Catalog" })).toBeInTheDocument();
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
    renderRoute("/catalog/service");
    expect(await screen.findByText(/3 entities/)).toBeInTheDocument();

    const row = (await screen.findByRole("link", { name: /Ledger/ })).closest("tr") as HTMLElement;
    expect(within(row).getByText("critical")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: "Payments" })).toHaveAttribute(
      "href",
      "/catalog/team/payments",
    );
    expect(within(row).getByText("#payments")).toBeInTheDocument();

    // The filter comes before the column header of the same name.
    await user.click(screen.getAllByRole("button", { name: "Tier" })[0]);
    await user.click(await screen.findByRole("option", { name: "critical" }));

    await vi.waitFor(() => {
      const url = new URL(requests["GET entities"].url);
      expect(JSON.parse(url.searchParams.get("filter") ?? "null")).toEqual({
        and: [{ property: "tier", op: "in", values: ["critical"] }],
      });
    });
    expect(await screen.findByText(/1 of 3 match/)).toBeInTheDocument();
  });

  it("filters entities by when they were updated", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities": ok(page),
      "GET entities/facets": ok([]),
    });
    renderRoute("/catalog/service");

    // The filter comes before the column header of the same name.
    await user.click((await screen.findAllByRole("button", { name: "Updated" }))[0]);
    await user.click(await screen.findByRole("button", { name: "Older than 90 days" }));

    await vi.waitFor(() => {
      const filter = JSON.parse(
        new URL(requests["GET entities"].url).searchParams.get("filter") ?? "null",
      );
      expect(filter.and).toEqual([
        { property: "$updatedAt", op: "lt", value: expect.stringMatching(/^\d{4}-\d{2}-\d{2}T/) },
      ]);
    });
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
    expect(screen.getByRole("link", { name: /Open on ledger.example.com/ })).toHaveAttribute(
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
    expect(screen.getByRole("link", { name: "registry" })).toHaveAttribute(
      "href",
      "/integrations/registry",
    );
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
