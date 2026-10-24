import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint } from "@/features/blueprints";
import type { EntityDetail, EntityPage } from "@/features/catalog";
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

  it("filters a blueprint's entities on the server", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities": ok(page),
      "GET entities/facets": ok([{ value: "payments", title: "Payments", count: 1 }]),
    });
    renderRoute("/catalog/service");

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
  });

  it("shows an entity with its relations both ways", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInMember,
      "GET blueprints/service": ok(service),
      "GET entities/service/ledger": ok(ledger),
    });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("heading", { name: "Ledger" })).toBeInTheDocument();
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
