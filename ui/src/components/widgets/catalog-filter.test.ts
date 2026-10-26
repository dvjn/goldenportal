import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import type { Entity } from "@/features/catalog";
import { toCatalogFilters } from "./catalog-filter";

const entity: Entity = {
  blueprint: "service",
  identifier: "ledger",
  title: "Ledger",
  properties: { owners: ["payments", "search"], coverage: 72, critical: true },
  related: {},
  integration: "registry",
  createdAt: "2026-10-25T00:00:00Z",
  updatedAt: "2026-10-25T00:00:00Z",
};

afterEach(() => vi.useRealTimers());

describe("widget click-through variables", () => {
  it("preserves scorecard tier filters in catalog links", () => {
    expect(
      toCatalogFilters({ property: "$scorecard.readiness", op: "in", values: ["gold", ""] }, {}),
    ).toEqual({ "$scorecard.readiness": ["gold", ""] });
  });

  it("preserves entity property types and expands many-valued relations", () => {
    const context = { entity: entity.identifier, entityData: entity };
    expect(
      toCatalogFilters(
        {
          and: [
            { property: "owner", op: "eq", value: "$entity.owners" },
            { property: "coverage", op: "eq", value: "$entity.coverage" },
            { property: "critical", op: "eq", value: "$entity.critical" },
          ],
        },
        context,
      ),
    ).toEqual({ owner: ["payments", "search"], coverage: [72], critical: [true] });
    expect(
      toCatalogFilters({ property: "name", op: "eq", value: "$entity.title" }, context),
    ).toEqual({ name: ["Ledger"] });
    expect(
      toCatalogFilters({ property: "owner", op: "eq", value: "$entity.missing" }, context),
    ).toBeUndefined();
    expect(
      toCatalogFilters({ property: "owner", op: "eq", value: "$entity.owners" }, {}),
    ).toBeUndefined();
  });

  it("uses UTC calendar dates, including relative dates across a leap day", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-03-01T01:00:00+05:30"));
    expect(
      toCatalogFilters(
        { property: "day", op: "in", values: ["$today", "$today-1d", "$today+1d"] },
        {},
      ),
    ).toEqual({ day: ["2024-02-29", "2024-02-28", "2024-03-01"] });
    expect(toCatalogFilters({ property: "day", op: "eq", value: "$today-1w" }, {})).toBeUndefined();
    expect(
      toCatalogFilters({ property: "day", op: "gte", value: "$today-7d" }, {}),
    ).toBeUndefined();
  });
});
