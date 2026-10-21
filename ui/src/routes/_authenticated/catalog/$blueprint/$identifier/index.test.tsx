import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { signedIn, stubApi } from "@/test/api";
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
});
