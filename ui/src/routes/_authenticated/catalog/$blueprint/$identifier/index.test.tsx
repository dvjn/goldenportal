import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { bob, cara, signedIn, signedInAs, stubApi } from "@/test/api";
import { catalogApi } from "@/test/catalog";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("entity page", () => {
  it("shows the properties by type and the relations as links", async () => {
    stubApi({ ...signedIn, ...catalogApi });
    renderRoute("/catalog/service/ledger");

    expect(await screen.findByRole("heading", { name: "Ledger" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Payments" })).toHaveAttribute(
      "href",
      "/catalog/team/payments",
    );
    expect(screen.getByText("Mar 2, 2026")).toBeInTheDocument();
    expect(screen.getByText("No")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Service" })).toHaveAttribute(
      "href",
      "/catalog/service",
    );
    expect(screen.getAllByText("Ledger")).toHaveLength(2);
  });

  it("shows a member no edit actions", async () => {
    stubApi({ ...signedInAs(cara), ...catalogApi });
    renderRoute("/catalog/service/payments-api");

    expect(await screen.findByRole("heading", { name: "Payments API" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Edit" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
  });

  it("explains why an entity of an integration cannot be edited", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAs(bob), ...catalogApi });
    renderRoute("/catalog/service/ledger");

    const edit = await screen.findByRole("button", { name: "Edit" });
    expect(edit).toBeDisabled();
    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
    await user.hover(edit.parentElement as HTMLElement);
    expect((await screen.findAllByText("Managed by the CI integration")).length).toBeGreaterThan(0);
  });

  it("deletes a manual entity after confirmation", async () => {
    const user = userEvent.setup();
    let deleted = false;
    stubApi({
      ...signedInAs(bob),
      ...catalogApi,
      "DELETE /api/v1/catalog/blueprints/service/entities/payments-api": () => {
        deleted = true;
        return new Response(null, { status: 204 });
      },
    });
    renderRoute("/catalog/service/payments-api");

    expect(await screen.findByRole("link", { name: "Edit" })).toHaveAttribute(
      "href",
      "/catalog/service/payments-api/edit",
    );
    await user.click(screen.getByRole("button", { name: "Delete" }));
    await user.click(
      within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }),
    );

    expect(await screen.findByRole("columnheader", { name: "Tier" })).toBeInTheDocument();
    expect(deleted).toBe(true);
  });
});
