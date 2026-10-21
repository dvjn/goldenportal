import { screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vite-plus/test";
import { signedIn, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

let fetchMock: ReturnType<typeof stubApi>;

beforeEach(() => {
  fetchMock = stubApi(signedIn);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("not provisioned page", () => {
  it("explains what to do and offers to sign out", async () => {
    renderRoute("/not-provisioned");

    expect(
      await screen.findByRole("heading", { name: "You don't have access to this portal yet" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Contact your administrator to get access.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sign out" })).toHaveAttribute(
      "href",
      "/api/v1/auth/signout",
    );
  });

  it("shows no technical details and no shell", async () => {
    renderRoute("/not-provisioned");
    await screen.findByRole("heading");

    expect(document.body.textContent).not.toContain("@");
    expect(document.body.textContent).not.toContain("SCIM");
    expect(screen.queryByRole("button", { name: "Toggle Sidebar" })).not.toBeInTheDocument();
    const requested = fetchMock.mock.calls.map(([request]) => new URL(request.url).pathname);
    expect(requested).not.toContain("/api/v1/auth/me");
  });
});
