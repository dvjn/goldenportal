import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { adminApi, bob, signedIn, signedInAs, stubApi } from "@/test/api";
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

async function trail() {
  const navigation = await screen.findByRole("navigation", { name: "breadcrumb" });
  return within(navigation)
    .queryAllByRole("link")
    .filter((link) => link.getAttribute("aria-label") !== "Go to home")
    .map((link) => [link.textContent, link.getAttribute("href")]);
}

describe("header breadcrumb", () => {
  it("shows no crumb on a page without ancestors", async () => {
    stubApi(adminApi);
    renderRoute("/settings/system");

    await screen.findByRole("heading", { name: "System settings" });
    expect(await trail()).toEqual([]);
  });

  it("shows no crumb for a blueprint list", async () => {
    stubApi({ ...signedIn, ...catalogApi });
    renderRoute("/catalog/service");

    await screen.findByRole("heading", { name: "Service" });
    expect(await trail()).toEqual([]);
  });

  it("shows the blueprint but not the entity on an entity page", async () => {
    stubApi({ ...signedIn, ...catalogApi });
    renderRoute("/catalog/service/ledger");

    await screen.findByRole("heading", { name: "Ledger" });
    expect(await trail()).toEqual([["Service", "/catalog/service"]]);
  });

  it("shows no crumb for the selected item or tab of catalog setup", async () => {
    stubApi({ ...signedInAs(bob), ...catalogApi, ...integrationsApi });
    renderRoute("/settings/catalog-setup?tab=integrations");

    await screen.findByRole("heading", { name: "Catalog setup" });
    expect(await trail()).toEqual([]);
  });

  it("shows only the ancestors of a revision", async () => {
    stubApi({ ...signedInAs(bob), ...historyApi });
    renderRoute("/settings/catalog-setup/history/1");

    await screen.findByRole("heading", { name: /Revision 1/ });
    expect(await trail()).toEqual([
      ["Catalog setup", "/settings/catalog-setup"],
      ["History", "/settings/catalog-setup/history"],
    ]);
  });

  it("drops the current page from the trail", async () => {
    stubApi({ ...signedInAs(bob), ...historyApi });
    renderRoute("/settings/catalog-setup/history");

    await screen.findByRole("link", { name: "Revision 2" });
    expect(await trail()).toEqual([["Catalog setup", "/settings/catalog-setup"]]);
  });
});
