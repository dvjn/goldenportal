import { screen, within } from "@testing-library/react";
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
  vi.restoreAllMocks();
});

const request =
  "/oauth/authorize?response_type=code&client_id=gpc_1&redirect_uri=http%3A%2F%2Flocalhost%3A3000%2Fcb" +
  "&code_challenge=abc&code_challenge_method=S256&state=s1";

const consent = {
  client: "Claude Code",
  redirectUri: "http://localhost:3000/cb",
  redirectHost: "localhost:3000",
  local: true,
  scopes: ["portal:read"],
  withheld: ["portal:author"],
};

describe("authorizing an app", () => {
  it("names the app, what it may do and where it goes back to, then sends the answer", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });
    const requests = stubApi({
      "GET config": acmeConfig,
      "GET me": ok({ ...ada, role: "member" }),
      "GET oauth/authorize": ok(consent),
      "POST oauth/authorize": ok({ redirect: "http://localhost:3000/cb?code=gpk_1&state=s1" }),
    });
    renderRoute(request);

    expect(await screen.findByText("Authorize Claude Code")).toBeInTheDocument();
    // The request is passed on as the app sent it.
    const asked = new URL(requests["GET oauth/authorize"].url).searchParams;
    expect(asked.get("client_id")).toBe("gpc_1");
    expect(asked.get("code_challenge")).toBe("abc");
    expect(
      within(screen.getByRole("list", { name: "Allowed" })).getAllByRole("listitem"),
    ).toHaveLength(1);
    // A member can't hand over authoring, and is told so.
    expect(screen.getByText(/Your role can't allow/)).toHaveTextContent("Change, publish");
    // A redirect to this machine is flagged.
    expect(screen.getByText("localhost:3000")).toBeInTheDocument();
    expect(screen.getByText("This device")).toBeInTheDocument();
    // A self-registered app's name proves nothing.
    expect(screen.getByText("Unverified")).toBeInTheDocument();

    await userEvent.setup().click(screen.getByRole("button", { name: "Allow" }));
    await vi.waitFor(() =>
      expect(assign).toHaveBeenCalledWith("http://localhost:3000/cb?code=gpk_1&state=s1"),
    );
    const decided = (await requests["POST oauth/authorize"].json()) as Record<string, unknown>;
    expect(decided).toMatchObject({ client_id: "gpc_1", state: "s1", approve: true });
  });

  it("names the domain that published the app's metadata", async () => {
    stubApi({
      "GET config": acmeConfig,
      "GET me": ok(ada),
      "GET oauth/authorize": ok({
        ...consent,
        clientHost: "claude.ai",
        logo: "data:image/png;base64,AA==",
      }),
    });
    renderRoute(request);

    expect(await screen.findByText("claude.ai")).toBeInTheDocument();
    expect(screen.queryByText("Unverified")).not.toBeInTheDocument();
  });

  it("explains a request it can't grant and offers to tell the app", async () => {
    stubApi({
      "GET config": acmeConfig,
      "GET me": ok(ada),
      "GET oauth/authorize": ok({
        ...consent,
        scopes: [],
        withheld: [],
        problem: "PKCE with code_challenge_method=S256 is required",
        redirect: "http://localhost:3000/cb?error=invalid_request",
      }),
    });
    renderRoute(request);

    expect(await screen.findByText(/PKCE/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Return to App" })).toHaveAttribute(
      "href",
      "http://localhost:3000/cb?error=invalid_request",
    );
    expect(screen.queryByRole("button", { name: "Allow" })).not.toBeInTheDocument();
  });

  it("never sends the browser to an app it doesn't know", async () => {
    stubApi({
      "GET config": acmeConfig,
      "GET me": ok(ada),
      "GET oauth/authorize": fail(400, "the app asking for access isn't registered"),
    });
    renderRoute(request);

    expect(
      await screen.findByText("the app asking for access isn't registered"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("signs people in first, then brings them back", async () => {
    stubApi({ "GET config": acmeConfig, "GET me": fail(401, "sign in to continue") });
    const { router } = renderRoute(request);

    await screen.findByRole("link", { name: "Sign in with SSO" });
    expect(router.state.location.pathname).toBe("/sign-in");
    expect(router.state.location.search).toMatchObject({
      redirect: expect.stringContaining("/oauth/authorize?"),
    });
  });
});

describe("connected apps", () => {
  it("lists authorized apps and revokes one", async () => {
    const requests = stubApi({
      "GET config": acmeConfig,
      "GET me": ok(ada),
      "GET oauth/grants": ok([
        {
          id: "g1",
          client: "Claude Code",
          scopes: ["portal:read", "portal:author"],
          createdAt: "2026-11-01T10:00:00Z",
          lastUsedAt: null,
        },
      ]),
      "DELETE oauth/grants/g1": ok(null),
    });
    renderRoute("/account/apps");

    const row = (await screen.findByText("Claude Code")).closest("tr") as HTMLElement;
    expect(within(row).getByText("Read")).toBeInTheDocument();
    expect(within(row).getByText("Author")).toBeInTheDocument();
    expect(within(row).getByText("Never")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(within(row).getByRole("button", { name: "Revoke Claude Code" }));
    await user.click(await screen.findByRole("button", { name: "Revoke Access" }));
    await vi.waitFor(() => expect(requests["DELETE oauth/grants/g1"]).toBeDefined());
  });
});
