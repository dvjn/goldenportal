import { cleanup, screen, within } from "@testing-library/react";
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
      "GET runs": ok({ runs: [], total: 0 }),
    });
    renderRoute("/runs");

    const heading = await screen.findByRole("heading", { level: 1, name: "Runs" });
    const tile = heading.closest(".flex.min-w-0.items-center")?.firstElementChild;
    const nav = screen
      .getAllByRole("link", { name: "Runs" })
      .find((link) => link.closest('[data-sidebar="sidebar"]'));
    expect(iconOf(tile)).toBeDefined();
    expect(iconOf(tile)).toBe(iconOf(nav));
    expect(tile).toHaveClass("bg-gray-500/15");

    // A page below it shows the same icon in its trail.
    stubApi({
      "GET config": acmeConfig,
      "GET me": ok({ ...ada, role: "member" }),
      "GET runs/r1": ok({
        id: "r1",
        kind: "action",
        action: "restart",
        title: "Restart",
        version: 1,
        entity: { blueprint: "service", identifier: "ledger", title: "Ledger" },
        requester: { id: "1", email: "ada@example.com", name: "Ada", role: "member", groups: [] },
        via: "web",
        inputs: {},
        status: "succeeded",
        summary: "",
        links: [],
        outputs: {},
        createdAt: "2026-10-24T10:00:00Z",
        startedAt: null,
        finishedAt: null,
        steps: [],
      }),
      "GET actions/restart": ok(null),
      "GET catalog/service/actions": ok([]),
    });
    cleanup();
    renderRoute("/runs/r1");
    const header = (
      await screen.findByRole("heading", { level: 1, name: "Restart" })
    ).closest<HTMLElement>('[data-slot="page-header"]') as HTMLElement;
    const crumb = within(header).getByRole("link", { name: "Runs" }).parentElement;
    expect(iconOf(crumb)).toBe(iconOf(nav));
  });
});
