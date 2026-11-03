import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Available, Offer } from "@/features/actions";
import type { Blueprint } from "@/features/blueprints";
import type { EntityDetail } from "@/features/catalog";
import type { HomeDashboard } from "@/features/dashboards";
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

const restart: Available = {
  identifier: "restart",
  title: "Restart",
  icon: "rotate-cw",
  destructive: true,
  inputs: [{ identifier: "reason", title: "Reason", type: "string", required: true }],
};

const scale: Available = { identifier: "scale", title: "Scale", destructive: false, inputs: [] };

// offer is an action as the Self-Service catalog lists it.
const offer = (action: Available): Offer => ({
  ...action,
  kind: "action",
  blueprint: "service",
  blueprintTitle: "Service",
});

const restartServices: Offer = {
  kind: "workflow",
  identifier: "restart-services",
  title: "Restart Services",
  destructive: false,
  inputs: [],
};

const operations: HomeDashboard = {
  identifier: "operations",
  title: "Operations",
  order: 1,
  inputs: [],
  defaults: {},
  widgets: [
    {
      identifier: "respond",
      type: "actions",
      title: "Respond",
      width: 4,
      actions: ["scale", "gone", "restart"],
      workflows: ["restart-services"],
    },
  ],
};

const dashboard = {
  ...member,
  "GET home": ok([operations]),
  "GET self-service": ok({
    groups: [{ title: "Operate", items: [offer(restart), offer(scale), restartServices] }],
  }),
  "GET catalog": ok([{ identifier: "service", title: "Service", entities: 2 }]),
  "GET catalog/service": ok({
    entities: [{ blueprint: "service", identifier: "ledger", title: "Ledger" }],
    total: 1,
  }),
};

describe("actions widget", () => {
  it("offers a dashboard's actions and then its workflows in order, marking destructive ones", async () => {
    stubApi(dashboard);
    renderRoute("/");

    const card = (await screen.findByRole("button", { name: "Scale" })).closest(
      "[data-slot=card]",
    ) as HTMLElement;
    const buttons = within(card).getAllByRole("button");
    expect(buttons.map((button) => button.textContent)).toEqual([
      "Scale",
      "RestartDestructive",
      "Restart Services",
    ]);
  });

  it("asks for the entity first, and shows why the server turned the run down", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...dashboard,
      "POST actions/scale/runs": fail(403, "you can't run Scale on Ledger"),
    });
    renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "Scale" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Run" }));
    expect(await within(dialog).findByText("Choose the service to run it on.")).toBeInTheDocument();
    expect(requests["POST actions/scale/runs"]).toBeUndefined();

    await user.click(within(dialog).getByRole("combobox", { name: "Service" }));
    await user.click(await screen.findByRole("option", { name: /Ledger/ }));
    await user.click(within(dialog).getByRole("button", { name: "Run" }));

    expect(await within(dialog).findByText("you can't run Scale on Ledger")).toBeInTheDocument();
    expect(await requests["POST actions/scale/runs"].json()).toMatchObject({ entity: "ledger" });
  });

  it("opens a workflow's form without asking for an entity", async () => {
    const user = userEvent.setup();
    stubApi(dashboard);
    renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "Restart Services" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("combobox", { name: "Service" })).not.toBeInTheDocument();
  });

  it("says so when there's nothing to run", async () => {
    stubApi({
      ...dashboard,
      "GET home": ok([
        {
          ...operations,
          widgets: [{ ...operations.widgets[0], actions: ["gone"], workflows: undefined }],
        },
      ]),
    });
    renderRoute("/");

    expect(await screen.findByText("Nothing to run")).toBeInTheDocument();
  });

  it("runs on the page's entity, leaving out what the viewer can't run there", async () => {
    const user = userEvent.setup();
    const service: Blueprint = {
      identifier: "service",
      title: "Service",
      published: {
        version: 1,
        source: "",
        spec: {
          identifier: "service",
          title: "Service",
          properties: [],
          entity: {
            tabs: [{ identifier: "incidents", title: "Incidents", widgets: operations.widgets }],
          },
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
      properties: {},
      related: {},
      integration: "registry",
      createdAt: "2026-10-24T10:00:00Z",
      updatedAt: "2026-10-24T10:00:00Z",
      incoming: [],
    };
    stubApi({
      ...member,
      "GET blueprints/service": ok(service),
      "GET catalog/service/entities/ledger": ok(ledger),
      "GET catalog/service/scorecards": ok([]),
      "GET catalog/service/actions": ok([restart]),
      "GET self-service": dashboard["GET self-service"],
      "GET runs": ok({ runs: [], total: 0 }),
    });
    renderRoute("/catalog/service/ledger?tab=incidents");

    const run = await screen.findByRole("button", { name: /^Restart(Destructive)?$/ });
    const card = run.closest("[data-slot=card]") as HTMLElement;
    expect(within(card).queryByRole("button", { name: "Scale" })).not.toBeInTheDocument();
    await user.click(run);
    const dialog = await screen.findByRole("dialog");
    // The page's entity is the one it runs on, so there's nothing to pick.
    expect(within(dialog).getByText("Ledger")).toBeInTheDocument();
    expect(within(dialog).queryByRole("combobox", { name: "Service" })).not.toBeInTheDocument();
    await waitFor(() => expect(within(dialog).getByLabelText(/^Reason/)).toBeInTheDocument());
  });
});
