import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { adminApi, cara, scim, signedInAs, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("system settings", () => {
  it("shows the forbidden page to a member", async () => {
    const fetchMock = stubApi(signedInAs(cara));
    renderRoute("/settings");

    expect(
      await screen.findByRole("heading", { name: "You don't have access to this page." }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to home" })).toHaveAttribute("href", "/");
    expect(fetchMock.mock.calls.some(([request]) => request.url.endsWith("/settings"))).toBe(false);
  });

  it("opens the general page with the identity settings", async () => {
    stubApi(adminApi);
    renderRoute("/settings");

    expect(await screen.findByRole("heading", { name: "General" })).toBeInTheDocument();
    expect(
      screen.getByText("https://login.example.test/application/o/portal/"),
    ).toBeInTheDocument();
    expect(screen.getByText("Set")).toBeInTheDocument();
    expect(screen.getByText("10 h")).toBeInTheDocument();
    expect(screen.getByText("45 min")).toBeInTheDocument();
    expect(screen.getByText("https://portal.example.test/scim/v2")).toBeInTheDocument();
    expect(screen.queryByText("No push in the last 24 hours")).not.toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "System settings" });
    expect(within(nav).getByRole("link", { name: "General" })).toHaveAttribute(
      "href",
      "/settings/general",
    );
  });

  it("warns when the identity provider has not pushed for a day", async () => {
    stubApi({
      ...adminApi,
      "GET /api/v1/scim": () => Response.json({ data: { ...scim, last_push_at: undefined } }),
    });
    renderRoute("/settings/general");

    expect(await screen.findByText("No push in the last 24 hours")).toBeInTheDocument();
    expect(screen.getByText("No push yet")).toBeInTheDocument();
  });

  it("rotates the SCIM token after confirmation and shows it once", async () => {
    const user = userEvent.setup();
    const fetchMock = stubApi({
      ...adminApi,
      "POST /api/v1/scim/token": () =>
        Response.json(
          { data: { token: { id: "t-2", created_at: scim.last_push_at }, secret: "gpt_rotated" } },
          { status: 201 },
        ),
    });
    renderRoute("/settings/general");

    await user.click(await screen.findByRole("button", { name: "Rotate token" }));
    const confirm = await screen.findByRole("alertdialog");
    await user.click(within(confirm).getByRole("button", { name: "Rotate token" }));

    expect(await screen.findByRole("textbox", { name: "New SCIM token" })).toHaveValue(
      "gpt_rotated",
    );
    expect(fetchMock.mock.calls.some(([request]) => request.url.endsWith("/scim/token"))).toBe(
      true,
    );
  });
});
