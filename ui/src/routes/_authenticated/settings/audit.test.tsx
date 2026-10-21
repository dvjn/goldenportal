import { screen } from "@testing-library/react";
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
      actor: { user_id: "u-1", label: "alice@example.test" },
      action: "grant.created",
      target: { kind: "group", id: "g-2", name: "platform-team" },
      details: { role: "admin", previous_role: "" },
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
    });
    renderRoute("/settings/audit?page=2");

    expect(await screen.findByText("Granted a role")).toBeInTheDocument();
    expect(screen.getByText("platform-team")).toBeInTheDocument();
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.getByText("Granted Admin")).toBeInTheDocument();
    expect(screen.getByText("51 to 51 of 51")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Previous" })).toHaveAttribute(
      "href",
      "/settings/audit?page=1",
    );
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    const request = fetchMock.mock.calls.find(([call]) => call.url.includes("/audit"))?.[0];
    expect(new URL(request?.url ?? "").search).toBe("?offset=50&limit=50");
  });
});
