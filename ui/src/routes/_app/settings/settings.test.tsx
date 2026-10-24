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

const admin = { ...ada, role: "admin" };
const signedInAdmin = { "GET config": acmeConfig, "GET me": ok(admin) };
const now = new Date().toISOString();

const grants = [
  {
    subjectType: "user",
    subjectId: ada.id,
    name: "Ada Lovelace",
    email: ada.email,
    role: "admin",
    grantedAt: now,
  },
  {
    subjectType: "group",
    subjectId: "g1",
    name: "platform-engineers",
    memberCount: 3,
    role: "builder",
    grantedAt: now,
  },
];

const users = [
  { id: ada.id, displayName: "Ada Lovelace", email: ada.email },
  { id: "u2", displayName: "Grace Hopper", email: "grace@example.com" },
];
const groups = [{ id: "g1", name: "platform-engineers", memberCount: 3 }];

const serverConfiguration = {
  portal: {
    name: "Acme",
    logoUrl: "",
    faviconUrl: "",
    faviconIsLogo: true,
    baseUrl: "https://portal.example.com",
  },
  oidc: {
    configured: true,
    issuer: "https://idp.example.com",
    clientId: "portal",
    clientSecretSet: true,
    scopes: ["openid"],
    groupsClaim: "groups",
    redirectUrl: "https://portal.example.com/api/v1/auth/callback",
    bootstrapAdminEmail: "",
  },
  scim: { enabled: false, url: "https://portal.example.com/scim/v2" },
  sessions: { secureCookies: true, idleTimeoutHours: 8, maxAgeHours: 168 },
  tokens: { prefix: "gpp_", maxDays: 365 },
  database: {
    dialect: "postgres",
    host: "db",
    port: "5432",
    name: "portal",
    user: "portal",
    passwordSet: true,
    sslMode: "require",
  },
  server: {
    environment: "production",
    version: "1.0.0",
    httpPort: "8000",
    metricsPort: "2121",
    logLevel: "INFO",
  },
};

const row = (text: string) => screen.getByText(text).closest("tr") as HTMLElement;

describe("settings", () => {
  it("is only for admins", async () => {
    stubApi({ "GET config": acmeConfig, "GET me": ok({ ...ada, role: "builder" }) });
    const { router } = renderRoute("/settings");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
  });

  it("opens on the server configuration tab from the user menu", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAdmin, "GET settings/configuration": ok(serverConfiguration) });
    const { router } = renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "Open user menu" }));
    await user.click(screen.getByRole("menuitem", { name: "Settings" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/configuration"));
    expect(screen.getByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument();
    const tabs = screen.getAllByRole("tab");
    expect(tabs[0]).toHaveTextContent("Server Configuration");
    expect(tabs[0]).toHaveAttribute("aria-selected", "true");
  });

  it("hides the settings link from everyone else", async () => {
    const user = userEvent.setup();
    stubApi({ "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) });
    renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "Open user menu" }));

    expect(screen.queryByRole("menuitem", { name: "Settings" })).not.toBeInTheDocument();
  });

  it("switches tabs through links", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAdmin, "GET access": ok(grants), "GET audit": ok([]) });
    const { router } = renderRoute("/settings/access");

    await user.click(await screen.findByRole("tab", { name: "Audit Log" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/audit"));
    expect(await screen.findByText("Nothing recorded yet")).toBeInTheDocument();
  });
});

describe("access", () => {
  it("lists only direct grants, and not your own controls", async () => {
    stubApi({ ...signedInAdmin, "GET access": ok(grants) });
    renderRoute("/settings/access");

    await screen.findByText("platform-engineers");
    expect(within(row("platform-engineers")).getByText("Group · 3 members")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Role for platform-engineers" })).toHaveTextContent(
      "Builder",
    );
    expect(screen.getByRole("combobox", { name: "Role for Ada Lovelace" })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Remove access for Ada Lovelace" }),
    ).not.toBeInTheDocument();
  });

  it("changes a group's role in place", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInAdmin,
      "GET access": ok(grants),
      "PUT access/group/g1": () => new Response(null, { status: 200 }),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("combobox", { name: "Role for platform-engineers" }));
    await user.click(screen.getByRole("option", { name: "Admin" }));

    await waitFor(() => expect(requests["PUT access/group/g1"]).toBeDefined());
    expect(await requests["PUT access/group/g1"]?.json()).toEqual({ role: "admin" });
    expect(await screen.findByText("platform-engineers is now admin")).toBeInTheDocument();
  });

  it("grants a person a role", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInAdmin,
      "GET access": ok(grants),
      "GET users": ok(users),
      "GET groups": ok(groups),
      "PUT access/user/u2": () => new Response(null, { status: 200 }),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("button", { name: "Grant access" }));
    await user.click(screen.getByRole("combobox", { name: "Person or group" }));
    expect(screen.queryByRole("option", { name: /Ada Lovelace/ })).not.toBeInTheDocument();
    await user.click(await screen.findByRole("option", { name: /Grace Hopper/ }));
    await user.click(screen.getByRole("button", { name: "Grant access" }));

    await waitFor(() => expect(requests["PUT access/user/u2"]).toBeDefined());
    expect(await requests["PUT access/user/u2"]?.json()).toEqual({ role: "builder" });
    expect(await screen.findByText("Granted builder to Grace Hopper")).toBeInTheDocument();
  });

  it("removes a grant after confirming", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAdmin, "GET access": ok(grants) });
    renderRoute("/settings/access");

    await user.click(
      await screen.findByRole("button", { name: "Remove access for platform-engineers" }),
    );
    const requests = stubApi({
      ...signedInAdmin,
      "GET access": ok([grants[0]]),
      "DELETE access/group/g1": () => new Response(null, { status: 204 }),
    });
    await user.click(screen.getByRole("button", { name: "Remove access" }));

    expect(await screen.findByText("Removed access for platform-engineers")).toBeInTheDocument();
    expect(requests["DELETE access/group/g1"]).toBeDefined();
  });

  it("tells the admin when a change fails", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAdmin,
      "GET access": ok(grants),
      "PUT access/group/g1": fail(500, "the database is unavailable"),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("combobox", { name: "Role for platform-engineers" }));
    await user.click(screen.getByRole("option", { name: "Member" }));

    expect(await screen.findByText("the database is unavailable")).toBeInTheDocument();
  });
});
