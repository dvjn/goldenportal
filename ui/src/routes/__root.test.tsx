import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const { createApi } = await importOriginal<typeof import("@/lib/api")>();
  return { api: createApi("http://localhost/api/v1") };
});

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ data: { name: "Acme" } })),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("app shell", () => {
  it("renders the configured portal name and the header", async () => {
    renderRoute("/");

    expect(await screen.findByRole("link", { name: "Acme" })).toHaveAttribute("href", "/");
    expect(document.title).toBe("Acme");
    expect(screen.getAllByRole("button", { name: "Toggle Sidebar" })).toHaveLength(3);
    expect(screen.getByText("Home")).toHaveAttribute("aria-current", "page");
    expect(screen.queryByText(/version/i)).not.toBeInTheDocument();
  });

  it("renders nothing until the config has loaded", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {})),
    );
    const { container } = renderRoute("/");

    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(container).toBeEmptyDOMElement();
  });

  it("shows an error with a retry when the config request fails", async () => {
    const user = userEvent.setup();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ error: { message: "config unavailable" } }, { status: 500 }),
      ),
    );
    renderRoute("/");

    expect(await screen.findByText("config unavailable")).toBeInTheDocument();

    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ data: { name: "Acme" } })),
    );
    await user.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByRole("link", { name: "Acme" })).toBeInTheDocument();
  });

  it("starts in dark mode and stores the chosen theme", async () => {
    const user = userEvent.setup();
    renderRoute("/");

    expect(document.documentElement).toHaveClass("dark");

    await user.click(await screen.findByRole("button", { name: "Toggle theme" }));
    await user.click(await screen.findByRole("menuitem", { name: "Light" }));

    expect(document.documentElement).toHaveClass("light");
    expect(document.documentElement).not.toHaveClass("dark");
    expect(localStorage.getItem("gp-ui-theme")).toBe("light");
  });

  it("shows the not found page for unknown routes", async () => {
    renderRoute("/missing");

    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to the home page" })).toHaveAttribute("href", "/");
  });
});
