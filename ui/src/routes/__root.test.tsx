import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { signedIn, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

beforeEach(() => {
  stubApi(signedIn);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("root route", () => {
  it("sets the document title to the portal name", async () => {
    renderRoute("/not-provisioned");

    await screen.findByRole("heading");
    expect(document.title).toBe("Acme");
  });

  it("renders nothing until the config has loaded", async () => {
    stubApi({
      ...signedIn,
      "GET /api/v1/config": () => new Promise<Response>(() => {}),
    });
    const { container } = renderRoute("/not-provisioned");

    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(container).toBeEmptyDOMElement();
  });

  it("shows an error with a retry when the config request fails", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedIn,
      "GET /api/v1/config": () =>
        Response.json({ error: { message: "config unavailable" } }, { status: 500 }),
    });
    renderRoute("/");

    expect(await screen.findByText("config unavailable")).toBeInTheDocument();

    stubApi(signedIn);
    await user.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByRole("link", { name: "Acme" })).toBeInTheDocument();
  });

  it("shows the not found page for unknown routes", async () => {
    renderRoute("/missing");

    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Go to the home page" })).toHaveAttribute("href", "/");
  });
});
