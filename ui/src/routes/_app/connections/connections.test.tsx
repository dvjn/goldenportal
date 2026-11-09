import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Connection } from "@/features/connections";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const inventory: Connection = {
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
  "GET connections/inventory": ok(inventory),
  "GET connections/inventory/versions": ok([]),
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
  "GET connections/inventory/tokens": ok([]),
  "GET connection-status": ok([
    {
      connection: "inventory",
      lastRun: null,
      feeds: [{ blueprint: "service", title: "Service", entities: 12 }],
      sources: [
        { resource: "services", blueprint: "service", title: "Service", mode: "reconcile" },
      ],
    },
  ]),
  "GET connections/inventory/runs": ok({
    total: 1,
    items: [
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

const portal: Connection = {
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
  "GET connections/portal": ok(portal),
  "GET connections/portal/versions": ok([]),
  "GET connections/portal/runs": ok({ items: [], total: 0 }),
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
  "POST connections/portal/sync": ok(runningRun),
};

describe("connections", () => {
  it("says when no blueprint reads it", async () => {
    stubApi({
      ...routes,
      "GET connection-status": ok([
        { connection: "inventory", lastRun: null, feeds: [], sources: [] },
      ]),
    });
    renderRoute("/connections/inventory");

    const reads = within(
      (await screen.findByText("Reads")).closest("[data-slot=card]") as HTMLElement,
    );
    expect(reads.getByText("Reads Nothing")).toBeInTheDocument();
    expect(reads.getByText("No blueprint's sources read inventory yet.")).toBeInTheDocument();
  });

  it("shows the connector and reads, with runs and pushing in their own tabs", async () => {
    const user = userEvent.setup();
    stubApi(routes);
    renderRoute("/connections/inventory");

    expect(await screen.findByText("HTTP Connector")).toBeInTheDocument();
    // Its config only takes pushes, though the connector could pull or check too.
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(within(about).getByText("Push")).toBeInTheDocument();
    expect(within(about).queryByText("Pull")).not.toBeInTheDocument();
    expect(within(about).queryByText("Syncs Every")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Test Connection" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sync Now" })).not.toBeInTheDocument();
    expect(screen.getByText("Never run")).toBeInTheDocument();

    const reads = (await screen.findByText("Reads")).closest("[data-slot=card]") as HTMLElement;
    expect(within(reads).getByText("services")).toBeInTheDocument();
    expect(within(reads).getByRole("link", { name: "Service" })).toHaveAttribute(
      "href",
      "/blueprints/service",
    );
    expect(within(reads).getByText("Reconcile")).toBeInTheDocument();
    expect(within(reads).getByTitle("12 entities")).toHaveTextContent("12");

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
    renderRoute("/connections/inventory?tab=runs");

    await screen.findByLabelText("Succeeded with Warnings");
    expect(screen.getByText("Showing 1 to 1 of 1")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Outcome" }));
    await user.click(await screen.findByRole("option", { name: /Failed/ }));

    await waitFor(() =>
      expect(
        new URL(requests["GET connections/inventory/runs"]?.url ?? "").searchParams.get("outcome"),
      ).toBe("failed"),
    );
  });

  it("links a run an action made to that action run", async () => {
    stubApi({
      ...routes,
      "GET connections/inventory/runs": ok({
        total: 1,
        items: [
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
    renderRoute("/connections/inventory?tab=runs");

    expect(await screen.findByRole("link", { name: "Action · Open incident" })).toHaveAttribute(
      "href",
      "/runs/run-1",
    );
  });

  it("says from when a sync read what changed", async () => {
    stubApi({
      ...routes,
      "GET connections/inventory/runs": ok({
        total: 1,
        items: [
          {
            ...syncRun,
            readSince: {
              pipelines: "2026-10-24T09:00:00Z",
              merge_requests: "2026-10-24T08:00:00Z",
            },
          },
        ],
      }),
    });
    renderRoute("/connections/inventory?tab=runs");

    const mode = (await screen.findByText(/^Read from/)).parentElement as HTMLElement;
    expect(mode).toHaveTextContent("Incremental");
    expect(mode.title).toMatch(/^merge_requests from .*\npipelines from /);
  });

  it("lists connections with how their last run went", async () => {
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
      "GET connections": ok([
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
      "GET connection-status": ok([
        { connection: "inventory", lastRun: run, feeds: [], sources: [] },
        { connection: "fresh", lastRun: null, feeds: [], sources: [] },
      ]),
    });
    renderRoute("/connections");

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
      "POST connections/inventory/tokens": ok({
        id: "t1",
        name: "CI",
        prefix: "gpi_abcdefgh",
        createdBy: "ada@example.com",
        createdAt: "2026-10-24T10:00:00Z",
        lastUsedAt: null,
        secret: "gpi_abcdefghsecret",
      }),
    });
    renderRoute("/connections/inventory?tab=push");

    await user.click(await screen.findByRole("button", { name: "New Token" }));
    await user.type(screen.getByLabelText("Name"), "CI");
    await user.click(screen.getByRole("button", { name: "Create Token" }));

    expect(await screen.findByLabelText("New ingestion token")).toHaveValue("gpi_abcdefghsecret");
    expect(await requests["POST connections/inventory/tokens"].json()).toEqual({ name: "CI" });
  });

  it("syncs a connection that pulls instead of taking pushes", async () => {
    const user = userEvent.setup();
    const requests = stubApi(pullRoutes);
    renderRoute("/connections/portal");

    await user.click(await screen.findByRole("button", { name: "Sync Now" }));
    expect(await screen.findByText("Started the sync")).toBeInTheDocument();
    // Without a mode, the schedule decides.
    expect(new URL(requests["POST connections/portal/sync"]?.url ?? "").search).toBe("");
    // It pulls every minute, completely every day, so there's no Push tab.
    expect(screen.getByText("Syncs Every").nextElementSibling).toHaveTextContent("1m");
    expect(screen.getByText("Complete Every").nextElementSibling).toHaveTextContent("1d");
    expect(screen.queryByRole("tab", { name: "Push" })).not.toBeInTheDocument();
  });

  it("runs a complete sync from the Sync Now menu", async () => {
    const user = userEvent.setup();
    const requests = stubApi(pullRoutes);
    renderRoute("/connections/portal");

    await user.click(await screen.findByRole("button", { name: "More Sync Options" }));
    await user.click(await screen.findByRole("menuitem", { name: "Complete Sync" }));
    await waitFor(() =>
      expect(
        new URL(requests["POST connections/portal/sync"]?.url ?? "").searchParams.get("mode"),
      ).toBe("complete"),
    );
  });

  it("disables Sync Now while a sync runs", async () => {
    stubApi({
      ...pullRoutes,
      "GET connection-status": ok([
        {
          connection: "portal",
          lastRun: { ...runningRun, trigger: "schedule" },
          feeds: [],
          sources: [],
        },
      ]),
    });
    renderRoute("/connections/portal");

    expect(await screen.findByLabelText("Running")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sync Now" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "More Sync Options" })).toBeDisabled();
  });

  it("shows a running sync with its counts so far, and how long only once it ends", async () => {
    stubApi({
      ...pullRoutes,
      "GET connections/portal/runs": ok({
        total: 2,
        items: [{ ...runningRun, id: 3, received: 500, created: 500 }, syncRun],
      }),
    });
    renderRoute("/connections/portal?tab=runs");

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
      "POST connections/portal/check": ok({
        version: "published",
        passed: false,
        error: "GET https://example.com: 401 Unauthorized",
      }),
    });
    renderRoute("/connections/portal");

    await user.click(await screen.findByRole("button", { name: "Test Connection" }));
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(await within(about).findByText("Failed")).toBeInTheDocument();
    expect(
      within(about).getByText("GET https://example.com: 401 Unauthorized"),
    ).toBeInTheDocument();
    expect(requests["POST connections/portal/check"]).toBeDefined();
  });

  it("shows a passed connection test", async () => {
    const user = userEvent.setup();
    stubApi({
      ...pullRoutes,
      "POST connections/portal/check": ok({ version: "published", passed: true }),
    });
    renderRoute("/connections/portal");

    await user.click(await screen.findByRole("button", { name: "Test Connection" }));
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(await within(about).findByText("Passed")).toBeInTheDocument();
  });

  it("explains a run that failed to fetch some resources", async () => {
    const user = userEvent.setup();
    stubApi({
      ...pullRoutes,
      "GET connections/portal/runs": ok({
        total: 1,
        items: [{ ...syncRun, trigger: "schedule", error: "users: 503 Service Unavailable" }],
      }),
    });
    renderRoute("/connections/portal?tab=runs");

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
      "GET connections/portal": ok(both),
      "GET connections/portal/tokens": ok([]),
    });
    renderRoute("/connections/portal");

    expect(await screen.findByRole("button", { name: "Sync Now" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Push" })).toBeInTheDocument();
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(within(about).getByText("Pull")).toBeInTheDocument();
    expect(within(about).getByText("Push")).toBeInTheDocument();
  });
});
