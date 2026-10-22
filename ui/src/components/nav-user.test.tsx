import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { navigateAway } from "@/lib/navigation";
import { cara, signedIn, signedInAs, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";
import { initials } from "./nav-user";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

vi.mock("@/lib/navigation", () => ({ navigateAway: vi.fn() }));

let fetchMock: ReturnType<typeof stubApi>;

beforeEach(() => {
  fetchMock = stubApi({
    ...signedIn,
    "POST /api/v1/auth/logout": () =>
      Response.json({ data: { redirect_to: "http://login.example.test/end" } }, { status: 201 }),
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.mocked(navigateAway).mockClear();
});

async function openUserMenu(initialsShown = "AA") {
  const user = userEvent.setup();
  renderRoute("/");
  const trigger = await screen.findByRole("button", { name: /Open the user menu/ });
  expect(await within(trigger).findByText(initialsShown)).toBeInTheDocument();
  await user.click(trigger);

  return user;
}

describe("initials", () => {
  it("takes the first letters of the first two names, or of the email", () => {
    expect(initials("Alice Archer", "alice@example.test")).toBe("AA");
    expect(initials("Cher", "cher@example.test")).toBe("C");
    expect(initials("Mary Ann Lee", "mary@example.test")).toBe("MA");
    expect(initials("  ", "bob@example.test")).toBe("B");
  });
});

describe("NavUser", () => {
  it("shows the initials in the top bar and the name and email in the menu", async () => {
    await openUserMenu();

    const label = await screen.findByText("alice@example.test");
    expect(label.parentElement).toHaveTextContent("Alice Archer");
  });

  it("gives an admin the administration links", async () => {
    await openUserMenu();

    expect(await screen.findByText("Administration")).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "System settings" })).toHaveAttribute(
      "href",
      "/settings/general",
    );
    expect(screen.getByRole("menuitem", { name: "Access" })).toHaveAttribute(
      "href",
      "/settings/access",
    );
  });

  it("gives a modeler the definitions and integrations links", async () => {
    await openUserMenu();

    expect(await within(screen.getByRole("menu")).findByText("Catalog")).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Definitions" })).toHaveAttribute(
      "href",
      "/definitions",
    );
    expect(screen.getByRole("menuitem", { name: "Integrations" })).toHaveAttribute(
      "href",
      "/integrations",
    );
  });

  it("shows no administration links to a member", async () => {
    stubApi(signedInAs(cara));
    await openUserMenu("CC");

    expect(await screen.findByRole("menuitem", { name: "Sign out" })).toBeInTheDocument();
    expect(screen.queryByText("Administration")).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Definitions" })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Integrations" })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "System settings" })).not.toBeInTheDocument();
  });

  it("follows the system theme until a theme is chosen, then stores it", async () => {
    const user = await openUserMenu();
    expect(document.documentElement).toHaveClass("light");

    await user.click(await screen.findByRole("menuitem", { name: "Theme" }));
    expect(await screen.findByRole("menuitemradio", { name: "System" })).toBeChecked();
    screen.getByRole("menuitemradio", { name: "Light" }).focus();
    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("menuitemradio", { name: "Dark" })).toHaveFocus();
    await user.keyboard("{Enter}");

    expect(document.documentElement).toHaveClass("dark");
    expect(document.documentElement).not.toHaveClass("light");
    expect(localStorage.getItem("goldport-ui-theme")).toBe("dark");
  });

  it("signs out and follows the provider logout", async () => {
    const user = await openUserMenu();

    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));

    await waitFor(() => expect(navigateAway).toHaveBeenCalledWith("http://login.example.test/end"));
    const logout = fetchMock.mock.calls.find(([request]) => request.url.endsWith("/auth/logout"));
    expect(logout?.[0].method).toBe("POST");
  });
});
