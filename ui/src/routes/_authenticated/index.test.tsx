import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { signedIn, stubApi } from "@/test/api";
import { catalogApi } from "@/test/catalog";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Home", () => {
  it("shows a card per blueprint with its entity count", async () => {
    stubApi({ ...signedIn, ...catalogApi });
    renderRoute("/");

    const service = await screen.findByRole("link", { name: /^Service/ });
    expect(service).toHaveAttribute("href", "/catalog/service");
    expect(service).toHaveTextContent("A deployable service");
    expect(service).toHaveTextContent(/1\s*entity/);
    expect(screen.getByRole("link", { name: /^Team/ })).toHaveTextContent(/2\s*entities/);
  });

  it("says when no blueprint is defined", async () => {
    stubApi(signedIn);
    renderRoute("/");

    expect(await screen.findByText("The catalog is empty")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open the definitions" })).toHaveAttribute(
      "href",
      "/settings/catalog-setup?tab=definitions",
    );
  });
});
