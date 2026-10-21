import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { adminApi, cara, grants, settings, signedInAs, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function row(name: string) {
  const cell = screen.getByText(name, { selector: "td span" });
  return cell.closest("tr") as HTMLElement;
}

describe("access page", () => {
  it("lists the grants and blocks removing the last Admin grant", async () => {
    stubApi(adminApi);
    renderRoute("/settings/access");

    expect(await screen.findByRole("heading", { name: "Access" })).toBeInTheDocument();
    expect(screen.getByText("2 grants", { exact: false })).toBeInTheDocument();
    expect(within(row("platform-team")).getByText("Group, 1 member")).toBeInTheDocument();
    expect(within(row("platform-team")).getByRole("button", { name: "Remove" })).toBeDisabled();
    expect(within(row("payments-team")).getByRole("button", { name: "Remove" })).toBeEnabled();
    expect(screen.queryByText("Access relies on bootstrap admins")).not.toBeInTheDocument();
  });

  it("filters the grants by name and role", async () => {
    const user = userEvent.setup();
    stubApi(adminApi);
    renderRoute("/settings/access");

    await user.type(await screen.findByRole("textbox", { name: "Search grants" }), "pay");

    expect(screen.queryByText("platform-team", { selector: "td span" })).not.toBeInTheDocument();
    expect(screen.getByText("payments-team", { selector: "td span" })).toBeInTheDocument();
  });

  it("grants a role to a group picked from the directory", async () => {
    const user = userEvent.setup();
    let body: unknown;
    stubApi({
      ...adminApi,
      "PUT /api/v1/access/grants/group/g-4": async (request) => {
        body = await request.json();
        return Response.json({ data: { grants } });
      },
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("button", { name: "Grant access" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("combobox"));
    await user.click(await screen.findByRole("option", { name: /contractors/ }));
    await user.click(within(dialog).getByRole("radio", { name: /^Admin/ }));
    await user.click(within(dialog).getByRole("button", { name: "Grant access" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(body).toEqual({ role: "admin" });
  });

  it("keeps Admin on the last Admin grant when changing its role", async () => {
    const user = userEvent.setup();
    stubApi(adminApi);
    renderRoute("/settings/access");

    await user.click(
      within(await waitFor(() => row("platform-team"))).getByRole("button", {
        name: "Change role",
      }),
    );
    const dialog = await screen.findByRole("dialog");

    expect(within(dialog).getByRole("heading", { name: "Change role" })).toBeInTheDocument();
    expect(within(dialog).getByRole("radio", { name: /^Maintainer/ })).toBeDisabled();
    expect(within(dialog).getByRole("radio", { name: /^Admin/ })).toBeChecked();
  });

  it("explains the effective role of a user", async () => {
    stubApi(adminApi);
    renderRoute("/settings/access?user=u-2");

    expect(await screen.findByText("Through payments-team.")).toBeInTheDocument();
    expect(screen.getByText(/Groups: all-engineers, payments-team/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revoke sessions" })).toBeInTheDocument();
  });

  it("shows the bootstrap banner while no Admin grant exists", async () => {
    stubApi({
      ...adminApi,
      "GET /api/v1/settings": () =>
        Response.json({
          data: {
            ...settings,
            bootstrap: { admins: ["alice@example.test"], admin_granted: false },
          },
        }),
    });
    renderRoute("/settings/access");

    expect(await screen.findByText("Access relies on bootstrap admins")).toBeInTheDocument();
    expect(screen.getByText(/grants Admin to alice@example.test/)).toBeInTheDocument();
  });

  it("is forbidden to a member", async () => {
    stubApi(signedInAs(cara));
    renderRoute("/settings/access");

    expect(
      await screen.findByRole("heading", { name: "You don't have access to this page." }),
    ).toBeInTheDocument();
  });
});
