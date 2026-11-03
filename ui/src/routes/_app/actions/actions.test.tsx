import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Action, ActionSpec, Available, Run } from "@/features/actions";
import type { Blueprint } from "@/features/blueprints";
import type { EntityDetail } from "@/features/catalog";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const spec: ActionSpec = {
  identifier: "restart",
  title: "Restart",
  description: "Restarts the service in one environment.",
  icon: "rotate-cw",
  color: "amber",
  blueprint: "service",
  run: { roles: ["builder"], filter: { property: "owner.members", op: "contains", value: "$me" } },
  inputs: [
    {
      identifier: "environment",
      title: "Environment",
      type: "string",
      enum: ["production", "staging"],
      required: true,
    },
    {
      identifier: "reason",
      title: "Reason",
      description: "Why it needs a restart.",
      type: "string",
      minLength: 5,
      required: true,
    },
  ],
  steps: [
    {
      id: "restart",
      title: "Restart the service",
      type: "http",
      timeout: "30s",
      retry: { attempts: 2, backoff: "3s" },
      http: {
        method: "POST",
        url: "https://ops.example.com/services/{{ $entity }}/restart",
        output: [{ name: "job", value: ".body.id" }],
      },
    },
    {
      title: "Output",
      type: "output",
      timeout: "30s",
      output: { summary: "Restarted {{ $entity.title }}" },
    },
  ],
  outputs: [{ name: "job", value: "$step.restart.job" }],
};

const restart: Action = {
  identifier: "restart",
  title: "Restart",
  icon: "rotate-cw",
  color: "amber",
  published: {
    version: 1,
    source: "kind: Action\n",
    spec,
    publishedAt: "2026-10-24T10:00:00Z",
    publishedBy: "ada@example.com",
  },
  draft: null,
  createdAt: "2026-10-24T10:00:00Z",
};

const run: Run = {
  id: "r1",
  kind: "action",
  action: "restart",
  title: "Restart",
  version: 1,
  entity: { blueprint: "service", identifier: "ledger", title: "Ledger" },
  requester: {
    id: "1",
    email: "ada@example.com",
    name: "Ada Lovelace",
    role: "member",
    groups: [],
  },
  via: "web",
  inputs: { environment: "staging", reason: "stuck queue" },
  status: "succeeded",
  summary: "Restarted Ledger in staging",
  links: [],
  outputs: { job: "job-1" },
  createdAt: "2026-10-24T10:00:00Z",
  startedAt: "2026-10-24T10:00:00Z",
  finishedAt: "2026-10-24T10:00:02Z",
};

const builder = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "builder" }) };

describe("actions", () => {
  it("lists actions with what they run on and how many steps they take", async () => {
    stubApi({
      ...builder,
      "GET actions": ok([
        {
          identifier: "restart",
          title: "Restart",
          publishedVersion: 1,
          hasDraft: false,
          stats: { inputs: 2, steps: 4 },
          facts: { blueprint: "service" },
          updatedAt: "2026-10-24T10:00:00Z",
        },
      ]),
      "GET catalog": ok([{ identifier: "service", title: "Service", entities: 3 }]),
    });
    renderRoute("/actions");

    const row = (await screen.findByRole("link", { name: /Restart/ })).closest("tr") as HTMLElement;
    expect(await within(row).findByText("Service")).toBeInTheDocument();
    expect(within(row).getByText("4")).toBeInTheDocument();
  });

  it("shows the steps, inputs and who can run an action, and its runs by status", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...builder,
      "GET actions/restart": ok(restart),
      "GET actions/restart/versions": ok([]),
      "GET actions/restart/runs": ok({ runs: [run], total: 1 }),
      "GET catalog": ok([]),
    });
    renderRoute("/actions/restart");

    const steps = (await screen.findByText("Steps")).closest("[data-slot=card]") as HTMLElement;
    expect(within(steps).getByText("Restart the service")).toBeInTheDocument();
    expect(
      within(steps).getByText("https://ops.example.com/services/{{ $entity }}/restart"),
    ).toBeInTheDocument();
    const inputs = screen.getByText("Inputs").closest("[data-slot=card]") as HTMLElement;
    expect(within(inputs).getByText("production")).toBeInTheDocument();
    expect(within(inputs).getByText("≥ 5 chars")).toBeInTheDocument();
    const about = screen.getByText("About").closest("[data-slot=card]") as HTMLElement;
    expect(within(about).getByText("builder")).toBeInTheDocument();
    expect(within(about).getByText(/owner\.members/)).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "Runs" }));
    const table = await screen.findByRole("table");
    expect(await within(table).findByRole("link", { name: "Restart" })).toHaveAttribute(
      "href",
      "/runs/r1",
    );
    expect(screen.getByLabelText("Succeeded")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Status" }));
    await user.click(await screen.findByRole("option", { name: /Interrupted/ }));
    await waitFor(() =>
      expect(
        new URL(requests["GET actions/restart/runs"]?.url ?? "").searchParams.get("status"),
      ).toBe("interrupted"),
    );
  });

  it("describes who approves an approval step, how many and for how long", async () => {
    const withApproval: Action = {
      ...restart,
      published: restart.published && {
        ...restart.published,
        spec: {
          ...spec,
          steps: [
            spec.steps[0],
            {
              id: "approve",
              title: "Review the plan",
              type: "approval",
              timeout: "30s",
              approval: {
                approvers: { groups: ["platform-engineers"] },
                require: 2,
                timeout: "24h",
                message: "Apply {{ $step.plan.count }} changes?",
                review: [{ title: "Changes", json: "$step.plan.changes" }],
              },
            },
          ],
        },
      },
    };
    stubApi({
      ...builder,
      "GET actions/restart": ok(withApproval),
      "GET actions/restart/versions": ok([]),
      "GET catalog": ok([]),
    });
    renderRoute("/actions/restart");

    const step = (await screen.findByText("Review the plan")).closest("section") as HTMLElement;
    expect(within(step).getByText("platform-engineers")).toBeInTheDocument();
    expect(within(step).getByText("Require").nextElementSibling).toHaveTextContent("2");
    expect(within(step).getByText("Timeout").nextElementSibling).toHaveTextContent("24h");
    expect(within(step).getByText("$step.plan.changes")).toBeInTheDocument();
  });

  it("opens a new action from a template that names its blueprint", async () => {
    stubApi({
      ...builder,
      "POST actions/validate": async (request) => {
        const { source } = (await request.json()) as { source: string };
        expect(source).toContain("kind: Action");
        return Response.json({ data: { spec: spec, issues: [] } });
      },
      "GET catalog": ok([]),
    });
    renderRoute("/actions/new");

    const preview = (await screen.findByText("Preview")).closest("[data-slot=card]") as HTMLElement;
    expect(await within(preview).findByText("Restart the service")).toBeInTheDocument();
    expect(within(preview).getByText("Reason")).toBeInTheDocument();
  });
});

const serviceBlueprint: Blueprint = {
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

const restartAvailable: Available = {
  identifier: "restart",
  title: "Restart",
  icon: "rotate-cw",
  inputs: spec.inputs,
};

const scale: Available = {
  identifier: "scale",
  title: "Scale",
  inputs: [
    {
      identifier: "replicas",
      title: "Replicas",
      type: "integer",
      minimum: 1,
      maximum: 20,
      required: true,
    },
    { identifier: "team", title: "Team", type: "relation", relation: "team" },
    { identifier: "notify", title: "Notify", type: "boolean" },
  ],
};

const entityPage = {
  "GET config": acmeConfig,
  "GET me": ok({ ...ada, role: "member" }),
  "GET blueprints/service": ok(serviceBlueprint),
  "GET catalog/service/entities/ledger": ok(ledger),
  "GET catalog/service/scorecards": ok([]),
  "GET catalog/service/actions": ok([restartAvailable, scale]),
  "GET runs": ok({ runs: [run], total: 1 }),
  "GET runs/r1": ok({ ...run, steps: [] }),
  "GET actions/restart": ok(restart),
  "GET catalog/team": ok({
    entities: [{ blueprint: "team", identifier: "payments", title: "Payments" }],
    total: 1,
  }),
};

describe("running actions on an entity", () => {
  it("shows each field's problems from the server, then runs and opens the run", async () => {
    const user = userEvent.setup();
    let tries = 0;
    const requests = stubApi({
      ...entityPage,
      "POST actions/scale/runs": () => {
        tries++;
        if (tries === 1) {
          return Response.json(
            {
              error: {
                message: "check the inputs",
                problems: { replicas: ["must be at most 20"], "": ["there's no input x"] },
              },
            },
            { status: 422 },
          );
        }
        return Response.json({ data: { ...run, id: "r1" } });
      },
    });
    renderRoute("/catalog/service/ledger");

    await user.click(await screen.findByRole("button", { name: "Actions" }));
    await user.click(screen.getByRole("menuitem", { name: "Scale" }));

    const dialog = await screen.findByRole("dialog");
    // Nothing is picked for the user, not even for yes or no.
    expect(within(dialog).getByRole("radio", { name: "Yes" })).not.toBeChecked();
    expect(within(dialog).getByRole("radio", { name: "No" })).not.toBeChecked();
    expect(within(dialog).getByLabelText(/^Replicas/)).toHaveAttribute("placeholder", "1-20");
    await user.type(within(dialog).getByLabelText(/^Replicas/), "50");
    await user.click(within(dialog).getByRole("combobox", { name: "Team" }));
    await user.click(await screen.findByRole("option", { name: /Payments/ }));
    await user.click(within(dialog).getByRole("button", { name: "Run" }));

    expect(await within(dialog).findByText("must be at most 20")).toBeInTheDocument();
    expect(within(dialog).getByText("there's no input x")).toBeInTheDocument();
    const first = (await requests["POST actions/scale/runs"].json()) as {
      entity: string;
      inputs: Record<string, unknown>;
      idempotencyKey: string;
    };
    expect(first.entity).toBe("ledger");
    expect(first.inputs).toEqual({ replicas: 50, team: "payments" });

    await user.clear(within(dialog).getByLabelText(/^Replicas/));
    expect(within(dialog).queryByText("must be at most 20")).not.toBeInTheDocument();
    await user.type(within(dialog).getByLabelText(/^Replicas/), "5");
    await user.click(within(dialog).getByRole("button", { name: "Run" }));

    expect(await screen.findByRole("heading", { name: "Restart" })).toBeInTheDocument();
    const second = (await requests["POST actions/scale/runs"].json()) as {
      idempotencyKey: string;
    };
    expect(second.idempotencyKey).toBe(first.idempotencyKey);
  });

  it("runs as soon as the form is sent", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...entityPage,
      "POST actions/restart/runs": ok(run),
    });
    renderRoute("/catalog/service/ledger");

    await user.click(await screen.findByRole("button", { name: "Actions" }));
    await user.click(screen.getByRole("menuitem", { name: "Restart" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("combobox", { name: "Environment" }));
    await user.click(await screen.findByRole("option", { name: "staging" }));
    await user.type(within(dialog).getByLabelText(/^Reason/), "stuck queue");
    await user.click(within(dialog).getByRole("button", { name: "Run" }));

    expect(await screen.findByRole("heading", { name: "Restart" })).toBeInTheDocument();
    expect(await requests["POST actions/restart/runs"].json()).toMatchObject({
      entity: "ledger",
      inputs: { environment: "staging", reason: "stuck queue" },
    });
  });

  it("lists the runs on the entity in their own tab", async () => {
    const user = userEvent.setup();
    const requests = stubApi(entityPage);
    renderRoute("/catalog/service/ledger");

    await user.click(await screen.findByRole("tab", { name: /Runs/ }));
    expect(await screen.findByRole("link", { name: "Restart" })).toHaveAttribute(
      "href",
      "/runs/r1",
    );
    const url = new URL(requests["GET runs"].url);
    expect(url.searchParams.get("blueprint")).toBe("service");
    expect(url.searchParams.get("entity")).toBe("ledger");
    // The tab is the entity's, so its runs don't name it again.
    expect(screen.queryByRole("columnheader", { name: "Entity" })).not.toBeInTheDocument();
  });
});
