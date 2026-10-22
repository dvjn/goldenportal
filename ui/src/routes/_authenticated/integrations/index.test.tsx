import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { bob, signedInAs, stubApi } from "@/test/api";
import { integrationsApi } from "@/test/integrations";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function row(name: string) {
  return screen.getByRole("link", { name }).closest("tr") as HTMLElement;
}

describe("integrations page", () => {
  it("lists every integration with its connector, entities and runs", async () => {
    stubApi({ ...signedInAs(bob), ...integrationsApi });
    renderRoute("/integrations");

    expect(await screen.findByRole("heading", { name: "Integrations" })).toBeInTheDocument();
    expect(within(row("Manual")).getByText("Built in")).toBeInTheDocument();
    expect(within(row("Manual")).getByText("No runs yet")).toBeInTheDocument();
    expect(within(row("CI")).getByText("Push")).toBeInTheDocument();
    expect(within(row("CI")).getByText("Partial")).toBeInTheDocument();
    expect(within(row("CI")).getByText("When the source pushes")).toBeInTheDocument();
    expect(within(row("GitLab")).getByText("Failed")).toBeInTheDocument();
    expect(within(row("GitLab")).getByText(/^in \d+ years$/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "GitLab" })).toHaveAttribute(
      "href",
      "/integrations/gitlab",
    );
  });
});
