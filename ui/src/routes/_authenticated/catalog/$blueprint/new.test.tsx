import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { bob, cara, signedInAs, stubApi } from "@/test/api";
import { catalogApi, paymentsApi } from "@/test/catalog";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("new entity page", () => {
  it("creates an entity from the blueprint's fields", async () => {
    const user = userEvent.setup();
    let body: unknown;
    stubApi({
      ...signedInAs(bob),
      ...catalogApi,
      "POST /api/v1/catalog/blueprints/service/entities": async (request) => {
        body = await request.json();
        return Response.json({ data: paymentsApi }, { status: 201 });
      },
    });
    renderRoute("/catalog/service/new");

    await user.type(await screen.findByLabelText("Title"), "Payments API");
    await user.type(screen.getByLabelText("Identifier"), "payments-api");
    await user.type(screen.getByLabelText("Launched"), "2026-03-02");
    await user.click(screen.getByLabelText("Public"));
    await user.click(screen.getByRole("button", { name: "Create entity" }));

    expect(await screen.findByRole("heading", { name: "Payments API" })).toBeInTheDocument();
    expect(body).toEqual({
      identifier: "payments-api",
      title: "Payments API",
      properties: { tier: null, repositoryUrl: null, public: true, launched: "2026-03-02" },
      relations: { owner: [] },
    });
  });

  it("shows the server's problems next to each field", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAs(bob),
      ...catalogApi,
      "POST /api/v1/catalog/blueprints/service/entities": () =>
        Response.json(
          {
            error: {
              message: "the entity has invalid fields, fix them and save again",
              fields: {
                identifier: "use letters, digits, dots, hyphens and underscores",
                "properties.repositoryUrl": "'gitlab' is not valid uri",
                "relations.owner": "no Team has the identifier billing",
              },
            },
          },
          { status: 422 },
        ),
    });
    renderRoute("/catalog/service/new");

    await user.type(await screen.findByLabelText("Title"), "Payments API");
    await user.click(screen.getByRole("button", { name: "Create entity" }));

    expect(
      await screen.findByText("use letters, digits, dots, hyphens and underscores"),
    ).toBeInTheDocument();
    expect(screen.getByText("'gitlab' is not valid uri")).toBeInTheDocument();
    expect(screen.getByText("no Team has the identifier billing")).toBeInTheDocument();
    expect(screen.getByLabelText("Identifier")).toHaveAttribute("aria-invalid", "true");
  });

  it("is forbidden for a member, who sees no New entity button", async () => {
    stubApi({ ...signedInAs(cara), ...catalogApi });
    renderRoute("/catalog/service/new");

    expect(await screen.findByText("You don't have access to this page.")).toBeInTheDocument();
  });
});
