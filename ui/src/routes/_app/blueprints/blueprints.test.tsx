import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Blueprint, BlueprintSpec } from "@/features/blueprints";
import type { Impact } from "@/features/config-docs";
import { acmeConfig, ada, ok, stubApi } from "@/test/api";
import { renderRoute } from "@/test/render";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const signedInBuilder = { "GET config": acmeConfig, "GET me": ok({ ...ada, role: "builder" }) };

const spec: BlueprintSpec = {
  identifier: "service",
  title: "Service",
  properties: [
    { identifier: "tier", title: "Tier", type: "string", required: true, enum: ["gold", "silver"] },
    { identifier: "owner", title: "Owner", type: "relation", relation: "team" },
    {
      identifier: "critical",
      title: "Critical",
      type: "boolean",
      compute: '.properties.tier == "gold"',
    },
  ],
};

const source = "identifier: service\ntitle: Service\n";

const service: Blueprint = {
  identifier: "service",
  title: "Service",
  published: {
    version: 1,
    source,
    spec,
    publishedAt: "2026-10-24T10:00:00Z",
    publishedBy: "ada@example.com",
  },
  draft: {
    source: `${source}description: Draft\n`,
    spec,
    issues: [],
    updatedAt: "2026-10-24T11:00:00Z",
    updatedBy: "ada@example.com",
  },
  createdAt: "2026-10-24T10:00:00Z",
};

const breakingImpact: Impact = {
  firstVersion: false,
  changes: [
    {
      section: "properties",
      identifier: "stars",
      change: "removed",
      breaking: true,
      detail: "removes it and the values entities have for it",
    },
  ],
  breaking: true,
  affected: 3,
  issues: [],
};

const serviceRoutes = {
  ...signedInBuilder,
  "GET blueprints/service": ok(service),
  "GET blueprints/service/versions": ok([
    { version: 1, publishedAt: "2026-10-24T10:00:00Z", publishedBy: "ada@example.com" },
  ]),
  "GET blueprints/service/usage": ok({
    entities: 12,
    writers: [
      {
        integration: "registry",
        title: "Service Registry",
        entities: 12,
        mapped: true,
        lastRunAt: "2026-10-24T10:00:00Z",
      },
    ],
    references: [
      {
        blueprint: "deployment",
        title: "Deployment",
        property: "service",
        propertyTitle: "Service",
        many: false,
      },
    ],
  }),
};

describe("blueprints", () => {
  it("lists blueprints with their status and entities", async () => {
    stubApi({
      ...signedInBuilder,
      "GET catalog": ok([{ identifier: "service", title: "Service", entities: 12 }]),
      "GET blueprints": ok([
        {
          identifier: "service",
          title: "Service",
          publishedVersion: 2,
          hasDraft: true,
          stats: { properties: 3, relations: 1 },
          updatedAt: "2026-10-24T10:00:00Z",
        },
      ]),
    });
    renderRoute("/blueprints");

    const row = (await screen.findByText("Service")).closest("tr") as HTMLElement;
    // Published, with a draft waiting.
    expect(within(row).getByText("Published")).toBeInTheDocument();
    expect(within(row).getByText("Draft")).toBeInTheDocument();
    expect(await within(row).findByRole("cell", { name: "12" })).toBeInTheDocument();
    // The primary action leads the page, not the table.
    expect(
      screen.getByRole("link", { name: "New Blueprint" }).closest("[data-slot=card]"),
    ).toBeNull();
  });

  it("keeps members out", async () => {
    stubApi({ "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) });
    const { router } = renderRoute("/blueprints");

    // They stay on the page, told why, with the navigation still there.
    expect(await screen.findByText("You don't have access to this page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/blueprints");
    expect(screen.getByRole("link", { name: "Home" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Blueprints" })).not.toBeInTheDocument();
  });

  it("shows the published blueprint read-only", async () => {
    stubApi(serviceRoutes);
    renderRoute("/blueprints/service");

    const tier = (await screen.findByText("Tier")).closest("tr") as HTMLElement;
    expect(within(tier).getByText("Required")).toBeInTheDocument();
    expect(within(tier).getByText("gold")).toBeInTheDocument();
    expect(within(tier).getByText("Stored")).toBeInTheDocument();
    const owner = screen.getByText("Owner").closest("tr") as HTMLElement;
    expect(within(owner).getByRole("link", { name: /team/ })).toHaveAttribute(
      "href",
      "/blueprints/team",
    );
    const critical = screen.getByText("Critical").closest("tr") as HTMLElement;
    expect(within(critical).getByText("Computed")).toBeInTheDocument();
    expect(await screen.findByText("There's an unpublished draft")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View in Catalog" })).toHaveAttribute(
      "href",
      "/catalog/service",
    );
    const about = within(
      (await screen.findByText("About")).closest("[data-slot=card]") as HTMLElement,
    );
    expect(about.getByText("Entities").nextElementSibling).toHaveTextContent("12");
    expect(screen.getByRole("link", { name: "Service Registry" })).toHaveAttribute(
      "href",
      "/integrations/registry",
    );
    expect(screen.getByText("updatedAt")).toBeInTheDocument();
  });

  it("puts the trail in the page bar, and the header once it's scrolled past", async () => {
    const user = userEvent.setup();
    // jsdom can't scroll, so the test reports the header passing under the bar.
    let scrolledPast: (past: boolean) => void = () => {};
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(callback: IntersectionObserverCallback) {
          scrolledPast = (past) =>
            callback(
              [
                {
                  isIntersecting: !past,
                  boundingClientRect: { top: past ? -200 : 80 },
                } as IntersectionObserverEntry,
              ],
              this as unknown as IntersectionObserver,
            );
        }
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
    stubApi(serviceRoutes);
    const { router } = renderRoute("/blueprints/service");

    expect(await screen.findByRole("heading", { level: 1, name: "Service" })).toBeVisible();
    const bar = document.querySelector<HTMLElement>('[data-slot="page-bar"]') as HTMLElement;
    const crumbs = within(bar).getByRole("navigation", { name: "breadcrumb" });
    expect(within(crumbs).getByRole("link", { name: "Blueprints" })).toHaveAttribute(
      "href",
      "/blueprints",
    );
    expect(within(crumbs).getByRole("link", { name: "Service" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(within(bar).queryByRole("tab")).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");

    // Scrolled past, the bar trades the trail for the title, tabs and
    // actions, and the page's own tabs step aside.
    act(() => scrolledPast(true));
    expect(bar).toHaveAttribute("data-collapsed");
    expect(within(bar).queryByRole("navigation", { name: "breadcrumb" })).not.toBeInTheDocument();
    const titles = within(bar).getAllByText("Service");
    expect(titles.filter((title) => title.closest("[hidden]") === null)).toHaveLength(1);
    expect(within(bar).getByRole("link", { name: "Export" })).toBeInTheDocument();
    expect(screen.getAllByRole("tab")).toHaveLength(3);
    await user.click(within(bar).getByRole("tab", { name: "Versions" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "versions" }));
    expect(within(bar).getByRole("tab", { name: "Versions" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    act(() => scrolledPast(false));
    expect(bar).not.toHaveAttribute("data-collapsed");
    expect(within(bar).getByRole("navigation", { name: "breadcrumb" })).toBeInTheDocument();
    expect(within(bar).queryByRole("tab")).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Versions" })).toHaveAttribute("aria-selected", "true");
  });

  it("edits the draft on the definition tab", async () => {
    stubApi(serviceRoutes);
    renderRoute("/blueprints/service?tab=definition");

    expect(await screen.findByRole("textbox", { name: "YAML" })).toHaveValue(service.draft?.source);
  });

  it("needs breaking changes acknowledged before publishing", async () => {
    const user = userEvent.setup();
    const requests = stubApi({
      ...serviceRoutes,
      "GET blueprints/service/draft/impact": ok(breakingImpact),
      "POST blueprints/service/draft/publish": ok({ version: 2 }),
    });
    renderRoute("/blueprints/service?tab=definition");

    await user.click(await screen.findByRole("button", { name: "Publish" }));
    const publish = await screen.findByRole("button", { name: "Publish Version" });
    expect(await screen.findByText("Breaking")).toBeInTheDocument();
    expect(publish).toBeDisabled();

    await user.click(screen.getByRole("checkbox"));
    await user.click(publish);

    expect(await screen.findByText("Published service version 2")).toBeInTheDocument();
    expect(await requests["POST blueprints/service/draft/publish"].json()).toEqual({
      acknowledgeBreaking: true,
    });
  });

  it("deletes only once the identifier is typed", async () => {
    const user = userEvent.setup();
    stubApi(serviceRoutes);
    renderRoute("/blueprints/service");

    await user.click(await screen.findByRole("button", { name: "Delete" }));
    const confirm = await screen.findByRole("button", { name: "Delete Blueprint" });
    expect(confirm).toBeDisabled();
    await user.type(screen.getByLabelText(/to confirm/), "service");
    expect(confirm).toBeEnabled();
  });
});
