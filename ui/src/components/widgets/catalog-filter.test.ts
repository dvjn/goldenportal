import { describe, expect, it } from "vite-plus/test";
import { toCatalogLink } from "./catalog-filter";

describe("widget links into the catalog", () => {
  it("sets the table's filters to the values a link compares with", () => {
    expect(
      toCatalogLink({
        and: [
          { property: "owner", op: "in", values: ["payments", "search"] },
          { property: "coverage", op: "eq", value: 72 },
          { property: "readiness", op: "in", values: ["gold", ""] },
        ],
      }),
    ).toEqual({
      f: { owner: ["payments", "search"], coverage: [72], readiness: ["gold", ""] },
    });
    expect(toCatalogLink(undefined)).toEqual({});
  });

  it("links comparisons with a day as a range of days", () => {
    expect(
      toCatalogLink({
        and: [
          { property: "status", op: "eq", value: "failed" },
          { property: "startedAt", op: "gte", value: "2024-02-23" },
          { property: "startedAt", op: "lt", value: "2024-03-01" },
        ],
      }),
    ).toEqual({
      f: { status: ["failed"] },
      d: { startedAt: { from: "2024-02-23", to: "2024-02-29" } },
    });
  });

  it("links a within as the table's own relative date filter", () => {
    const later = { property: "startedAt", op: "gte", value: "2024-02-23" };
    expect(
      toCatalogLink({ and: [{ property: "startedAt", op: "within", value: "7d" }, later] }),
    ).toEqual({ d: { startedAt: { within: "7d" } }, where: later });
  });

  it("keeps what the table's filters can't say as where", () => {
    const owner = { property: "service.owner", op: "in", values: ["payments"] };
    const at = { property: "startedAt", op: "gt", value: "2024-01-31T10:00:00Z" };
    expect(
      toCatalogLink({ and: [{ property: "status", op: "eq", value: "failed" }, owner, at] }),
    ).toEqual({ f: { status: ["failed"] }, where: { and: [owner, at] } });
    const either = { or: [{ property: "tier", op: "eq", value: "critical" }] };
    expect(toCatalogLink(either)).toEqual({ where: either });
    const team = { property: "identifier", op: "in", values: ["payments"] };
    expect(toCatalogLink(team)).toEqual({ where: team });
    const recent = { property: "service.startedAt", op: "within", value: "7d" };
    expect(toCatalogLink(recent)).toEqual({ where: recent });
    // Fields a path reaches through a scorecard aren't the table's either.
    const failing = { property: "readiness.failed", op: "gt", value: 0 };
    expect(toCatalogLink(failing)).toEqual({ where: failing });
    // A property filtered twice keeps its first values, and the rest narrow too.
    const second = { property: "tier", op: "eq", value: "critical" };
    expect(
      toCatalogLink({
        and: [{ property: "tier", op: "in", values: ["critical", "standard"] }, second],
      }),
    ).toEqual({ f: { tier: ["critical", "standard"] }, where: second });
  });

  it("has no link for a filter set to nothing, which the catalog can't send", () => {
    expect(
      toCatalogLink({
        and: [
          { property: "owner", op: "in" },
          { property: "tier", op: "eq", value: "a" },
        ],
      }),
    ).toBeUndefined();
    expect(toCatalogLink({ not: { property: "owner", op: "in", values: [] } })).toBeUndefined();
  });
});
