import { screen } from "@testing-library/react";
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
    expect(document.title).toBe("Acme");
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

    expect(document.documentElement).toHaveClass("dark");

    await user.click(await screen.findByRole("button", { name: "Open user menu" }));
    expect(screen.getByText("Ada Lovelace")).toBeInTheDocument();
    expect(screen.getByText("ada@example.com")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Dark" })).toBeChecked();
    await user.click(screen.getByRole("radio", { name: "Light" }));

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
