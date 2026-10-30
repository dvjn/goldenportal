import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
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

const event = (id: number, action: string, details: Record<string, unknown> = {}) => ({
  id,
  at: new Date().toISOString(),
  actor: { id: "1", label: "ada@example.com" },
  action,
  target: { type: "group", id: "g1", label: "platform-engineers" },
  details,
});

// auditApi answers GET audit and records the query of every request.
function auditApi(respond: (params: URLSearchParams) => unknown) {
  const queries: URLSearchParams[] = [];
  stubApi({
    ...signedInAdmin,
    "GET audit": (request) => {
      const params = new URL(request.url).searchParams;
      queries.push(params);
      return Response.json({ data: respond(params) });
    },
  });
  return queries;
}

describe("audit log", () => {
  it("describes each event in plain words", async () => {
    auditApi(() => ({
      events: [event(2, "access.grant", { role: "builder" }), event(1, "auth.sign_in")],
      total: 2,
    }));
    renderRoute("/settings/audit");

    expect(await screen.findByText("Granted access")).toBeInTheDocument();
    expect(screen.getByText("Role: Builder")).toBeInTheDocument();
    expect(screen.getByText("Signed in")).toBeInTheDocument();
    expect(screen.getByText("Showing 1 to 2 of 2")).toBeInTheDocument();
  });

  it("asks the server for the next page", async () => {
    const user = userEvent.setup();
    const queries = auditApi((params) => ({
      events: [event(params.get("page") === "2" ? 1 : 2, "auth.sign_in")],
      total: 15,
    }));
    const { router } = renderRoute("/settings/audit");

    expect(await screen.findByText("Page 1 of 2")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Next page" }));

    await waitFor(() => expect(router.state.location.search).toMatchObject({ page: 2 }));
    await waitFor(() => expect(queries.at(-1)?.get("page")).toBe("2"));
    expect(await screen.findByText("Page 2 of 2")).toBeInTheDocument();
  });

  it("filters by action through the URL", async () => {
    const user = userEvent.setup();
    const queries = auditApi(() => ({ events: [event(1, "token.create")], total: 1 }));
    const { router } = renderRoute("/settings/audit?page=2");

    await user.click(await screen.findByRole("button", { name: "Action" }));
    await user.click(await screen.findByRole("option", { name: "Created API token" }));
    await user.click(screen.getByRole("option", { name: "Signed in" }));

    await waitFor(() =>
      expect(router.state.location.search).toEqual({ action: "auth.sign_in,token.create" }),
    );
    await waitFor(() => expect(queries.at(-1)?.get("action")).toBe("auth.sign_in,token.create"));
  });

  it("filters by date with a preset", async () => {
    const user = userEvent.setup();
    const queries = auditApi(() => ({ events: [event(1, "auth.sign_in")], total: 1 }));
    const { router } = renderRoute("/settings/audit");

    await user.click(await screen.findByRole("button", { name: "Date" }));
    await user.click(screen.getByRole("button", { name: "Today" }));

    const today = new Date();
    const iso = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
    await waitFor(() => expect(router.state.location.search).toEqual({ from: iso, to: iso }));
    await waitFor(() => expect(queries.at(-1)?.get("from")).toBe(iso));
  });

  it("restores filters from the URL and offers to clear them", async () => {
    const user = userEvent.setup();
    const queries = auditApi((params) =>
      params.get("action")
        ? { events: [], total: 0 }
        : { events: [event(1, "auth.sign_in")], total: 1 },
    );
    const { router } = renderRoute("/settings/audit?action=token.revoke&q=ci");

    expect(await screen.findByText(/Nothing matches "ci"/)).toBeInTheDocument();
    expect(queries[0]?.get("q")).toBe("ci");
    expect(queries[0]?.get("action")).toBe("token.revoke");

    await user.click(screen.getByRole("button", { name: "Reset" }));

    await waitFor(() => expect(router.state.location.search).toEqual({}));
    expect(await screen.findByText("Signed in")).toBeInTheDocument();
  });

  it("explains an empty log", async () => {
    auditApi(() => ({ events: [], total: 0 }));
    renderRoute("/settings/audit");

    expect(await screen.findByText("Nothing recorded yet")).toBeInTheDocument();
  });
});
