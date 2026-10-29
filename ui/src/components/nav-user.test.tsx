import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { navigateAway } from "@/lib/navigation";
import { cara, signedIn, signedInAs, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";
import { avatarLetter } from "./nav-user";

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

async function openUserMenu(letterShown = "A") {
  const user = userEvent.setup();
  renderRoute("/");
  const trigger = await screen.findByRole("button", { name: /Open the user menu/ });
  expect(await within(trigger).findByText(letterShown)).toBeInTheDocument();
  await user.click(trigger);

  return user;
}

describe("avatarLetter", () => {
  it("takes the first letter of the name, or of the email", () => {
    expect(avatarLetter("alice Archer", "alice@example.test")).toBe("A");
    expect(avatarLetter("  ", "bob@example.test")).toBe("B");
  });
});

describe("NavUser", () => {
  it("shows the first letter in the top bar and the name and email in the menu", async () => {
    await openUserMenu();

    const label = await screen.findByText("alice@example.test");
    expect(label.parentElement).toHaveTextContent("Alice Archer");
  });

  it("gives an admin every settings link in order", async () => {
    await openUserMenu();

    await screen.findByRole("menuitem", { name: "Sign out" });
    const links = within(screen.getByRole("menu"))
      .getAllByRole("menuitem")
      .filter((item) => item.hasAttribute("href"))
      .map((item) => [item.textContent, item.getAttribute("href")]);
    expect(links).toEqual([
      ["Catalog setup", "/settings/catalog-setup"],
      ["Users and groups", "/settings/access"],
      ["Service tokens", "/settings/service-tokens"],
      ["Audit log", "/settings/audit"],
      ["System settings", "/settings/system"],
    ]);
  });

  it("shows a member only the catalog setup link", async () => {
    stubApi(signedInAs(cara));
    await openUserMenu("C");

    expect(await screen.findByRole("menuitem", { name: "Sign out" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Catalog setup" })).toHaveAttribute(
      "href",
      "/settings/catalog-setup",
    );
    expect(screen.queryByRole("menuitem", { name: "Users and groups" })).not.toBeInTheDocument();
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
