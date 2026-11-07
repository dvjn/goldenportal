import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Available, RowActions } from "@/features/actions";
import type { Blueprint } from "@/features/blueprints";
import type { Entity } from "@/features/catalog";
import type { HomeDashboard } from "@/features/dashboards";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/web") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const member = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) };

const service: Blueprint = {
  identifier: "service",
  title: "Service",
  published: {
    version: 1,
    source: "",
    spec: { identifier: "service", title: "Service", properties: [] },
    publishedAt: "2026-10-24T10:00:00Z",
    publishedBy: "",
  },
  draft: null,
  createdAt: "2026-10-24T10:00:00Z",
};

const entity = (identifier: string, title: string): Entity => ({
  blueprint: "service",
  identifier,
  title,
  properties: {},
  related: {},
  integration: "registry",
  createdAt: "2026-10-24T10:00:00Z",
  updatedAt: "2026-10-24T10:00:00Z",
});

const restart: Available = {
  identifier: "restart",
  title: "Restart",
  inputs: [{ identifier: "reason", title: "Reason", type: "string", required: true }],
};
const scale: Available = { identifier: "scale", title: "Scale", inputs: [] };

// An identifier with a comma, which a comma-joined list would split.
const rowActions: RowActions = {
  actions: [restart, scale],
  entities: { ledger: ["restart", "scale"], "a,b": ["scale"] },
};

describe("row actions", () => {
  it("asks once for a catalog page's rows and offers each row its own", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...member,
      "GET blueprints/service": ok(service),
      "GET catalog/service": ok({
        items: [entity("ledger", "Ledger"), entity("a,b", "Odd One"), entity("idle", "Idle")],
        total: 3,
      }),
      "GET catalog/service/facets": ok([]),
      "GET catalog/service/row-actions": ok(rowActions),
    });
    renderRoute("/catalog/service");

    const menu = await screen.findByRole("button", { name: "Actions for Ledger" });
    const url = new URL(requests["GET catalog/service/row-actions"].url);
    expect(url.searchParams.getAll("entity")).toEqual(["ledger", "a,b", "idle"]);
    expect(screen.getByRole("button", { name: "Actions for Odd One" })).toBeInTheDocument();
    // A row with nothing to run has no menu.
    expect(screen.queryByRole("button", { name: "Actions for Idle" })).not.toBeInTheDocument();

    await user.click(menu);
    await user.click(screen.getByRole("menuitem", { name: "Restart" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Ledger")).toBeInTheDocument();
    expect(within(dialog).getByLabelText(/^Reason/)).toBeInTheDocument();
  });

  it("offers them on a table widget's entity rows, but not on rows that open elsewhere", async () => {
    const table = (identifier: string, rowLink?: { url: string }): HomeDashboard["widgets"][0] => ({
      identifier,
      type: "table",
      title: identifier,
      width: 12,
      query: { blueprint: "service" },
      ...(rowLink ? { rowLink } : {}),
    });
    const rows = {
      blueprint: "service",
      total: 1,
      columns: [],
      rows: [{ identifier: "ledger", title: "Ledger", values: [] }],
    };
    const requests = stubApi({
      ...member,
      "GET home": ok([
        {
          identifier: "overview",
          title: "Overview",
          order: 1,
          inputs: [],
          defaults: {},
          widgets: [
            table("entities"),
            table("linked", { url: "https://example.com/{{ identifier }}" }),
          ],
        },
      ]),
      "GET dashboards/overview/data": ok({
        widgets: {
          entities: { data: rows },
          linked: {
            data: {
              ...rows,
              rows: [{ ...rows.rows[0], link: { href: "https://example.com/ledger" } }],
            },
          },
        },
      }),
      "GET catalog/service/row-actions": ok(rowActions),
    });
    renderRoute("/");

    expect(await screen.findAllByRole("button", { name: "Actions for Ledger" })).toHaveLength(1);
    await waitFor(() =>
      expect(
        new URL(requests["GET catalog/service/row-actions"].url).searchParams.getAll("entity"),
      ).toEqual(["ledger"]),
    );
  });
});
