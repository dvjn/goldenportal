import { screen, waitFor } from "@testing-library/react";
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

const signedOut = {
  "GET config": acmeConfig,
  "GET me": fail(401, "sign in to continue"),
};

describe("sign in", () => {
  it("sends signed-out visitors to sign in and back afterwards", async () => {
    stubApi(signedOut);
    const { router } = renderRoute("/?tab=overview");

    const signIn = await screen.findByRole("link", { name: "Sign in with SSO" });
    expect(router.state.location.pathname).toBe("/sign-in");
    expect(signIn).toHaveAttribute(
      "href",
      `/api/v1/auth/login?redirect=${encodeURIComponent("/?tab=overview")}`,
    );
  });

  it("never sends single sign-on back to another site", async () => {
    stubApi(signedOut);
    renderRoute("/sign-in?redirect=%2F%2Fevil.example");

    expect(await screen.findByRole("link", { name: "Sign in with SSO" })).toHaveAttribute(
      "href",
      "/api/v1/auth/login?redirect=%2F",
    );
  });

  it("shows why the last sign-in failed", async () => {
    stubApi(signedOut);
    renderRoute("/sign-in?error=Your+account+is+deactivated.");

    expect(await screen.findByText("Couldn't sign you in")).toBeInTheDocument();
    expect(screen.getByText("Your account is deactivated.")).toBeInTheDocument();
  });

  it("skips the sign-in page when already signed in", async () => {
    stubApi({ "GET config": acmeConfig, "GET me": ok(ada) });
    const { router } = renderRoute("/sign-in?redirect=%2F");

    expect(await screen.findByRole("link", { name: "Acme" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
  });

  it("signs out from the user menu", async () => {
    const user = userEvent.setup();
    stubApi({ "GET config": acmeConfig, "GET me": ok(ada) });
    const { router } = renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "Open user menu" }));
    const requests = stubApi({
      ...signedOut,
      "POST auth/logout": () => new Response(null, { status: 201 }),
    });
    await user.click(screen.getByRole("menuitem", { name: "Sign out" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/sign-in"));
    expect(requests["POST auth/logout"]?.headers.get("X-GP-CSRF")).toBe("1");
  });
});
