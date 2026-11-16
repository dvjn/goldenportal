import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { acmeConfig, ada, fail, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
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

const grace = { type: "user", id: "u2", name: "Grace Hopper", email: "grace@example.com" };
// The server's matches for "gr", people and groups ranked together.
const grMatches = [
  { type: "group", id: "g2", name: "graders", memberCount: 1 },
  grace,
  { type: "user", id: "u3", name: "Greta Garbo", email: "greta@example.com" },
  { type: "group", id: "g3", name: "green-team", memberCount: 12 },
  { type: "user", id: "u4", name: "Margret Ng", email: "margret@example.com" },
];

// subjectsByQuery answers subject searches from the matches given for each query.
function subjectsByQuery(matches: Record<string, unknown[]>) {
  return (request: Request) =>
    Response.json({ data: matches[new URL(request.url).searchParams.get("q") ?? ""] ?? [] });
}

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
    redirectUrl: "https://portal.example.com/api/web/auth/callback",
    bootstrapAdminEmail: "",
  },
  scim: { enabled: false, url: "https://portal.example.com/scim/v2" },
  sessions: { secureCookies: true, idleTimeoutHours: 8, maxAgeHours: 168 },
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
    retentionSchedule: "17 4 * * *",
    scorecardSchedule: "0 * * * *",
    indexSchedule: "41 * * * *",
  },
};

const row = (text: string) => screen.getByText(text).closest("tr") as HTMLElement;

describe("settings", () => {
  it("is only for admins", async () => {
    stubApi({ "GET config": acmeConfig, "GET me": ok({ ...ada, role: "builder" }) });
    renderRoute("/settings");

    expect(await screen.findByText("You don't have access to this page")).toBeInTheDocument();
  });

  it("opens settings from the sidebar", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAdmin, "GET settings/configuration": ok(serverConfiguration) });
    const { router } = renderRoute("/");

    await user.click(await screen.findByRole("link", { name: "Settings" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/configuration"));
    expect(screen.getByRole("heading", { level: 1, name: "Settings" })).toBeInTheDocument();
    const tabs = screen.getAllByRole("tab");
    expect(tabs[0]).toHaveTextContent("Server Configuration");
    expect(tabs[0]).toHaveAttribute("aria-selected", "true");
  });

  it("hides settings from everyone else", async () => {
    stubApi({ "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) });
    renderRoute("/");

    expect(await screen.findByRole("link", { name: "Self-Service" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Settings" })).not.toBeInTheDocument();
  });

  it("switches tabs through links", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAdmin, "GET settings/access": ok(grants), "GET settings/audit": ok([]) });
    const { router } = renderRoute("/settings/access");

    await user.click(await screen.findByRole("tab", { name: "Audit Log" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/audit"));
    expect(await screen.findByText("No events yet")).toBeInTheDocument();
  });
});

describe("access", () => {
  it("lists only direct grants, and not your own controls", async () => {
    stubApi({ ...signedInAdmin, "GET settings/access": ok(grants) });
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
      "GET settings/access": ok(grants),
      "PUT settings/access/group/g1": () => new Response(null, { status: 200 }),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("combobox", { name: "Role for platform-engineers" }));
    await user.click(screen.getByRole("option", { name: "Admin" }));

    await waitFor(() => expect(requests["PUT settings/access/group/g1"]).toBeDefined());
    expect(await requests["PUT settings/access/group/g1"]?.json()).toEqual({ role: "admin" });
    expect(await screen.findByText("platform-engineers is now admin")).toBeInTheDocument();
  });

  it("grants a person a role", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInAdmin,
      "GET settings/access": ok(grants),
      "GET settings/access/subjects": subjectsByQuery({ grace: [grace] }),
      "PUT settings/access/user/u2": () => new Response(null, { status: 200 }),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("button", { name: "Grant Access" }));
    await user.click(screen.getByRole("combobox", { name: "Person or group" }));
    await user.type(screen.getByRole("combobox", { name: "Search people and groups" }), "grace");
    await user.click(await screen.findByRole("option", { name: /Grace Hopper/ }));
    expect(screen.getByRole("combobox", { name: "Person or group" })).toHaveTextContent(
      "Grace Hopper",
    );
    await user.click(screen.getByRole("button", { name: "Grant Access" }));

    await waitFor(() => expect(requests["PUT settings/access/user/u2"]).toBeDefined());
    expect(await requests["PUT settings/access/user/u2"]?.json()).toEqual({ role: "builder" });
    expect(await screen.findByText("Granted builder to Grace Hopper")).toBeInTheDocument();
  });

  it("offers no one until something is typed", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInAdmin,
      "GET settings/access": ok(grants),
      "GET settings/access/subjects": subjectsByQuery({ gr: grMatches }),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("button", { name: "Grant Access" }));
    await user.click(screen.getByRole("combobox", { name: "Person or group" }));
    const search = screen.getByRole("combobox", { name: "Search people and groups" });
    await user.type(search, "   ");
    await new Promise((resolve) => setTimeout(resolve, 300));

    expect(screen.queryByRole("option")).not.toBeInTheDocument();
    expect(screen.queryByText(/Nobody matches/)).not.toBeInTheDocument();
    expect(requests["GET settings/access/subjects"]).toBeUndefined();
  });

  it("lists the server's matches in its order with their details, the first highlighted", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInAdmin,
      "GET settings/access": ok(grants),
      "GET settings/access/subjects": subjectsByQuery({ gr: grMatches }),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("button", { name: "Grant Access" }));
    await user.click(screen.getByRole("combobox", { name: "Person or group" }));
    await user.type(screen.getByRole("combobox", { name: "Search people and groups" }), "gr");

    await screen.findByRole("option", { name: /graders/ });
    const options = screen.getAllByRole("option");
    expect(options.map((option) => option.textContent)).toEqual([
      "graders1 member",
      "Grace Hoppergrace@example.com",
      "Greta Garbogreta@example.com",
      "green-team12 members",
      "Margret Ngmargret@example.com",
    ]);
    expect(options[0]).toHaveAttribute("aria-selected", "true");
    expect(new URL(requests["GET settings/access/subjects"]?.url ?? "").searchParams.get("q")).toBe(
      "gr",
    );
  });

  it("hides matches for earlier text, and says when nobody matches", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAdmin,
      "GET settings/access": ok(grants),
      "GET settings/access/subjects": subjectsByQuery({ gr: grMatches }),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("button", { name: "Grant Access" }));
    await user.click(screen.getByRole("combobox", { name: "Person or group" }));
    const search = screen.getByRole("combobox", { name: "Search people and groups" });
    await user.type(search, "gr");
    await screen.findByRole("option", { name: /graders/ });
    await user.type(search, "z");

    expect(screen.queryByRole("option")).not.toBeInTheDocument();
    expect(
      await screen.findByText("Nobody matches. People appear after they first sign in."),
    ).toBeInTheDocument();
  });

  it("says it's searching until the server answers", async () => {
    const user = userEvent.setup();
    let answer = () => {};
    stubApi({
      ...signedInAdmin,
      "GET settings/access": ok(grants),
      "GET settings/access/subjects": () =>
        new Promise<Response>((resolve) => {
          answer = () => resolve(Response.json({ data: [grace] }));
        }),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("button", { name: "Grant Access" }));
    await user.click(screen.getByRole("combobox", { name: "Person or group" }));
    await user.type(screen.getByRole("combobox", { name: "Search people and groups" }), "grace");

    expect(await screen.findByText("Searching…")).toBeInTheDocument();
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(screen.getByText("Searching…")).toBeInTheDocument();
    answer();
    expect(await screen.findByRole("option", { name: /Grace Hopper/ })).toBeInTheDocument();
    expect(screen.queryByText("Searching…")).not.toBeInTheDocument();
  });

  it("shows why a search failed", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAdmin,
      "GET settings/access": ok(grants),
      "GET settings/access/subjects": fail(403, "this needs the admin role"),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("button", { name: "Grant Access" }));
    await user.click(screen.getByRole("combobox", { name: "Person or group" }));
    await user.type(screen.getByRole("combobox", { name: "Search people and groups" }), "grace");

    expect(await screen.findByText("this needs the admin role")).toBeInTheDocument();
    expect(screen.queryByRole("option")).not.toBeInTheDocument();
  });

  it("removes a grant after confirming", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAdmin, "GET settings/access": ok(grants) });
    renderRoute("/settings/access");

    await user.click(
      await screen.findByRole("button", { name: "Remove access for platform-engineers" }),
    );
    const requests = stubApi({
      ...signedInAdmin,
      "GET settings/access": ok([grants[0]]),
      "DELETE settings/access/group/g1": () => new Response(null, { status: 204 }),
    });
    await user.click(screen.getByRole("button", { name: "Remove access" }));

    expect(await screen.findByText("Removed access for platform-engineers")).toBeInTheDocument();
    expect(requests["DELETE settings/access/group/g1"]).toBeDefined();
  });

  it("tells the admin when a change fails", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAdmin,
      "GET settings/access": ok(grants),
      "PUT settings/access/group/g1": fail(500, "the database is unavailable"),
    });
    renderRoute("/settings/access");

    await user.click(await screen.findByRole("combobox", { name: "Role for platform-engineers" }));
    await user.click(screen.getByRole("option", { name: "Member" }));

    expect(
      await screen.findByText("Something went wrong. the database is unavailable"),
    ).toBeInTheDocument();
  });
});
