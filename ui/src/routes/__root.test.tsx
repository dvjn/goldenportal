import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { acmeConfig, ada, fail, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

beforeEach(() => {
  stubApi({ "GET config": acmeConfig, "GET me": ok(ada) });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("app shell", () => {
  it("renders the configured portal name and the navbar", async () => {
    renderRoute("/");

    expect(await screen.findByRole("link", { name: "Acme" })).toHaveAttribute("href", "/");
    expect(document.title).toBe("Home | Acme");
    expect(screen.getByRole("button", { name: "Open user menu" })).toHaveTextContent("AL");
    expect(document.querySelector("header img")).not.toBeInTheDocument();
  });

  it("shows the configured logo next to the portal name", async () => {
    stubApi({
      "GET config": ok({ name: "Acme", logoUrl: "https://example.com/logo.svg" }),
      "GET me": ok(ada),
    });
    renderRoute("/");

    const link = await screen.findByRole("link", { name: "Acme" });
    expect(link.querySelector("img")).toHaveAttribute("src", "https://example.com/logo.svg");
  });

  it("renders nothing until the config has loaded", async () => {
    stubApi({ "GET config": () => new Promise<Response>(() => {}), "GET me": ok(ada) });
    const { container } = renderRoute("/");

    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(container).toBeEmptyDOMElement();
  });

  it("shows an error with a retry when the config request fails", async () => {
    const user = userEvent.setup();
    stubApi({ "GET config": fail(500, "config unavailable"), "GET me": ok(ada) });
    renderRoute("/");

    expect(await screen.findByText("config unavailable")).toBeInTheDocument();

    stubApi({ "GET config": acmeConfig, "GET me": ok(ada) });
    await user.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByRole("link", { name: "Acme" })).toBeInTheDocument();
  });

  it("shows who is signed in and stores the chosen theme", async () => {
    const user = userEvent.setup();
    renderRoute("/");

    // The test environment reports a light system theme.
    expect(document.documentElement).toHaveClass("light");

    await user.click(await screen.findByRole("button", { name: "Open user menu" }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByText("Ada Lovelace")).toBeInTheDocument();
    expect(within(menu).getByText("ada@example.com")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "System" })).toBeChecked();
    await user.click(screen.getByRole("radio", { name: "Dark" }));

    expect(document.documentElement).toHaveClass("dark");
    expect(document.documentElement).not.toHaveClass("light");
    expect(localStorage.getItem("gp-ui-theme")).toBe('"dark"');
  });

  it("follows the system theme as it changes", async () => {
    let dark = false;
    const listeners: ((event: { matches: boolean }) => void)[] = [];
    vi.stubGlobal("matchMedia", (query: string) => ({
      get matches() {
        return dark;
      },
      media: query,
      addEventListener: (_: string, listener: (event: { matches: boolean }) => void) => {
        listeners.push(listener);
      },
      removeEventListener: () => {},
    }));
    renderRoute("/");
    await screen.findByRole("button", { name: "Open user menu" });
    expect(document.documentElement).toHaveClass("light");

    dark = true;
    act(() => {
      for (const listener of listeners) listener({ matches: dark });
    });

    expect(document.documentElement).toHaveClass("dark");
  });

  it("shows the not found page for unknown routes", async () => {
    renderRoute("/missing");

    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to the home page" })).toHaveAttribute("href", "/");
  });

  it("searches the portal with the platform's shortcut", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      "GET config": acmeConfig,
      "GET me": ok(ada),
      "GET search": (request: Request) => {
        const q = new URL(request.url).searchParams.get("q");
        return ok(
          q === ""
            ? [
                { kind: "blueprint", identifier: "service", title: "Service" },
                { kind: "dashboard", identifier: "ops", title: "Operations" },
              ]
            : [
                {
                  kind: "entity",
                  identifier: "payments",
                  title: "Payments",
                  detail: "Team",
                  blueprint: "team",
                },
                { kind: "scorecard", identifier: "payout-ready", title: "Payout Readiness" },
              ],
        )();
      },
    });
    const { router } = renderRoute("/");
    // Outside Apple devices the shortcut is Ctrl+K, and the sidebar says so.
    expect(await screen.findByRole("button", { name: /Search/ })).toHaveTextContent("CtrlK");

    // ⌘K is a Mac's shortcut, so here it's left alone.
    await user.keyboard("{Meta>}k{/Meta}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.keyboard("{Control>}k{/Control}");
    const dialog = within(await screen.findByRole("dialog"));
    // Before typing, it offers what to start from, each marked with its kind.
    expect(await dialog.findByRole("option", { name: /Service.*Blueprint/ })).toBeInTheDocument();
    await user.click(dialog.getByRole("option", { name: /Operations/ }));
    await vi.waitFor(() => expect(router.state.location.search).toEqual({ d: "ops" }));

    await user.keyboard("{Control>}k{/Control}");
    const again = within(await screen.findByRole("dialog"));
    await user.type(again.getByRole("combobox"), "pay");
    // One list, closest first; an entity names its blueprint beside its kind.
    const entity = await again.findByRole("option", { name: /Payments.*Team.*Entity/ });
    expect(again.getAllByRole("option").map((option) => option.textContent)).toEqual([
      "PaymentsTeamEntity",
      "Payout ReadinessScorecard",
    ]);
    expect(new URL(requests["GET search"].url).searchParams.get("q")).toBe("pay");
    expect(entity).toHaveAttribute("data-selected", "true");
    await user.keyboard("{Enter}");
    await vi.waitFor(() => expect(router.state.location.pathname).toBe("/catalog/team/payments"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
