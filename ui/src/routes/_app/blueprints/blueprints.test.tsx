import { screen, waitFor, within } from "@testing-library/react";
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
  it("lists blueprints with their status", async () => {
    stubApi({
      ...signedInBuilder,
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
    expect(within(row).getByText("Version 2")).toBeInTheDocument();
    expect(within(row).getByText("Draft")).toBeInTheDocument();
  });

  it("keeps members out", async () => {
    stubApi({ "GET config": acmeConfig, "GET me": ok({ ...ada, role: "member" }) });
    const { router } = renderRoute("/blueprints");

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
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
    expect(await screen.findByRole("link", { name: "deployment" })).toBeInTheDocument();
    expect(await screen.findByText("12")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Service Registry" })).toHaveAttribute(
      "href",
      "/integrations/registry",
    );
    expect(screen.getByText("$identifier")).toBeInTheDocument();
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
