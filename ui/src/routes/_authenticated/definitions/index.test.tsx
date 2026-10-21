import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { discardAll } from "@/features/definitions";
import { bob, cara, signedInAs, stubApi } from "@/test/api";
import { definitionsApi, plan, teamYAML } from "@/test/definitions";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

vi.mock("@/features/definitions/lazy-yaml-editor", async () => {
  const { YamlEditor } = await import("@/test/yaml-editor");
  return { LazyYamlEditor: YamlEditor };
});

afterEach(() => {
  discardAll();
  vi.unstubAllGlobals();
});

async function editTeam() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: /Team/ }));
  fireEvent.change(screen.getByLabelText("YAML of file:///definitions/Blueprint/team.yaml"), {
    target: { value: teamYAML.replace("title: Email", "title: Team email") },
  });
  return user;
}

describe("definitions editor", () => {
  it("shows the forbidden page to a member", async () => {
    stubApi(signedInAs(cara));
    renderRoute("/definitions");

    expect(
      await screen.findByRole("heading", { name: "You don't have access to this page." }),
    ).toBeInTheDocument();
  });

  it("plans the changed documents and applies the plan", async () => {
    const requests: Record<string, unknown> = {};
    stubApi({
      ...signedInAs(bob),
      ...definitionsApi,
      "POST /api/v1/definitions/plan": async (request) => {
        requests.plan = await request.json();
        return Response.json({ data: plan }, { status: 201 });
      },
      "POST /api/v1/definitions/apply": async (request) => {
        requests.apply = await request.json();
        return Response.json({ data: { number: 2 } }, { status: 201 });
      },
    });
    renderRoute("/definitions");

    const user = await editTeam();
    await user.click(screen.getByRole("button", { name: /CI/ }));
    await user.click(screen.getByRole("button", { name: "Remove" }));
    expect(screen.getByText("2 documents changed")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Plan" }));

    const dialog = await screen.findByRole("dialog");
    expect(requests.plan).toEqual({
      source: teamYAML.replace("title: Email", "title: Team email"),
      partial: true,
      remove: [
        { kind: "Blueprint", identifier: "team" },
        { kind: "Integration", identifier: "ci" },
      ],
    });
    expect(
      within(dialog).getByText(
        "0 to add, 1 to change, 1 to remove. Nothing changes until you apply.",
      ),
    ).toBeInTheDocument();
    expect(within(dialog).getByText("properties.email.title")).toBeInTheDocument();
    expect(within(dialog).getByText("Team email")).toBeInTheDocument();
    expect(
      within(dialog).getByText("Deletes 12 entities that the ci integration owns"),
    ).toBeInTheDocument();
    expect(within(dialog).getByText("Ledger and 11 more")).toBeInTheDocument();
    expect(
      within(dialog).getByText("Drops the service.owner links of 1 entity"),
    ).toBeInTheDocument();

    await user.type(within(dialog).getByLabelText("Message (optional)"), "rename the email");
    await user.click(within(dialog).getByRole("button", { name: "Apply" }));

    expect(await screen.findByText("Applied revision 2.")).toBeInTheDocument();
    expect(requests.apply).toEqual({ plan_id: "plan-1", message: "rename the email" });
    expect(screen.queryByText(/documents? changed/)).not.toBeInTheDocument();
  });

  it("marks each problem at its line in the draft it came from", async () => {
    stubApi({
      ...signedInAs(bob),
      ...definitionsApi,
      "POST /api/v1/definitions/plan": () =>
        Response.json(
          {
            error: {
              message: "the definitions have 2 problems, fix them and plan again",
              problems: [
                {
                  document: 1,
                  kind: "Blueprint",
                  identifier: "team",
                  path: "properties.email.type",
                  line: 7,
                  column: 11,
                  message: "value must be one of 'string', 'number'",
                },
                {
                  kind: "Blueprint",
                  identifier: "service",
                  path: "relations.owner.target",
                  message: 'no blueprint has the identifier "team"',
                },
              ],
            },
          },
          { status: 422 },
        ),
    });
    renderRoute("/definitions");

    const user = await editTeam();
    await user.click(screen.getByRole("button", { name: "Plan" }));

    expect(
      await screen.findByText("the definitions have 2 problems, fix them and plan again"),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("list", { name: "Editor markers" })).getByText(
        "Line 7: properties.email.type: value must be one of 'string', 'number'",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("Blueprint service, relations.owner.target")).toBeInTheDocument();
  });

  it("uploads YAML to replace every definition", async () => {
    const user = userEvent.setup();
    let body: unknown;
    stubApi({
      ...signedInAs(bob),
      ...definitionsApi,
      "POST /api/v1/definitions/plan": async (request) => {
        body = await request.json();
        return Response.json({ data: plan }, { status: 201 });
      },
    });
    renderRoute("/definitions");

    await user.click(await screen.findByRole("button", { name: "Upload YAML" }));
    fireEvent.change(screen.getByLabelText("YAML"), { target: { value: teamYAML } });
    await user.click(screen.getByRole("radio", { name: /Replace all definitions/ }));
    await user.click(screen.getByRole("button", { name: "Plan" }));

    expect(await screen.findByText("properties.email.title")).toBeInTheDocument();
    expect(body).toEqual({ source: teamYAML, partial: false });
  });
});
