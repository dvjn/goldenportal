import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { ServiceToken } from "@/features/service-tokens";
import { adminApi, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const ci: ServiceToken = {
  id: "t-1",
  name: "gitlab-ci",
  role: "maintainer",
  created_by: "alice@example.test",
  created_at: "2026-10-20T09:00:00Z",
  expires_at: "2099-01-20T09:00:00Z",
};

const tokenApi = {
  ...adminApi,
  "GET /api/v1/access/service-tokens": () => Response.json({ data: { tokens: [ci] } }),
};

describe("Service tokens page", () => {
  it("lists the tokens with their role and use", async () => {
    stubApi(tokenApi);
    renderRoute("/settings/service-tokens");

    const row = (await screen.findByText("gitlab-ci")).closest("tr") as HTMLElement;
    expect(within(row).getByText("Maintainer")).toBeInTheDocument();
    expect(within(row).getByText("alice@example.test")).toBeInTheDocument();
    expect(within(row).getByText("Never")).toBeInTheDocument();
    expect(within(row).getByRole("button", { name: "Revoke" })).toBeInTheDocument();
  });

  it("moves the old API tokens link", async () => {
    stubApi(tokenApi);
    renderRoute("/settings/api-tokens");

    expect(await screen.findByText("gitlab-ci")).toBeInTheDocument();
  });

  it("creates a token and shows its secret once", async () => {
    const user = userEvent.setup();
    let body: unknown;
    stubApi({
      ...tokenApi,
      "POST /api/v1/access/service-tokens": async (request) => {
        body = await request.json();
        return Response.json({ data: { token: ci, secret: "gpt_secret" } }, { status: 201 });
      },
    });
    renderRoute("/settings/service-tokens");

    await user.click(await screen.findByRole("button", { name: "New service token" }));
    await user.type(screen.getByLabelText("Name"), "gitlab-ci");
    await user.click(screen.getByRole("button", { name: "Create token" }));

    expect(await screen.findByRole("textbox", { name: "New service token" })).toHaveValue(
      "gpt_secret",
    );
    expect(body).toEqual({ name: "gitlab-ci", role: "member", expires_in_days: 90 });
    expect(screen.getByText(/Authorization: Bearer/)).toHaveTextContent(
      "https://portal.example.test/api/v1/auth/me",
    );
  });
});
