import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Run, RunStep } from "@/features/actions";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const changes = Array.from({ length: 4 }, (_, i) => ({ setting: `s${i}`, from: i, to: i + 1 }));

const review: RunStep = {
  position: 1,
  step: "approve",
  type: "approval",
  title: "Review the plan",
  status: "waiting",
  attempts: 1,
  detail: {
    require: 2,
    message: "Apply 4 changes to Ledger in production?",
    review: [
      {
        title: "Plan",
        markdown: "| Setting | Now |\n| --- | --- |\n| replicas | 2 |\n\n<script>alert(1)</script>",
      },
      { title: "Changes", json: changes },
    ],
  },
  decisions: [
    {
      email: "builder@example.com",
      name: "Dev Builder",
      decision: "approve",
      comment: "Fine by me",
      decidedAt: "2026-10-24T10:05:00Z",
    },
  ],
  startedAt: "2026-10-24T10:00:01Z",
  finishedAt: null,
  dueAt: "2099-10-25T10:00:01Z",
};

const waiting: Run = {
  id: "r1",
  kind: "action",
  action: "apply-config",
  title: "Apply config",
  version: 1,
  entity: { blueprint: "service", identifier: "ledger", title: "Ledger" },
  requester: {
    id: "9",
    email: "ada@example.com",
    name: "Ada Lovelace",
    role: "member",
    groups: [],
  },
  via: "web",
  inputs: {},
  status: "waiting",
  summary: "",
  links: [],
  outputs: {},
  createdAt: "2026-10-24T10:00:00Z",
  startedAt: "2026-10-24T10:00:00Z",
  finishedAt: null,
  canDecide: true,
  steps: [
    {
      position: 0,
      step: "plan",
      type: "http",
      title: "Plan the changes",
      status: "done",
      attempts: 1,
      startedAt: "2026-10-24T10:00:00Z",
      finishedAt: "2026-10-24T10:00:01Z",
    },
    review,
  ],
};

const grace = {
  "GET config": acmeConfig,
  "GET me": ok({ ...ada, id: "2", email: "grace@example.com", role: "member" }),
  "GET actions/apply-config": ok(null),
  "GET catalog/service/actions": ok([]),
};

describe("approvals", () => {
  it("shows approvers what to review and how many approved", async () => {
    const user = userEvent.setup();
    stubApi({ ...grace, "GET runs/r1": ok(waiting) });
    renderRoute("/runs/r1");

    const step = await screen.findByRole("listitem", {
      name: "Review the plan: Waiting for approval",
    });
    expect(within(step).getByText("Apply 4 changes to Ledger in production?")).toBeInTheDocument();
    expect(within(step).getByTitle("1 of 2 approvals needed")).toBeInTheDocument();
    expect(within(step).getByText(/^in /)).toBeInTheDocument();
    // The markdown's table shows; its HTML doesn't run.
    expect(within(step).getByRole("cell", { name: "replicas" })).toBeInTheDocument();
    expect(step.querySelector("script")).toBeNull();
    // Long JSON starts folded, with its length.
    const changesBlock = within(step).getByRole("button", { name: /Changes/ });
    expect(changesBlock).toHaveTextContent("22 lines");
    expect(within(step).queryByText(/"setting": "s3"/)).not.toBeInTheDocument();
    await user.click(changesBlock);
    expect(within(step).getByText(/"setting": "s3"/)).toBeInTheDocument();

    const decisions = within(step).getByRole("list", { name: "Decisions" });
    expect(within(decisions).getByLabelText("Approved")).toBeInTheDocument();
    expect(within(decisions).getByText("Fine by me")).toBeInTheDocument();
  });

  it("approves, and rejects only with a comment", async () => {
    const user = userEvent.setup();
    const rejected = {
      ...waiting,
      status: "rejected",
      canDecide: false,
      steps: [waiting.steps?.[0], { ...review, status: "failed" }],
    };
    let current: unknown = waiting;
    const requests = stubApi({
      ...grace,
      "GET runs/r1": () => Response.json({ data: current }),
      "POST runs/r1/decide": () => {
        current = rejected;
        return Response.json({ data: rejected });
      },
    });
    renderRoute("/runs/r1");

    const reject = await screen.findByRole("button", { name: "Reject" });
    expect(reject).toBeDisabled();
    expect(screen.getByRole("button", { name: "Approve" })).toBeEnabled();
    await user.type(screen.getByLabelText("Comment"), "Too many at once");
    await user.click(reject);

    await waitFor(() => expect(requests["POST runs/r1/decide"]).toBeDefined());
    expect(await requests["POST runs/r1/decide"].json()).toEqual({
      decision: "reject",
      comment: "Too many at once",
    });
    expect(await screen.findByText("Rejected the run")).toBeInTheDocument();
    expect(screen.getAllByLabelText("Rejected").length).toBeGreaterThan(0);
    // Once decided, the record stays but there's nothing more to decide.
    expect(screen.getByText("Apply 4 changes to Ledger in production?")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("doesn't offer whoever asked for the run a say", async () => {
    stubApi({
      ...grace,
      "GET me": ok({ ...ada, id: "9", role: "member" }),
      "GET runs/r1": ok({ ...waiting, canDecide: false }),
    });
    renderRoute("/runs/r1");

    expect(await screen.findByText("Apply 4 changes to Ledger in production?")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
    // It hasn't finished, so it can still be cancelled.
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
  });

  it("lists the runs waiting on me, counted in the sidebar", async () => {
    stubApi({ ...grace, "GET approvals": ok([waiting]) });
    renderRoute("/approvals");

    const row = (await screen.findByRole("link", { name: "Apply config" })).closest(
      "tr",
    ) as HTMLElement;
    expect(within(row).getByRole("link", { name: "Apply config" })).toHaveAttribute(
      "href",
      "/runs/r1",
    );
    expect(within(row).getByText("Ledger")).toBeInTheDocument();
    expect(within(row).getByText("Ada Lovelace")).toBeInTheDocument();
    expect(within(row).getByText("Apply 4 changes to Ledger in production?")).toBeInTheDocument();
    expect(row).toHaveTextContent("1 / 2");
    expect(screen.getByLabelText("1 waiting on you")).toHaveTextContent("1");
  });
});
