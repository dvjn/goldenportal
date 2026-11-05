import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Integration } from "@/features/integrations";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
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
      connector: {
        type: "http",
        config: { resources: { services: { push: { items: ".body.services[]" } } } },
      },
      pulls: false,
      receives: true,
      events: false,
      checks: false,
      mappings: [
        {
          resource: "services",
          blueprint: "service",
          filter: ".active",
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
      id: "http",
      title: "HTTP",
      description: "JSON fetched from URLs, or sent to the ingest API.",
      icon: "globe",
      color: "violet",
      pulls: true,
      receives: true,
      events: false,
      checks: true,
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
        mode: "complete",
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

const portal: Integration = {
  ...inventory,
  identifier: "portal",
  published: inventory.published && {
    ...inventory.published,
    spec: {
      ...inventory.published.spec,
      identifier: "portal",
      connector: { type: "portal" },
      sync: { interval: "1m", complete: "1d" },
      pulls: true,
      receives: false,
      events: false,
      checks: true,
    },
  },
};

const syncRun = {
  id: 2,
  startedAt: "2026-10-24T10:00:00Z",
  finishedAt: "2026-10-24T10:00:01Z",
  trigger: "manual",
  mode: "incremental",
  received: 3,
  created: 1,
  updated: 1,
  unchanged: 1,
  deleted: 2,
  failed: 0,
  errors: [],
};

// runningRun is a sync as Sync Now answers: started, and not finished yet.
const runningRun = {
  ...syncRun,
  finishedAt: null,
  received: 0,
  created: 0,
  updated: 0,
  unchanged: 0,
  deleted: 0,
};

const pullRoutes = {
  ...routes,
  "GET integrations/portal": ok(portal),
  "GET integrations/portal/versions": ok([]),
  "GET integrations/portal/runs": ok({ runs: [], total: 0 }),
  "GET connectors": ok([
    {
      id: "portal",
      title: "Portal",
      description: "The portal's own objects.",
      icon: "box",
      color: "blue",
      pulls: true,
      receives: false,
      events: false,
      checks: true,
    },
  ]),
  "POST integrations/portal/sync": ok(runningRun),
};

describe("integrations", () => {
  it("shows the mappings, with runs and pushing in their own tabs", async () => {
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
    expect(within(mappings).getByText("services")).toBeInTheDocument();
    expect(within(mappings).getByText(".active")).toBeInTheDocument();
    expect(await screen.findByText("HTTP Connector")).toBeInTheDocument();
    // Its config only takes pushes, though the connector could pull or check too.
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(within(about).getByText("Push")).toBeInTheDocument();
    expect(within(about).queryByText("Pull")).not.toBeInTheDocument();
    expect(within(about).queryByText("Syncs Every")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Test Connection" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sync Now" })).not.toBeInTheDocument();
    expect(screen.getByText("Never run")).toBeInTheDocument();

    const feeds = (await screen.findByText("Feeds")).closest("[data-slot=card]") as HTMLElement;
    expect(within(feeds).getByRole("link", { name: "Service" })).toHaveAttribute(
      "href",
      "/catalog/service",
    );
    expect(within(feeds).getByTitle("12 entities")).toHaveTextContent("12");

    await user.click(screen.getByRole("tab", { name: "Runs" }));
    expect(await screen.findByLabelText("Succeeded with Warnings")).toBeInTheDocument();
    expect(screen.getByText("1s")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Mode" })).toBeInTheDocument();
    expect(screen.getByText("Complete")).toBeInTheDocument();

    await user.click(await screen.findByRole("button", { name: "View problems" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("tier is required")).toBeInTheDocument();
    await user.keyboard("{Escape}");

    // Pushing shows requests to copy, aimed at the endpoint in either mode.
    await user.click(screen.getByRole("tab", { name: "Push" }));
    expect(
      await screen.findByText(/curl -X POST ".*\/api\/ingest\/inventory\?mode=incremental"/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/curl -X POST ".*\/api\/ingest\/inventory\?mode=complete"/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Copy incremental push request/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Copy complete push request/i })).toBeInTheDocument();
  });

  it("filters runs by outcome on the server", async () => {
    const user = userEvent.setup();
    const requests = stubApi(routes);
    renderRoute("/integrations/inventory?tab=runs");

    await screen.findByLabelText("Succeeded with Warnings");
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

  it("links a run an action made to that action run", async () => {
    stubApi({
      ...routes,
      "GET integrations/inventory/runs": ok({
        total: 1,
        runs: [
          {
            id: 2,
            startedAt: "2026-10-24T10:00:00Z",
            finishedAt: "2026-10-24T10:00:01Z",
            trigger: "action",
            mode: "incremental",
            actionRun: "run-1",
            action: "Open incident",
            received: 1,
            created: 1,
            updated: 0,
            unchanged: 0,
            deleted: 0,
            failed: 0,
            errors: [],
          },
        ],
      }),
    });
    renderRoute("/integrations/inventory?tab=runs");

    expect(await screen.findByRole("link", { name: "Action · Open incident" })).toHaveAttribute(
      "href",
      "/runs/run-1",
    );
  });

  it("lists integrations with how their last run went", async () => {
    const run = {
      id: 2,
      startedAt: "2026-10-24T10:00:00Z",
      finishedAt: "2026-10-24T10:00:00Z",
      trigger: "schedule",
      mode: "complete",
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
    renderRoute("/integrations/inventory?tab=push");

    await user.click(await screen.findByRole("button", { name: "New Token" }));
    await user.type(screen.getByLabelText("Name"), "CI");
    await user.click(screen.getByRole("button", { name: "Create Token" }));

    expect(await screen.findByLabelText("New ingestion token")).toHaveValue("gpi_abcdefghsecret");
    expect(await requests["POST integrations/inventory/tokens"].json()).toEqual({ name: "CI" });
  });

  it("syncs an integration that pulls instead of taking pushes", async () => {
    const user = userEvent.setup();
    const requests = stubApi(pullRoutes);
    renderRoute("/integrations/portal");

    await user.click(await screen.findByRole("button", { name: "Sync Now" }));
    expect(await screen.findByText("Started the sync")).toBeInTheDocument();
    // Without a mode, the schedule decides.
    expect(new URL(requests["POST integrations/portal/sync"]?.url ?? "").search).toBe("");
    // It pulls every minute, completely every day, so there's no Push tab.
    expect(screen.getByText("Syncs Every").nextElementSibling).toHaveTextContent("1m");
    expect(screen.getByText("Complete Every").nextElementSibling).toHaveTextContent("1d");
    expect(screen.queryByRole("tab", { name: "Push" })).not.toBeInTheDocument();
  });

  it("runs a complete sync from the Sync Now menu", async () => {
    const user = userEvent.setup();
    const requests = stubApi(pullRoutes);
    renderRoute("/integrations/portal");

    await user.click(await screen.findByRole("button", { name: "More Sync Options" }));
    await user.click(await screen.findByRole("menuitem", { name: "Complete Sync" }));
    await waitFor(() =>
      expect(
        new URL(requests["POST integrations/portal/sync"]?.url ?? "").searchParams.get("mode"),
      ).toBe("complete"),
    );
  });

  it("disables Sync Now while a sync runs", async () => {
    stubApi({
      ...pullRoutes,
      "GET integration-status": ok([
        { integration: "portal", lastRun: { ...runningRun, trigger: "schedule" }, feeds: [] },
      ]),
    });
    renderRoute("/integrations/portal");

    expect(await screen.findByLabelText("Running")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sync Now" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "More Sync Options" })).toBeDisabled();
  });

  it("shows a running sync with its counts so far, and how long only once it ends", async () => {
    stubApi({
      ...pullRoutes,
      "GET integrations/portal/runs": ok({
        total: 2,
        runs: [{ ...runningRun, id: 3, received: 500, created: 500 }, syncRun],
      }),
    });
    renderRoute("/integrations/portal?tab=runs");

    const running = (await screen.findByLabelText("Running")).closest("tr") as HTMLElement;
    expect(within(running).getAllByText("500")).toHaveLength(2);
    expect(within(running).queryByText(/\d+(ms|s)$/)).not.toBeInTheDocument();
    const finished = screen.getByLabelText("Succeeded").closest("tr") as HTMLElement;
    expect(within(finished).getByText("1s")).toBeInTheDocument();
  });

  it("tests the connection and shows why it failed", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...pullRoutes,
      "POST integrations/portal/check": ok({
        version: "published",
        passed: false,
        error: "GET https://example.com: 401 Unauthorized",
      }),
    });
    renderRoute("/integrations/portal");

    await user.click(await screen.findByRole("button", { name: "Test Connection" }));
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(await within(about).findByText("Failed")).toBeInTheDocument();
    expect(
      within(about).getByText("GET https://example.com: 401 Unauthorized"),
    ).toBeInTheDocument();
    expect(requests["POST integrations/portal/check"]).toBeDefined();
  });

  it("shows a passed connection test", async () => {
    const user = userEvent.setup();
    stubApi({
      ...pullRoutes,
      "POST integrations/portal/check": ok({ version: "published", passed: true }),
    });
    renderRoute("/integrations/portal");

    await user.click(await screen.findByRole("button", { name: "Test Connection" }));
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(await within(about).findByText("Passed")).toBeInTheDocument();
  });

  it("explains a run that failed to fetch some resources", async () => {
    const user = userEvent.setup();
    stubApi({
      ...pullRoutes,
      "GET integrations/portal/runs": ok({
        total: 1,
        runs: [{ ...syncRun, trigger: "schedule", error: "users: 503 Service Unavailable" }],
      }),
    });
    renderRoute("/integrations/portal?tab=runs");

    expect(await screen.findByText("Incremental")).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "View problems" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(/Some or all resources couldn't be fetched/),
    ).toBeInTheDocument();
    expect(within(dialog).getByText("users: 503 Service Unavailable")).toBeInTheDocument();
  });

  it("offers pushing and syncing together when the config does both", async () => {
    const both = {
      ...portal,
      published: portal.published && {
        ...portal.published,
        spec: { ...portal.published.spec, receives: true },
      },
    };
    stubApi({
      ...pullRoutes,
      "GET integrations/portal": ok(both),
      "GET integrations/portal/tokens": ok([]),
    });
    renderRoute("/integrations/portal");

    expect(await screen.findByRole("button", { name: "Sync Now" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Push" })).toBeInTheDocument();
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(within(about).getByText("Pull")).toBeInTheDocument();
    expect(within(about).getByText("Push")).toBeInTheDocument();
  });
});
