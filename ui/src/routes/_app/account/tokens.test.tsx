import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { acmeConfig, ada, fail, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const days = (n: number) => new Date(Date.now() + n * 24 * 60 * 60 * 1000).toISOString();

const ciToken = {
  id: "t1",
  name: "CI pipeline",
  prefix: "gpp_AbCdEfGh",
  readOnly: false,
  expiresAt: days(10),
  lastUsedAt: null,
  createdAt: days(-1),
};

const signedIn = { "GET config": acmeConfig, "GET me": ok(ada) };

describe("API tokens page", () => {
  it("lists tokens with their access and expiry", async () => {
    stubApi({
      ...signedIn,
      "GET me/tokens": ok([
        ciToken,
        { ...ciToken, id: "t2", name: "Old script", readOnly: true, expiresAt: days(-2) },
      ]),
    });
    renderRoute("/account/tokens");

    const ci = (await screen.findByRole("cell", { name: "CI pipeline" })).closest("tr");
    expect(ci).not.toBeNull();
    expect(within(ci as HTMLElement).getByText("gpp_AbCdEfGh…")).toBeInTheDocument();
    expect(within(ci as HTMLElement).getByText("Read and write")).toBeInTheDocument();
    expect(within(ci as HTMLElement).getByText("in 10 days")).toBeInTheDocument();
    expect(within(ci as HTMLElement).getByText("Never")).toBeInTheDocument();

    const old = screen.getByRole("cell", { name: "Old script" }).closest("tr") as HTMLElement;
    expect(within(old).getByText("Read only")).toBeInTheDocument();
    expect(within(old).getByText("Expired")).toBeInTheDocument();
  });

  it("explains what tokens are for when there are none", async () => {
    stubApi({ ...signedIn, "GET me/tokens": ok([]) });
    renderRoute("/account/tokens");

    expect(await screen.findByText("No API tokens yet")).toBeInTheDocument();
  });

  it("creates a token and shows the secret once", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedIn,
      "GET me/tokens": ok([]),
      "POST me/tokens": ok({
        token: { ...ciToken, readOnly: true },
        secret: "gpp_AbCdEfGh-the-whole-secret",
      }),
    });
    renderRoute("/account/tokens");

    await user.click(await screen.findByRole("button", { name: "New token" }));
    await user.type(screen.getByLabelText("Name"), "CI pipeline");
    await user.click(screen.getByLabelText("Read only"));
    await user.click(screen.getByRole("button", { name: "Create token" }));

    expect(await screen.findByLabelText("New API token")).toHaveValue(
      "gpp_AbCdEfGh-the-whole-secret",
    );
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
    expect(await navigator.clipboard.readText()).toBe("gpp_AbCdEfGh-the-whole-secret");
    expect(await requests["POST me/tokens"]?.json()).toEqual({
      name: "CI pipeline",
      readOnly: true,
      expiresInDays: 90,
    });

    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByLabelText("New API token")).not.toBeInTheDocument();
  });

  it("shows why a token could not be created", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedIn,
      "GET me/tokens": ok([]),
      "POST me/tokens": fail(400, "give the token a name of up to 100 characters"),
    });
    renderRoute("/account/tokens");

    await user.click(await screen.findByRole("button", { name: "New token" }));
    await user.type(screen.getByLabelText("Name"), " ");
    await user.click(screen.getByRole("button", { name: "Create token" }));

    expect(
      await screen.findByText("give the token a name of up to 100 characters"),
    ).toBeInTheDocument();
  });

  it("revokes a token after confirming", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedIn, "GET me/tokens": ok([ciToken]) });
    renderRoute("/account/tokens");

    await user.click(await screen.findByRole("button", { name: "Revoke CI pipeline" }));
    const requests = stubApi({
      ...signedIn,
      "GET me/tokens": ok([]),
      "DELETE me/tokens/t1": () => new Response(null, { status: 204 }),
    });
    await user.click(screen.getByRole("button", { name: "Revoke token" }));

    expect(await screen.findByText("No API tokens yet")).toBeInTheDocument();
    expect(requests["DELETE me/tokens/t1"]).toBeDefined();
    expect(await screen.findByText("Revoked CI pipeline")).toBeInTheDocument();
  });

  it("is reachable from the user menu", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedIn, "GET me/tokens": ok([]) });
    const { router } = renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "Open user menu" }));
    await user.click(screen.getByRole("menuitem", { name: "API tokens" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/account/tokens"));
  });

  it("searches, sorts and pages tokens through the URL", async () => {
    const user = userEvent.setup();
    const many = Array.from({ length: 25 }, (_, i) => ({
      ...ciToken,
      id: `t${i}`,
      name: `Token ${String(i + 1).padStart(2, "0")}`,
    }));
    stubApi({
      ...signedIn,
      "GET me/tokens": ok([...many, { ...ciToken, id: "x", name: "Deploy bot" }]),
    });
    const { router } = renderRoute("/account/tokens");

    expect(await screen.findByText("Page 1 of 3")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ page: 2 }));
    expect(screen.getByText("Page 2 of 3")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Name" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ sort: "name" }));
    expect(router.state.location.search).not.toHaveProperty("page");
    const firstRow = () => screen.getAllByRole("row")[1] as HTMLElement;
    expect(within(firstRow()).getByText("Deploy bot")).toBeInTheDocument();

    await user.type(screen.getByRole("searchbox", { name: "Search tokens" }), "deploy");
    await waitFor(() => expect(router.state.location.search).toMatchObject({ q: "deploy" }));
    expect(screen.getAllByRole("row")).toHaveLength(2);
    expect(screen.getByText("Showing 1 to 1 of 1")).toBeInTheDocument();
  });

  it("goes back to the last page from one past the end", async () => {
    stubApi({ ...signedIn, "GET me/tokens": ok([ciToken]) });
    const { router } = renderRoute("/account/tokens?page=4");

    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("page"));
    expect(await screen.findByText("Showing 1 to 1 of 1")).toBeInTheDocument();
  });

  it("restores the table state from the URL", async () => {
    stubApi({
      ...signedIn,
      "GET me/tokens": ok([ciToken, { ...ciToken, id: "x", name: "Deploy bot" }]),
    });
    renderRoute("/account/tokens?q=deploy");

    expect(await screen.findByText("Deploy bot")).toBeInTheDocument();
    expect(screen.queryByText("CI pipeline")).not.toBeInTheDocument();
    expect(screen.getByRole("searchbox", { name: "Search tokens" })).toHaveValue("deploy");
  });
});
