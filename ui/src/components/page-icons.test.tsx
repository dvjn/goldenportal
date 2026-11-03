import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

// iconOf names the lucide icon an element draws, by its class.
function iconOf(element: Element | null | undefined) {
  return [...(element?.querySelector("svg")?.classList ?? [])].find(
    (name) => name.startsWith("lucide-") && name !== "lucide-icon",
  );
}

describe("page icons", () => {
  it("leads a top-level page with its sidebar icon, on a gray tile", async () => {
    stubApi({
      "GET config": acmeConfig,
      "GET me": ok({ ...ada, role: "member" }),
      "GET scorecards": ok([]),
    });
    renderRoute("/scorecards");

    const heading = await screen.findByRole("heading", { level: 1, name: "Scorecards" });
    const tile = heading.closest(".flex.min-w-0.items-center")?.firstElementChild;
    const nav = screen
      .getAllByRole("link", { name: "Scorecards" })
      .find((link) => link.closest('[data-sidebar="sidebar"]'));
    expect(iconOf(tile)).toBeDefined();
    expect(iconOf(tile)).toBe(iconOf(nav));
    expect(tile).toHaveClass("bg-gray-500/15");
  });
});
