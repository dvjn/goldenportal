import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

function entityRequests(fetchMock: ReturnType<typeof stubApi>) {
  return fetchMock.mock.calls
    .map(([request]) => new URL(request.url))
    .filter((url) => url.pathname.endsWith("/entities"));
}

describe("entity list", () => {
  it("shows the entities with their integration and columns", async () => {
    stubApi({ ...signedIn, ...catalogApi });
    renderRoute("/catalog/service");

    const row = (await screen.findByRole("link", { name: "Ledger" })).closest("tr") as HTMLElement;
    expect(within(row).getByText("ledger")).toBeInTheDocument();
    expect(within(row).getByText("CI")).toBeInTheDocument();
    expect(within(row).getByText("1")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: /gitlab.example.test/ })).toHaveAttribute(
      "href",
      "https://gitlab.example.test/acme/ledger",
    );
    expect(screen.getByRole("columnheader", { name: "Tier" })).toBeInTheDocument();
    expect(screen.getByText("1 to 2 of 2")).toBeInTheDocument();
  });

  it("passes the search and the filter to the server", async () => {
    const user = userEvent.setup();
    const fetchMock = stubApi({ ...signedIn, ...catalogApi });
    renderRoute("/catalog/service?integration=ci&page=2");

    await screen.findByRole("link", { name: "Ledger" });
    expect(entityRequests(fetchMock)[0]?.search).toBe("?q=&integration=ci&offset=25&limit=25");

    await user.type(screen.getByLabelText("Search by title or identifier"), "pay");
    await vi.waitFor(() =>
      expect(entityRequests(fetchMock).at(-1)?.searchParams.get("q")).toBe("pay"),
    );
    expect(entityRequests(fetchMock).at(-1)?.searchParams.get("offset")).toBe("0");
  });
});
