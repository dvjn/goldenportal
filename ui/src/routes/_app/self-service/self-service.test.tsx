import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { ActionSpec, Offer, Run, SelfServiceCatalog, Workflow } from "@/features/actions";
import { acmeConfig, ada, fail, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const member = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) };
const builder = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "builder" }) };

const restartServices: Offer = {
  kind: "workflow",
  identifier: "restart-services",
  title: "Restart services",
  description: "Restarts several services.",
  inputs: [
    {
      identifier: "services",
      title: "Services",
      type: "relation",
      relation: "service",
      many: true,
      required: true,
    },
  ],
};

const register: Offer = {
  kind: "workflow",
  identifier: "register-service",
  title: "Register a service",
  inputs: [{ identifier: "name", title: "Name", type: "string", required: true }],
};

const scale: Offer = {
  kind: "action",
  identifier: "scale",
  title: "Scale",
  blueprint: "service",
  blueprintTitle: "Service",
  inputs: [],
};

const catalog: SelfServiceCatalog = {
  groups: [
    { title: "Create", items: [register] },
    { title: "Operate", items: [restartServices, scale] },
  ],
};

const services = {
  entities: [
    { blueprint: "service", identifier: "ledger", title: "Ledger" },
    { blueprint: "service", identifier: "search-api", title: "Search API" },
  ],
  total: 2,
};

const catalogPage = {
  ...member,
  "GET self-service": ok(catalog),
  "GET catalog": ok([{ identifier: "service", title: "Service", entities: 2 }]),
  "GET catalog/service": ok(services),
};

const parent: Run = {
  id: "p1",
  kind: "workflow",
  action: "restart-services",
  title: "Restart services",
  version: 1,
  entity: { blueprint: "", identifier: "", title: "" },
  requester: {
    id: "1",
    email: "ada@example.com",
    name: "Ada Lovelace",
    role: "member",
    groups: [],
  },
  via: "web",
  inputs: { services: ["ledger", "search-api"] },
  status: "waiting",
  summary: "",
  links: [],
  outputs: {},
  createdAt: "2026-10-24T10:00:00Z",
  startedAt: "2026-10-24T10:00:00Z",
  finishedAt: null,
  steps: [
    {
      position: 0,
      step: "restart",
      type: "action",
      title: "Restart each service",
      status: "waiting",
      attempts: 1,
      startedAt: "2026-10-24T10:00:00Z",
      finishedAt: null,
    },
  ],
};

const child = (id: string, entity: string, status: Run["status"], error?: string): Run => ({
  ...parent,
  id,
  kind: "action",
  action: "restart",
  title: "Restart",
  entity: { blueprint: "service", identifier: entity, title: entity },
  status,
  error,
  parent: "p1",
  steps: undefined,
});

describe("self-service", () => {
  it("lists workflows and actions by group, and searches them", async () => {
    const user = userEvent.setup();
    stubApi(catalogPage);
    renderRoute("/self-service");

    expect(await screen.findByRole("heading", { name: "Self-Service" })).toBeInTheDocument();
    // Self-Service sits beside the catalog, and runs and approvals are activity.
    const menuLinks = screen
      .getAllByRole("link", { name: "Self-Service" })
      .filter((link) => link.dataset.sidebar === "menu-button");
    expect(menuLinks.map((link) => link.getAttribute("href"))).toEqual(["/self-service"]);
    const activity = (
      await screen.findByText("Activity", { selector: "[data-sidebar=group-label]" })
    ).parentElement as HTMLElement;
    expect(
      within(activity)
        .getAllByRole("link")
        .map((link) => [link.textContent, link.getAttribute("href")]),
    ).toEqual([
      ["Runs", "/runs"],
      ["Approvals", "/approvals"],
    ]);
    const create = await screen.findByRole("region", { name: "Create" });
    expect(within(create).getByRole("button", { name: "Register a service" })).toBeInTheDocument();
    const operate = screen.getByRole("region", { name: "Operate" });
    // Only an action says it runs on an entity.
    expect(within(operate).getByText("on a Service")).toBeInTheDocument();
    expect(within(operate).getAllByText(/^on an? /)).toHaveLength(1);
    expect(within(operate).getByRole("button", { name: "Scale" })).toBeInTheDocument();

    await user.type(screen.getByLabelText("Search self-service"), "several");
    expect(screen.queryByRole("region", { name: "Create" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Restart services" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Scale" })).not.toBeInTheDocument();
  });

  it("runs a workflow with many entities picked, on no entity, then opens the run", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...catalogPage,
      "POST workflows/restart-services/runs": ok(parent),
      "GET runs/p1": ok(parent),
      "GET workflows/restart-services": fail(404, "not found"),
      "GET runs": ok({ runs: [], total: 0 }),
    });
    const { router } = renderRoute("/self-service");

    await user.click(await screen.findByRole("button", { name: "Restart services" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("combobox", { name: "Service" })).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("combobox", { name: "Services" }));
    await user.click(await screen.findByRole("option", { name: /Ledger/ }));
    await user.click(await screen.findByRole("option", { name: /Search API/ }));
    await user.keyboard("{Escape}");
    await user.click(within(dialog).getByRole("button", { name: "Run" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/runs/p1"));
    expect(await requests["POST workflows/restart-services/runs"].json()).toMatchObject({
      inputs: { services: ["ledger", "search-api"] },
    });
  });

  it("opens a workflow's form from a link, as search results do", async () => {
    stubApi(catalogPage);
    renderRoute("/self-service?workflow=register-service");

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Register a service")).toBeInTheDocument();
    expect(within(dialog).getByLabelText(/^Name/)).toBeInTheDocument();
  });

  it("leaves the entity blank for a workflow in the runs table", async () => {
    stubApi({ ...member, "GET runs": ok({ runs: [{ ...parent, steps: undefined }], total: 1 }) });
    renderRoute("/runs?who=all");

    const row = (await screen.findByRole("link", { name: "Restart services" })).closest(
      "tr",
    ) as HTMLElement;
    expect(within(row).queryAllByRole("link")).toHaveLength(1);
  });

  it("opens an action's form at the entity picker", async () => {
    const user = userEvent.setup();
    stubApi(catalogPage);
    renderRoute("/self-service");

    await user.click(await screen.findByRole("button", { name: "Scale" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("combobox", { name: "Service" })).toBeInTheDocument();
  });

  it("shows the runs an action step started while it waits, then its total", async () => {
    let current = parent;
    stubApi({
      ...member,
      "GET runs/p1": () => Response.json({ data: current }),
      "GET workflows/restart-services": fail(404, "not found"),
      "GET self-service": ok(catalog),
      "GET runs": ok({
        runs: [child("c1", "ledger", "running"), child("c2", "search-api", "succeeded")],
        total: 2,
      }),
    });
    const { queryClient } = renderRoute("/runs/p1");

    const step = await screen.findByRole("listitem", {
      name: "Restart each service: Waiting for its runs",
    });
    expect(screen.getAllByText("Waiting").length).toBeGreaterThan(0);
    // It ran on no entity, so the header names none.
    expect(
      screen
        .getAllByRole("link")
        .some((link) => link.getAttribute("href")?.startsWith("/catalog/")),
    ).toBe(false);
    expect(await within(step).findByRole("link", { name: "search-api" })).toHaveAttribute(
      "href",
      "/runs/c2",
    );
    expect(step).toHaveTextContent("1 of 2 succeeded");

    current = {
      ...parent,
      status: "failed",
      finishedAt: "2026-10-24T10:01:00Z",
      steps: [
        {
          ...(parent.steps?.[0] as NonNullable<Run["steps"]>[number]),
          status: "failed",
          output: {
            runs: [
              { id: "c2", entity: "search-api", title: "Search API", status: "succeeded" },
              { entity: "ledger", status: "", error: "you can't run Restart on Ledger" },
            ],
            succeeded: 1,
            failed: 1,
          },
        },
      ],
    };
    await queryClient.invalidateQueries({ queryKey: ["runs", "p1"] });
    expect(await screen.findByText("you can't run Restart on Ledger")).toBeInTheDocument();
    const done = screen.getByRole("listitem", { name: "Restart each service: Failed" });
    expect(done).toHaveTextContent("1 of 2 succeeded");
    expect(within(done).getByRole("link", { name: "Search API" })).toHaveAttribute(
      "href",
      "/runs/c2",
    );
  });

  it("links a run an action step started back to its parent", async () => {
    stubApi({
      ...member,
      "GET runs/c1": ok({ ...child("c1", "ledger", "succeeded"), steps: [] }),
      "GET actions/restart": fail(404, "not found"),
      "GET catalog/service/actions": ok([]),
    });
    renderRoute("/runs/c1");

    expect(await screen.findByRole("link", { name: "Run that started it" })).toHaveAttribute(
      "href",
      "/runs/p1",
    );
  });
});

const spec: ActionSpec = {
  kind: "Workflow",
  identifier: "restart-services",
  title: "Restart services",
  blueprint: "",
  group: "Operations",
  run: { roles: ["member"] },
  inputs: restartServices.inputs,
  steps: [
    {
      id: "restart",
      title: "Restart each service",
      type: "action",
      timeout: "30s",
      action: {
        name: "restart",
        entities: "$input.services",
        inputs: { reason: "$input.reason" },
        maxFailures: 0,
      },
    },
  ],
  outputs: [],
};

const doc: Workflow = {
  identifier: "restart-services",
  title: "Restart services",
  published: { version: 1, source: "", spec, publishedAt: "2026-10-24T10:00:00Z", publishedBy: "" },
  draft: null,
  createdAt: "2026-10-24T10:00:00Z",
};

describe("workflows", () => {
  it("lists workflows by group for builders", async () => {
    stubApi({
      ...builder,
      "GET workflows": ok([
        {
          identifier: "restart-services",
          title: "Restart services",
          publishedVersion: 1,
          hasDraft: false,
          stats: { inputs: 3, steps: 2 },
          facts: { group: "Operations" },
          updatedAt: "2026-10-24T10:00:00Z",
        },
      ]),
    });
    renderRoute("/workflows");

    const row = (await screen.findByRole("link", { name: /Restart services/ })).closest(
      "tr",
    ) as HTMLElement;
    expect(row).toHaveAttribute("data-slot", "table-row");
    expect(within(row).getByText("Operations")).toBeInTheDocument();
  });

  it("describes its group and action steps, with its runs in a tab", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...builder,
      "GET workflows/restart-services": ok(doc),
      "GET workflows/restart-services/versions": ok([]),
      "GET workflows/restart-services/runs": ok({
        runs: [{ ...parent, steps: undefined }],
        total: 1,
      }),
    });
    renderRoute("/workflows/restart-services");

    const about = (await screen.findByText("About")).closest("[data-slot=card]") as HTMLElement;
    expect(within(about).getByText("Group").nextElementSibling).toHaveTextContent("Operations");
    const step = screen.getByText("Restart each service").closest("section") as HTMLElement;
    expect(within(step).getByRole("link", { name: "restart" })).toHaveAttribute(
      "href",
      "/actions/restart",
    );
    expect(within(step).getByText("$input.services")).toBeInTheDocument();
    expect(within(step).getByText("Max Failures").nextElementSibling).toHaveTextContent("0");

    await user.click(screen.getByRole("tab", { name: "Runs" }));
    const table = await screen.findByRole("table");
    expect(await within(table).findByRole("link", { name: "Restart services" })).toHaveAttribute(
      "href",
      "/runs/p1",
    );
    expect(requests["GET workflows/restart-services/runs"]).toBeDefined();
  });
});
