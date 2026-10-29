import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { bob, signedInAs, stubApi } from "@/test/api";
import { catalogApi } from "@/test/catalog";
import { historyApi } from "@/test/definitions";
import { integrationsApi } from "@/test/integrations";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("configuration redirect", () => {
  it("keeps the search of the old catalog setup link", async () => {
    stubApi({ ...signedInAs(bob), ...catalogApi, ...integrationsApi });
    renderRoute("/settings/configuration?tab=definitions");

    expect(await screen.findByRole("heading", { name: "Catalog setup" })).toBeInTheDocument();
    expect(await screen.findByRole("tab", { name: "Definitions" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("moves a nested old link", async () => {
    stubApi({ ...signedInAs(bob), ...historyApi });
    renderRoute("/settings/configuration/history");

    expect(await screen.findByRole("link", { name: "Revision 2" })).toBeInTheDocument();
  });
});
