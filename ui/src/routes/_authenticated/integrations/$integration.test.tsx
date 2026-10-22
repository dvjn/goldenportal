import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { bob, cara, signedInAs, stubApi } from "@/test/api";
import { failedSync, gitlab, integrationsApi } from "@/test/integrations";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("integration page", () => {
  it("shows the definition, the entities it owns and its runs", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAs(bob), ...integrationsApi });
    renderRoute("/integrations/ci");

    expect(await screen.findByRole("heading", { name: /CI/ })).toBeInTheDocument();
    expect(screen.getByText("Receives pushes")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open in the definitions editor" })).toHaveAttribute(
      "href",
      "/definitions?document=Integration%2Fci",
    );
    expect(screen.getByRole("link", { name: "Service" })).toHaveAttribute(
      "href",
      "/catalog/service?integration=ci",
    );
    expect(screen.queryByRole("button", { name: "Sync now" })).not.toBeInTheDocument();

    const runs = screen.getByRole("region", { name: "Runs" });
    expect(within(runs).getByText("service, snapshot")).toBeInTheDocument();
    expect(within(runs).getByText("3 written")).toBeInTheDocument();
    await user.click(within(runs).getByRole("button", { name: "Show 2 warnings" }));
    expect(within(runs).getByText(/^Kept 2 service entities/)).toBeInTheDocument();
    await user.click(within(runs).getByRole("button", { name: "Hide details" }));
    expect(within(runs).queryByText(/Kept 2 service entities/)).not.toBeInTheDocument();
  });

  it("syncs a pull integration and shows why its last run failed", async () => {
    const user = userEvent.setup();
    let synced = false;
    stubApi({
      ...signedInAs(bob),
      ...integrationsApi,
      "POST /api/v1/integrations/gitlab/sync": () => {
        synced = true;
        return Response.json(
          { data: { ...failedSync, id: 4, status: "running" } },
          { status: 201 },
        );
      },
    });
    renderRoute("/integrations/gitlab");

    expect(await screen.findByText("token from")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Show error" }));
    expect(screen.getByText(/^Could not reach GitLab/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Sync now" }));
    expect(synced).toBe(true);
  });

  it("disables sync while a sync runs, and hides it from a member", async () => {
    const running = { ...gitlab, last_run: { ...failedSync, status: "running" as const } };
    stubApi({
      ...signedInAs(bob),
      ...integrationsApi,
      "GET /api/v1/integrations/gitlab": () => Response.json({ data: running }),
    });
    const { unmount } = renderRoute("/integrations/gitlab");

    expect(await screen.findByRole("button", { name: "Sync now" })).toBeDisabled();
    unmount();

    stubApi({ ...signedInAs(cara), ...integrationsApi });
    renderRoute("/integrations/gitlab");
    expect(await screen.findByRole("heading", { name: /GitLab/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Sync now" })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Open in the definitions editor" }),
    ).not.toBeInTheDocument();
  });
});
