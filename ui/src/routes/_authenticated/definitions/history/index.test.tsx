import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { bob, signedInAs, stubApi } from "@/test/api";
import { historyApi, plan } from "@/test/definitions";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("definitions history", () => {
  it("lists the revisions newest first", async () => {
    stubApi({ ...signedInAs(bob), ...historyApi });
    renderRoute("/definitions/history");

    const newest = (await screen.findByRole("link", { name: "Revision 2" })).closest(
      "tr",
    ) as HTMLElement;
    expect(within(newest).getByText("Rolled back to 1")).toBeInTheDocument();
    expect(within(newest).getByText("1 added, 0 changed, 0 removed")).toBeInTheDocument();
    const oldest = screen.getByRole("link", { name: "Revision 1" }).closest("tr") as HTMLElement;
    expect(within(oldest).getByText("add the catalog")).toBeInTheDocument();
    expect(within(oldest).getByText("alice@example.test")).toBeInTheDocument();
  });

  it("compares a revision with another and rolls back to it", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAs(bob),
      ...historyApi,
      "GET /api/v1/definitions/diff": (request) => {
        const url = new URL(request.url);
        expect(url.search).toBe("?from=2&to=1");
        return Response.json({
          data: {
            from: 2,
            to: 1,
            changes: [{ kind: "Integration", identifier: "ci", title: "CI", action: "remove" }],
          },
        });
      },
      "POST /api/v1/definitions/revisions/1/rollback": () =>
        Response.json({ data: { ...plan, rollback_of: 1 } }, { status: 201 }),
    });
    renderRoute("/definitions/history/1");

    expect(await screen.findByRole("heading", { name: "Revision 1" })).toBeInTheDocument();
    expect(screen.getByText("Service")).toBeInTheDocument();

    await user.click(screen.getByRole("combobox", { name: "Compare with" }));
    await user.click(await screen.findByRole("option", { name: "Revision 2" }));
    expect(await screen.findByText("CI")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Roll back to this revision" }));
    expect(
      await within(await screen.findByRole("dialog")).findByText("Roll back to revision 1"),
    ).toBeInTheDocument();
  });

  it("explains why the current revision cannot be rolled back to", async () => {
    stubApi({ ...signedInAs(bob), ...historyApi });
    renderRoute("/definitions/history/2");

    expect(
      await screen.findByRole("button", { name: "Roll back to this revision" }),
    ).toBeDisabled();
    expect(screen.getByText("Current")).toBeInTheDocument();
  });
});
