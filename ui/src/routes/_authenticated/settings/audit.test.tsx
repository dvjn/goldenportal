import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { AuditPage } from "@/features/audit";
import { adminApi, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const secondPage: AuditPage = {
  entries: [
    {
      id: 51,
      at: "2026-10-22T09:00:00Z",
      actor: { kind: "user", id: "u-1", name: "Alice", email: "alice@example.test" },
      action: "grant.created",
      resource: { kind: "group", id: "g-2", name: "platform-team" },
      details: { changes: { role: { before: "", after: "admin" } } },
      client: { ip: "203.0.113.7", user_agent: "Firefox" },
    },
  ],
  offset: 50,
  limit: 50,
  total: 51,
};

describe("audit log page", () => {
  it("shows a page of entries with readable actions", async () => {
    const fetchMock = stubApi({
      ...adminApi,
      "GET /api/v1/audit": () => Response.json({ data: secondPage }),
      "GET /api/v1/audit/facets": () =>
        Response.json({ data: { actors: [], actor_kinds: [], actions: [], resource_kinds: [] } }),
    });
    renderRoute("/settings/audit?page=2");

    expect(await screen.findByText("Granted a role")).toBeInTheDocument();
    expect(screen.getByText("Granted Admin")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Alice" })).toHaveAttribute(
      "href",
      "/settings/access?user=u-1",
    );
    expect(screen.getByText("alice@example.test")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "platform-team" })).toHaveAttribute(
      "href",
      "/settings/access",
    );
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.getByText("Showing 51 to 51 of 51")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    const request = fetchMock.mock.calls.find(([call]) => call.url.includes("/audit?"))?.[0];
    expect(new URL(request?.url ?? "").search).toBe("?offset=50&limit=50&sort=desc");

    await userEvent.click(screen.getByRole("button", { name: "Show the entry" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("203.0.113.7")).toBeInTheDocument();
    expect(within(dialog).getByText("Firefox")).toBeInTheDocument();
    expect(within(dialog).getByRole("cell", { name: "role" })).toBeInTheDocument();
    expect(within(dialog).getByRole("cell", { name: "admin" })).toBeInTheDocument();
  });

  it("filters by an actor kind through the actor column", async () => {
    const fetchMock = stubApi({
      ...adminApi,
      "GET /api/v1/audit": () => Response.json({ data: secondPage }),
      "GET /api/v1/audit/facets": () =>
        Response.json({
          data: {
            actors: [{ kind: "user", id: "u-1", name: "Alice", email: "alice@example.test" }],
            actor_kinds: ["user", "scim"],
            actions: ["grant.created"],
            resource_kinds: ["group"],
          },
        }),
    });
    renderRoute("/settings/audit");

    await userEvent.click(await screen.findByRole("button", { name: "Filter by actor" }));
    await userEvent.click(await screen.findByRole("menuitemradio", { name: "SCIM provisioning" }));

    await vi.waitFor(() => {
      const urls = fetchMock.mock.calls.map(([call]) => call.url);
      expect(urls.some((url) => url.includes("actor_kind=scim"))).toBe(true);
    });
  });
});
