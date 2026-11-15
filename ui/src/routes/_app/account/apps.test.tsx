import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const config = ok({
  name: "Acme",
  mcp: {
    user: "https://portal.example.com/api/mcp",
    builder: "https://portal.example.com/api/mcp/builder",
  },
});

const apps = [
  {
    id: "a1",
    client: "Claude Code",
    server: "Builder MCP",
    scopes: ["portal:read", "portal:author"],
    createdAt: new Date().toISOString(),
    lastUsedAt: null,
  },
];

describe("connected apps", () => {
  it("lists each app by the server it connects to, and says where builders connect", async () => {
    stubApi({
      "GET config": config,
      "GET me": ok({ ...ada, role: "builder" }),
      "GET account/apps": ok(apps),
    });
    renderRoute("/account/apps");

    expect(await screen.findByText("Builder MCP")).toBeInTheDocument();
    expect(screen.getByText("https://portal.example.com/api/mcp")).toBeInTheDocument();
    expect(screen.getByText("https://portal.example.com/api/mcp/builder")).toBeInTheDocument();
  });

  it("names only the user's server to members", async () => {
    stubApi({
      "GET config": config,
      "GET me": ok({ ...ada, role: "member" }),
      "GET account/apps": ok([]),
    });
    renderRoute("/account/apps");

    expect(await screen.findByText("https://portal.example.com/api/mcp")).toBeInTheDocument();
    expect(
      screen.queryByText("https://portal.example.com/api/mcp/builder"),
    ).not.toBeInTheDocument();
  });
});
