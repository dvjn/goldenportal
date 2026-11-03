import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Available, Run } from "@/features/actions";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

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
  links: [{ label: "Job", url: "https://ops.example.com/jobs/job-1" }],
  outputs: { job: "job-1" },
  createdAt: "2026-10-24T10:00:00Z",
  startedAt: "2026-10-24T10:00:00Z",
  finishedAt: "2026-10-24T10:00:02Z",
  steps: [
    {
      position: 0,
      step: "restart",
      type: "http",
      title: "Restart the service",
      status: "done",
      attempts: 2,
      output: { job: "job-1" },
      startedAt: "2026-10-24T10:00:00Z",
      finishedAt: "2026-10-24T10:00:01Z",
    },
    {
      position: 1,
      type: "output",
      title: "Output",
      status: "done",
      attempts: 1,
      startedAt: "2026-10-24T10:00:01Z",
      finishedAt: "2026-10-24T10:00:02Z",
    },
  ],
};

const restart: Available = {
  identifier: "restart",
  title: "Restart",
  inputs: [
    {
      identifier: "environment",
      title: "Environment",
      type: "string",
      enum: ["production", "staging"],
      required: true,
    },
    { identifier: "reason", title: "Reason", type: "string", required: true },
  ],
};

const member = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) };

const runPage = {
  ...member,
  "GET actions/restart": ok({
    identifier: "restart",
    title: "Restart",
    published: {
      version: 1,
      source: "",
      spec: { identifier: "restart", title: "Restart", inputs: restart.inputs },
      publishedAt: "2026-10-24T10:00:00Z",
      publishedBy: "",
    },
    draft: null,
    createdAt: "2026-10-24T10:00:00Z",
  }),
  "GET catalog/service/actions": ok([restart]),
};

describe("runs", () => {
  it("shows how each step went and what the run gave", async () => {
    stubApi({ ...runPage, "GET runs/r1": ok(run) });
    renderRoute("/runs/r1");

    expect(await screen.findByRole("heading", { name: "Restart" })).toBeInTheDocument();
    expect(screen.getAllByText("Succeeded").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "Ledger" })).toHaveAttribute(
      "href",
      "/catalog/service/ledger",
    );
    const steps = screen.getByRole("list", { name: "Steps" });
    const first = within(steps).getByRole("listitem", { name: "Restart the service: Done" });
    expect(within(first).getByText("2 attempts")).toBeInTheDocument();
    expect(within(first).getByText("1s")).toBeInTheDocument();
    expect(within(first).getByText(/"job": "job-1"/)).toBeInTheDocument();

    expect(screen.getByText("Restarted Ledger in staging")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Job" })).toHaveAttribute(
      "href",
      "https://ops.example.com/jobs/job-1",
    );
    const inputs = screen.getByText("Inputs").closest("[data-slot=card]") as HTMLElement;
    expect(await within(inputs).findByText("Environment")).toBeInTheDocument();
    expect(within(inputs).getByText("stuck queue")).toBeInTheDocument();
    // It's over, so there's nothing to cancel, retry or resolve.
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Resolve" })).not.toBeInTheDocument();
  });

  it("runs the action again with the run's inputs", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...runPage,
      "GET runs/r1": ok(run),
      "POST actions/restart/runs": ok({ ...run, id: "r2", status: "queued" }),
      "GET runs/r2": ok({ ...run, id: "r2", status: "queued" }),
    });
    const { router } = renderRoute("/runs/r1");

    await user.click(await screen.findByRole("button", { name: "Run Again" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("combobox", { name: "Environment" })).toHaveTextContent(
      "staging",
    );
    expect(within(dialog).getByLabelText(/^Reason/)).toHaveValue("stuck queue");
    await user.click(within(dialog).getByRole("button", { name: "Run" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/runs/r2"));
    expect(await requests["POST actions/restart/runs"].json()).toMatchObject({
      entity: "ledger",
      inputs: { environment: "staging", reason: "stuck queue" },
    });
  });

  it("cancels a run under way after asking", async () => {
    const user = userEvent.setup();
    const running: Run = {
      ...run,
      status: "running",
      finishedAt: null,
      summary: "",
      links: [],
      outputs: {},
      steps: [{ ...(run.steps?.[0] as NonNullable<Run["steps"]>[number]), status: "running" }],
    };
    const requests = stubApi({
      ...runPage,
      "GET runs/r1": ok(running),
      "POST runs/r1/cancel": ok({ ...running, status: "cancelled" }),
    });
    renderRoute("/runs/r1");

    await user.click(await screen.findByRole("button", { name: "Cancel" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Cancel Run" }),
    );

    expect(await screen.findByText("Cancelled the run")).toBeInTheDocument();
    expect(requests["POST runs/r1/cancel"]).toBeDefined();
  });

  it("resolves an interrupted run with what was checked", async () => {
    const user = userEvent.setup();
    const interrupted: Run = {
      ...run,
      status: "interrupted",
      error: "the portal stopped while Restart the service ran",
      steps: [{ ...(run.steps?.[0] as NonNullable<Run["steps"]>[number]), status: "interrupted" }],
    };
    const requests = stubApi({
      ...runPage,
      "GET runs/r1": ok(interrupted),
      "POST runs/r1/resolve": ok({ ...interrupted, status: "failed" }),
    });
    renderRoute("/runs/r1");

    expect(await screen.findByText(interrupted.error ?? "")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Resolve" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/"Restart the service"/)).toBeInTheDocument();
    // Saying it took effect is for builders.
    expect(
      within(dialog).queryByRole("button", { name: "It Took Effect" }),
    ).not.toBeInTheDocument();
    const failed = within(dialog).getByRole("button", { name: "It Failed" });
    expect(failed).toBeDisabled();
    await user.type(within(dialog).getByLabelText(/What did you check/), "job never started");
    await user.click(failed);

    await waitFor(() => expect(requests["POST runs/r1/resolve"]).toBeDefined());
    expect(await requests["POST runs/r1/resolve"].json()).toEqual({
      outcome: "failed",
      comment: "job never started",
    });
  });

  it("lists a member's own runs, filtered by status", async () => {
    const user = userEvent.setup();
    const requests = stubApi({ ...member, "GET runs": ok({ runs: [run], total: 1 }) });
    renderRoute("/runs");

    expect(await screen.findByRole("link", { name: "Restart" })).toHaveAttribute(
      "href",
      "/runs/r1",
    );
    expect(new URL(requests["GET runs"].url).searchParams.get("mine")).toBe("true");
    expect(screen.queryByRole("tab", { name: "Everyone" })).not.toBeInTheDocument();
    // They're all the member's own, so they don't name who asked.
    expect(screen.queryByRole("columnheader", { name: "Requested By" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("option", { name: "Status" }));
    await user.click(await screen.findByRole("option", { name: /Failed/ }));
    await waitFor(() =>
      expect(new URL(requests["GET runs"].url).searchParams.get("status")).toBe("failed"),
    );
  });

  it("lets builders see everyone's runs", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      "GET config": acmeConfig,
      "GET me": ok({ ...ada, role: "builder" }),
      "GET runs": ok({ runs: [{ ...run, via: "mcp" }], total: 1 }),
    });
    renderRoute("/runs");

    // Runs start on the builder's own, and removing that filter shows everyone's.
    await waitFor(() =>
      expect(new URL(requests["GET runs"].url).searchParams.get("mine")).toBe("true"),
    );
    await user.click(await screen.findByRole("button", { name: "Remove Requested By filter" }));
    await waitFor(() =>
      expect(new URL(requests["GET runs"].url).searchParams.get("mine")).toBeNull(),
    );
    expect(await screen.findByRole("columnheader", { name: "Requested By" })).toBeInTheDocument();
    expect(screen.getByLabelText("Through an agent")).toBeInTheDocument();
  });
});
