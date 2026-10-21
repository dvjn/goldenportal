import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { navigateAway } from "@/lib/navigation";
import { signedIn, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

vi.mock("@/lib/navigation", () => ({ navigateAway: vi.fn() }));

beforeEach(() => {
  stubApi(signedIn);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.mocked(navigateAway).mockClear();
});

describe("authenticated layout", () => {
  it("renders the shell for a signed-in user", async () => {
    renderRoute("/");

    expect(await screen.findByRole("link", { name: "Acme" })).toHaveAttribute("href", "/");
    expect(
      within(screen.getByRole("banner")).getByRole("button", { name: "Toggle theme" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "breadcrumb" })).not.toBeInTheDocument();
    expect(navigateAway).not.toHaveBeenCalled();
  });

  it("sends a signed-out visitor to sign in and back to the same page", async () => {
    stubApi({
      ...signedIn,
      "GET /api/v1/auth/me": () =>
        Response.json({ error: { message: "authentication required" } }, { status: 401 }),
    });
    const { container } = renderRoute("/?tab=owned");

    await vi.waitFor(() =>
      expect(navigateAway).toHaveBeenCalledWith("/api/v1/auth/login?return_to=%2F%3Ftab%3Downed"),
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("shows other failures in the error page", async () => {
    stubApi({
      ...signedIn,
      "GET /api/v1/auth/me": () =>
        Response.json({ error: { message: "could not check the session" } }, { status: 500 }),
    });
    renderRoute("/");

    expect(await screen.findByText("could not check the session")).toBeInTheDocument();
    expect(navigateAway).not.toHaveBeenCalled();
  });

  it("starts in dark mode and stores the chosen theme", async () => {
    const user = userEvent.setup();
    renderRoute("/");

    expect(document.documentElement).toHaveClass("dark");

    await user.click(await screen.findByRole("button", { name: "Toggle theme" }));
    await user.click(await screen.findByRole("menuitem", { name: "Light" }));

    expect(document.documentElement).toHaveClass("light");
    expect(document.documentElement).not.toHaveClass("dark");
    expect(localStorage.getItem("goldport-ui-theme")).toBe("light");
  });
});
