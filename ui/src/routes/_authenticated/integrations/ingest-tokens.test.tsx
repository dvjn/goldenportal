import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { bob, cara, signedInAs, stubApi } from "@/test/api";
import { integrationsApi, pipelinesToken } from "@/test/integrations";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ingest tokens", () => {
  it("lists a push integration's tokens", async () => {
    stubApi({ ...signedInAs(bob), ...integrationsApi });
    renderRoute("/integrations/ci");

    const tokens = await screen.findByRole("region", { name: "Ingest tokens" });
    const row = within(tokens).getByText("pipelines").closest("tr") as HTMLElement;
    expect(within(row).getByText("bob@example.test")).toBeInTheDocument();
    expect(within(row).getByRole("button", { name: "Revoke" })).toBeInTheDocument();
  });

  it("creates a token and shows it once with a push command", async () => {
    const user = userEvent.setup();
    let body: unknown;
    stubApi({
      ...signedInAs(bob),
      ...integrationsApi,
      "POST /api/v1/integrations/ci/tokens": async (request) => {
        body = await request.json();
        return Response.json(
          {
            data: {
              token: { ...pipelinesToken, id: "t-2", name: "deploys" },
              secret: "gpi_secret",
              ingest_url: "https://portal.example.test/api/v1/ingest/ci",
            },
          },
          { status: 201 },
        );
      },
    });
    renderRoute("/integrations/ci");

    await user.click(await screen.findByRole("button", { name: "New ingest token" }));
    await user.type(screen.getByLabelText("Name"), "deploys");
    await user.click(screen.getByRole("button", { name: "Create token" }));

    expect(await screen.findByRole("textbox", { name: "New ingest token" })).toHaveValue(
      "gpi_secret",
    );
    expect(body).toEqual({ name: "deploys", expires_in_days: 90 });
    expect(screen.getByText(/curl -X POST/)).toHaveTextContent(
      'curl -X POST https://portal.example.test/api/v1/ingest/ci/service \\ -H "Authorization: Bearer gpi_secret"',
    );
    expect(screen.getByText(/service.json holds/)).toHaveTextContent(
      "each item a record the service mapping reads",
    );
    expect(screen.getByRole("button", { name: "Copy command" })).toBeInTheDocument();
  });

  it("revokes a token", async () => {
    const user = userEvent.setup();
    const revoked = vi.fn(() => new Response(null, { status: 204 }));
    stubApi({
      ...signedInAs(bob),
      ...integrationsApi,
      "DELETE /api/v1/integrations/ci/tokens/t-1": revoked,
    });
    renderRoute("/integrations/ci");

    const tokens = await screen.findByRole("region", { name: "Ingest tokens" });
    await user.click(within(tokens).getByRole("button", { name: "Revoke" }));
    await user.click(
      within(screen.getByRole("alertdialog")).getByRole("button", { name: "Revoke" }),
    );

    await waitFor(() => expect(revoked).toHaveBeenCalled());
  });

  it("hides the tokens from a member", async () => {
    stubApi({ ...signedInAs(cara), ...integrationsApi });
    renderRoute("/integrations/ci");

    expect(await screen.findByRole("region", { name: "Runs" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Ingest tokens" })).not.toBeInTheDocument();
  });
});
