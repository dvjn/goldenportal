import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Integration } from "@/features/integrations";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const inventory: Integration = {
  identifier: "inventory",
  title: "Inventory",
  published: {
    version: 1,
    source: "identifier: inventory\n",
    spec: {
      identifier: "inventory",
      title: "Inventory",
      connector: { type: "push" },
      mappings: [
        {
          blueprint: "service",
          items: ".services[]",
          entity: {
            identifier: ".id",
            properties: [{ identifier: "owner", expression: ".team" }],
          },
        },
      ],
    },
    publishedAt: "2026-10-24T10:00:00Z",
    publishedBy: "ada@example.com",
  },
  draft: null,
  createdAt: "2026-10-24T10:00:00Z",
};

const routes = {
  "GET config": acmeConfig,
  "GET me": ok({ ...ada, role: "builder" }),
  "GET integrations/inventory": ok(inventory),
  "GET integrations/inventory/versions": ok([]),
  "GET connectors": ok([
    {
      id: "push",
      title: "Push",
      description: "Other systems send entities.",
      icon: "webhook",
      color: "violet",
      mode: "push",
    },
  ]),
  "GET catalog": ok([{ identifier: "service", title: "Service", entities: 12 }]),
  "GET integrations/inventory/tokens": ok([]),
  "GET integration-status": ok([
    {
      integration: "inventory",
      lastRun: null,
      feeds: [{ blueprint: "service", title: "Service", entities: 12 }],
    },
  ]),
  "GET integrations/inventory/runs": ok({
    total: 1,
    runs: [
      {
        id: 1,
        startedAt: "2026-10-24T10:00:00Z",
        finishedAt: "2026-10-24T10:00:01Z",
        trigger: "push",
        token: "ci",
        received: 3,
        created: 1,
        updated: 0,
        unchanged: 0,
        deleted: 0,
        failed: 2,
        errors: [
          { index: 1, blueprint: "service", identifier: "x", problems: ["tier is required"] },
        ],
      },
    ],
  }),
};

describe("integrations", () => {
  it("shows the mappings and the push endpoint, with runs in their own tab", async () => {
    const user = userEvent.setup();
    stubApi(routes);
    renderRoute("/integrations/inventory");

    const mappings = (await screen.findByText("Mappings")).closest(
      "[data-slot=card]",
    ) as HTMLElement;
    expect(await within(mappings).findByRole("link", { name: "Service" })).toHaveAttribute(
      "href",
      "/blueprints/service",
    );
    expect(screen.getByText(".services[]")).toBeInTheDocument();
    expect(screen.getByText(/\/api\/v1\/ingest\/inventory$/)).toBeInTheDocument();
    expect(await screen.findByText("Push Connector")).toBeInTheDocument();

    const feeds = (await screen.findByText("Feeds")).closest("[data-slot=card]") as HTMLElement;
    expect(within(feeds).getByRole("link", { name: "Service" })).toHaveAttribute(
      "href",
      "/catalog/service",
    );
    expect(within(feeds).getByTitle("12 entities")).toHaveTextContent("12");

    await user.click(screen.getByRole("tab", { name: "Runs" }));
    expect(await screen.findByLabelText("Succeeded with warnings")).toBeInTheDocument();
    expect(screen.getByText("1s")).toBeInTheDocument();

    await user.click(await screen.findByRole("button", { name: "View failures" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("tier is required")).toBeInTheDocument();
  });

  it("filters runs by outcome on the server", async () => {
    const user = userEvent.setup();
    const requests = stubApi(routes);
    renderRoute("/integrations/inventory?tab=runs");

    await screen.findByLabelText("Succeeded with warnings");
    expect(screen.getByText("Showing 1 to 1 of 1")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Outcome" }));
    await user.click(await screen.findByRole("option", { name: /Failed/ }));

    await waitFor(() =>
      expect(
        new URL(requests["GET integrations/inventory/runs"]?.url ?? "").searchParams.get("outcome"),
      ).toBe("failed"),
    );
  });

  it("lists integrations with how their last run went", async () => {
    const run = {
      id: 2,
      startedAt: "2026-10-24T10:00:00Z",
      finishedAt: "2026-10-24T10:00:00Z",
      trigger: "schedule",
      received: 0,
      created: 0,
      updated: 0,
      unchanged: 0,
      deleted: 0,
      failed: 0,
      errors: [],
      error: "gitlab API answered 401 Unauthorized",
    };
    stubApi({
      ...routes,
      "GET integrations": ok([
        {
          identifier: "inventory",
          title: "Inventory",
          publishedVersion: 1,
          hasDraft: false,
          updatedAt: "2026-10-24T10:00:00Z",
          stats: {},
        },
        {
          identifier: "fresh",
          title: "Fresh",
          publishedVersion: 1,
          hasDraft: false,
          updatedAt: "2026-10-24T10:00:00Z",
          stats: {},
        },
      ]),
      "GET integration-status": ok([
        { integration: "inventory", lastRun: run, feeds: [] },
        { integration: "fresh", lastRun: null, feeds: [] },
      ]),
    });
    renderRoute("/integrations");

    const failed = (await screen.findByRole("link", { name: /Inventory/ })).closest(
      "tr",
    ) as HTMLElement;
    expect(await within(failed).findByLabelText("Failed")).toBeInTheDocument();
    const fresh = screen.getByRole("link", { name: /Fresh/ }).closest("tr") as HTMLElement;
    expect(within(fresh).getByText("Never run")).toBeInTheDocument();
  });

  it("shows a new ingestion token's secret once", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...routes,
      "POST integrations/inventory/tokens": ok({
        id: "t1",
        name: "CI",
        prefix: "gpi_abcdefgh",
        createdBy: "ada@example.com",
        createdAt: "2026-10-24T10:00:00Z",
        lastUsedAt: null,
        secret: "gpi_abcdefghsecret",
      }),
    });
    renderRoute("/integrations/inventory");

    await user.click(await screen.findByRole("button", { name: "New Token" }));
    await user.type(screen.getByLabelText("Name"), "CI");
    await user.click(screen.getByRole("button", { name: "Create Token" }));

    expect(await screen.findByLabelText("New ingestion token")).toHaveValue("gpi_abcdefghsecret");
    expect(await requests["POST integrations/inventory/tokens"].json()).toEqual({ name: "CI" });
  });

  it("syncs a pull integration instead of taking pushes", async () => {
    const user = userEvent.setup();
    const portal = {
      ...inventory,
      identifier: "portal",
      published: inventory.published && {
        ...inventory.published,
        spec: {
          ...inventory.published.spec,
          identifier: "portal",
          connector: { type: "portal" },
          sync: { interval: "1m" },
        },
      },
    };
    const requests = stubApi({
      ...routes,
      "GET integrations/portal": ok(portal),
      "GET integrations/portal/versions": ok([]),
      "GET integrations/portal/runs": ok({ runs: [], total: 0 }),
      "GET connectors": ok([
        { id: "portal", title: "Portal", description: "The portal's own objects.", mode: "pull" },
      ]),
      "POST integrations/portal/sync": ok({
        id: 2,
        startedAt: "2026-10-24T10:00:00Z",
        finishedAt: "2026-10-24T10:00:01Z",
        trigger: "manual",
        received: 3,
        created: 1,
        updated: 1,
        unchanged: 1,
        deleted: 2,
        failed: 0,
        errors: [],
      }),
    });
    renderRoute("/integrations/portal");

    await user.click(await screen.findByRole("button", { name: "Sync Now" }));
    expect(await screen.findByText("Synced: 1 created, 1 updated, 2 deleted")).toBeInTheDocument();
    expect(requests["POST integrations/portal/sync"]).toBeDefined();
    expect(screen.queryByText("Push Endpoint")).not.toBeInTheDocument();
    expect(screen.queryByText("Ingestion Tokens")).not.toBeInTheDocument();
  });
});
