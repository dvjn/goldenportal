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

const users = [
  {
    ...admin,
    status: "active",
    lastLoginAt: new Date().toISOString(),
    role: "admin",
    effectiveRole: "admin",
    groups: [],
  },
  {
    id: "2",
    email: "grace@example.com",
    displayName: "Grace Hopper",
    status: "active",
    lastLoginAt: null,
    role: null,
    effectiveRole: "builder",
    groups: ["platform-engineers"],
  },
];

const groups = [
  { id: "g1", name: "platform-engineers", isTeam: false, memberCount: 3, role: null },
];

const row = (name: string) => screen.getByText(name).closest("tr") as HTMLElement;

describe("settings", () => {
  it("is only for admins", async () => {
    stubApi({ "GET config": acmeConfig, "GET me": ok({ ...ada, role: "builder" }) });
    const { router } = renderRoute("/settings");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
  });

  it("shows admins the settings link in the user menu", async () => {
    const user = userEvent.setup();
    stubApi({ ...signedInAdmin, "GET users": ok(users) });
    const { router } = renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "Open user menu" }));
    await user.click(screen.getByRole("menuitem", { name: "Settings" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/users"));
  });

  it("hides the settings link from everyone else", async () => {
    const user = userEvent.setup();
    stubApi({ "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) });
    renderRoute("/");

    await user.click(await screen.findByRole("button", { name: "Open user menu" }));

    expect(screen.queryByRole("menuitem", { name: "Settings" })).not.toBeInTheDocument();
  });

  it("lists users with their groups and roles", async () => {
    stubApi({ ...signedInAdmin, "GET users": ok(users) });
    renderRoute("/settings/users");

    await screen.findByText("Grace Hopper");
    const grace = row("Grace Hopper");
    expect(within(grace).getByText("platform-engineers")).toBeInTheDocument();
    expect(
      within(grace).getByRole("combobox", { name: "Role for Grace Hopper" }),
    ).toHaveTextContent("From groups");
    expect(within(grace).getByText("Builder")).toBeInTheDocument();
    expect(within(grace).getByText("Never")).toBeInTheDocument();

    expect(screen.getByRole("combobox", { name: "Role for Ada Lovelace" })).toBeDisabled();
  });

  it("sets a user's role", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInAdmin,
      "GET users": ok(users),
      "PUT users/2/role": () => new Response(null, { status: 200 }),
    });
    renderRoute("/settings/users");

    await user.click(await screen.findByRole("combobox", { name: "Role for Grace Hopper" }));
    await user.click(screen.getByRole("option", { name: "Admin" }));

    await waitFor(() => expect(requests["PUT users/2/role"]).toBeDefined());
    expect(await requests["PUT users/2/role"]?.json()).toEqual({ role: "admin" });
  });

  it("gives a group a role and marks it as a team", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...signedInAdmin,
      "GET groups": ok(groups),
      "PUT groups/g1": () => new Response(null, { status: 200 }),
    });
    renderRoute("/settings/groups");

    await user.click(await screen.findByRole("combobox", { name: "Role for platform-engineers" }));
    await user.click(screen.getByRole("option", { name: "Builder" }));
    await waitFor(() => expect(requests["PUT groups/g1"]).toBeDefined());
    expect(await requests["PUT groups/g1"]?.json()).toEqual({ role: "builder", isTeam: false });

    await user.click(screen.getByRole("switch", { name: "platform-engineers is a team" }));
    await waitFor(async () =>
      expect(await requests["PUT groups/g1"]?.clone().json()).toEqual({
        role: null,
        isTeam: true,
      }),
    );
  });

  it("tells the admin when a change fails", async () => {
    const user = userEvent.setup();
    stubApi({
      ...signedInAdmin,
      "GET groups": ok(groups),
      "PUT groups/g1": fail(500, "the database is unavailable"),
    });
    renderRoute("/settings/groups");

    await user.click(await screen.findByRole("switch", { name: "platform-engineers is a team" }));

    expect(await screen.findByText("the database is unavailable")).toBeInTheDocument();
  });

  it("explains where groups come from when there are none", async () => {
    stubApi({ ...signedInAdmin, "GET groups": ok([]) });
    renderRoute("/settings/groups");

    expect(await screen.findByText("No groups yet")).toBeInTheDocument();
  });
});
