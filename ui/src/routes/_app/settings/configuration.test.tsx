import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { ServerConfiguration } from "@/features/configuration";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const signedInAdmin = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "admin" }) };

const logo = "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg'></svg>";

const configuration: ServerConfiguration = {
  portal: { name: "Acme", logoUrl: logo, baseUrl: "https://portal.example.com" },
  oidc: {
    configured: true,
    issuer: "https://idp.example.com",
    clientId: "acme-portal",
    clientSecretSet: true,
    scopes: ["openid", "profile", "email"],
    groupsClaim: "groups",
    redirectUrl: "https://portal.example.com/api/v1/auth/callback",
    bootstrapAdminEmail: "",
  },
  scim: { enabled: false, url: "https://portal.example.com/scim/v2" },
  sessions: { secureCookies: false, idleTimeoutHours: 8, maxAgeHours: 168 },
  tokens: { prefix: "gpp_", maxDays: 365 },
  database: {
    dialect: "postgres",
    host: "db.internal",
    port: "5432",
    name: "portal",
    user: "portal_app",
    passwordSet: false,
    sslMode: "require",
  },
  server: {
    environment: "production",
    version: "1.4.0",
    httpPort: "8000",
    metricsPort: "2121",
    logLevel: "INFO",
  },
};

const row = (label: string) =>
  screen.getByText(label).closest("[data-slot=setting]") as HTMLElement;

const card = (title: string) => screen.getByText(title).closest("[data-slot=card]") as HTMLElement;

describe("configuration", () => {
  it("shows the portal with its logo beside its name", async () => {
    stubApi({ ...signedInAdmin, "GET settings/configuration": ok(configuration) });
    renderRoute("/settings/configuration");

    const portal = await screen.findByRole("img", { name: "Acme logo" });
    expect(portal).toHaveAttribute("src", logo);
    expect(within(card("Portal")).getByText("Acme")).toBeInTheDocument();
    expect(within(card("Portal")).getByText("https://portal.example.com")).toBeInTheDocument();
    expect(screen.queryByText(logo)).toBeNull();
  });

  it("sums up authentication, with secrets only as set or not", async () => {
    stubApi({ ...signedInAdmin, "GET settings/configuration": ok(configuration) });
    renderRoute("/settings/configuration");

    expect(await screen.findByText("Authentication")).toBeInTheDocument();
    expect(within(card("Authentication")).queryByText("Not configured")).not.toBeInTheDocument();
    expect(within(row("Issuer")).getByText("https://idp.example.com")).toBeInTheDocument();
    expect(within(row("Client ID")).getByText("acme-portal")).toBeInTheDocument();
    expect(within(row("Client Secret")).getByText("Set")).toBeInTheDocument();
    for (const scope of ["openid", "profile", "email"]) {
      expect(within(row("Scopes")).getByText(scope)).toBeInTheDocument();
    }
    expect(within(row("Groups Claim")).getByText("groups")).toBeInTheDocument();
    expect(within(row("Bootstrap Admin")).getByText("Not set")).toBeInTheDocument();
    expect(within(row("Provisioning")).getByText("SCIM off")).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("says when sign-in is not configured", async () => {
    stubApi({
      ...signedInAdmin,
      "GET settings/configuration": ok({
        ...configuration,
        oidc: { ...configuration.oidc, configured: false, issuer: "", clientSecretSet: false },
      }),
    });
    renderRoute("/settings/configuration");

    expect(await screen.findByText("Not configured")).toBeVisible();
    expect(within(row("Issuer")).getByText("Not set")).toBeInTheDocument();
    expect(within(row("Client Secret")).getByText("Not set")).toBeInTheDocument();
  });

  it("lists session, token and platform settings", async () => {
    stubApi({ ...signedInAdmin, "GET settings/configuration": ok(configuration) });
    renderRoute("/settings/configuration");

    expect(await screen.findByText("Sessions & API Tokens")).toBeInTheDocument();
    expect(row("Session Idle Timeout")).toHaveTextContent("8 hours");
    expect(row("Session Max Age")).toHaveTextContent("7 days");
    expect(row("Secure Cookies")).toHaveTextContent("Off");
    expect(row("API Token Max Expiry")).toHaveTextContent("365 days");
    expect(row("API Token Prefix")).toHaveTextContent("gpp_");
    expect(row("Dialect")).toHaveTextContent("postgres");
    expect(row("Host")).toHaveTextContent("db.internal:5432");
    expect(row("Database")).toHaveTextContent("portal");
    expect(row("User")).toHaveTextContent("portal_app");
    expect(within(row("Password")).getByText("Not set")).toBeInTheDocument();
    expect(row("SSL Mode")).toHaveTextContent("require");
    expect(row("Environment")).toHaveTextContent("production");
    expect(row("Version")).toHaveTextContent("1.4.0");
    expect(row("HTTP Port")).toHaveTextContent("8000");
    expect(row("Metrics Port")).toHaveTextContent("2121");
    expect(row("Log Level")).toHaveTextContent("INFO");
  });

  it("copies the redirect URI", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAdmin, "GET settings/configuration": ok(configuration) });
    renderRoute("/settings/configuration");

    await user.click(await screen.findByRole("button", { name: "Copy Redirect URI" }));

    expect(await navigator.clipboard.readText()).toBe(
      "https://portal.example.com/api/v1/auth/callback",
    );
  });

  it("tests the connection and reports the result in a toast", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAdmin,
      "GET settings/configuration": ok(configuration),
      "POST settings/configuration/actions/oidc.check": ok({
        ok: false,
        message: "Couldn't load the identity provider's settings: connection refused",
      }),
    });
    renderRoute("/settings/configuration");

    await user.click(await screen.findByRole("button", { name: "Test connection" }));

    expect(
      await screen.findByText("Couldn't load the identity provider's settings: connection refused"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Test connection" })).toBeEnabled();
  });

  it("gives the SCIM endpoint to copy when SCIM is on", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAdmin,
      "GET settings/configuration": ok({
        ...configuration,
        scim: { enabled: true, url: "https://portal.example.com/scim/v2" },
      }),
    });
    renderRoute("/settings/configuration");

    expect(await screen.findByText("SCIM on")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Copy SCIM Endpoint" }));

    expect(await navigator.clipboard.readText()).toBe("https://portal.example.com/scim/v2");
  });
});
