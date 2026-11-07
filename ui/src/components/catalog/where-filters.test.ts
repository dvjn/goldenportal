import { describe, expect, it } from "vite-plus/test";
import type { Filter } from "@/features/catalog";
import { chipsOf, dateFilter, optionsFilter, replaceConditions } from "./where-filters";

const midnight = (day: string) => new Date(`${day}T00:00:00`).toISOString();

describe("filter chips", () => {
  it("makes one editable chip for each condition the menu can say, in order", () => {
    const filter: Filter[] = [
      { property: "tier", op: "in", values: ["critical", "standard"] },
      { property: "owner", op: "ne", value: "payments" },
      { not: { property: "pipeline", op: "in", values: ["none", "manual"] } },
      { property: "docs", op: "exists" },
      { not: { property: "slack", op: "exists" } },
      { property: "startedAt", op: "within", value: "7d" },
    ];
    expect(chipsOf(filter)).toEqual([
      {
        kind: "options",
        indices: [0],
        property: "tier",
        values: ['"critical"', '"standard"'],
        negated: false,
      },
      { kind: "options", indices: [1], property: "owner", values: ['"payments"'], negated: true },
      {
        kind: "options",
        indices: [2],
        property: "pipeline",
        values: ['"none"', '"manual"'],
        negated: true,
      },
      { kind: "options", indices: [3], property: "docs", values: [], negated: false },
      { kind: "options", indices: [4], property: "slack", values: [], negated: true },
      { kind: "date", indices: [5], property: "startedAt", range: { within: "7d" } },
    ]);
  });

  it("joins the two ends of a range of days into one chip", () => {
    const filter: Filter[] = [
      { property: "updatedAt", op: "gte", value: midnight("2026-10-01") },
      { property: "tier", op: "eq", value: "critical" },
      { property: "updatedAt", op: "lt", value: midnight("2026-10-16") },
    ];
    expect(chipsOf(filter)[0]).toEqual({
      kind: "date",
      indices: [0, 2],
      property: "updatedAt",
      range: { from: "2026-10-01", to: "2026-10-15" },
    });
    expect(chipsOf(filter)).toHaveLength(2);
    // Days written alone read the same.
    expect(
      chipsOf([
        { property: "startedAt", op: "gt", value: "2026-10-01" },
        { property: "startedAt", op: "lte", value: "2026-10-20" },
      ]),
    ).toEqual([
      {
        kind: "date",
        indices: [0, 1],
        property: "startedAt",
        range: { from: "2026-10-02", to: "2026-10-20" },
      },
    ]);
  });

  it("fixes what the menu can't say", () => {
    const or: Filter = { or: [{ property: "tier", op: "eq", value: "critical" }] };
    const nested: Filter = { not: { and: [{ property: "tier", op: "eq", value: "critical" }] } };
    const path: Filter = { property: "owner.title", op: "eq", value: "Payments" };
    const builtIn: Filter = { property: "identifier", op: "in", values: ["a", "b"] };
    const number: Filter = { property: "coverage", op: "gte", value: 80 };
    const midday: Filter = { property: "updatedAt", op: "gte", value: "2026-10-01T10:30:00Z" };
    expect(chipsOf([or, nested, path, builtIn, number, midday]).map((c) => c.kind)).toEqual(
      Array(6).fill("fixed"),
    );
  });

  it("fixes a second condition on a property that has a chip", () => {
    const first: Filter = { property: "tier", op: "in", values: ["critical", "standard"] };
    const second: Filter = { property: "tier", op: "ne", value: "standard" };
    const chips = chipsOf([first, second]);
    expect(chips.map((c) => c.kind)).toEqual(["options", "fixed"]);
    // The same end of a range of days twice is a second condition too.
    const from: Filter = { property: "updatedAt", op: "gte", value: "2026-10-01" };
    expect(chipsOf([from, { ...from, value: "2026-10-05" }]).map((c) => c.kind)).toEqual([
      "date",
      "fixed",
    ]);
  });
});

describe("filter conditions", () => {
  it("says one value as eq or ne, several as in or not in, and none as exists", () => {
    expect(optionsFilter("tier", ['"critical"'], false)).toEqual({
      property: "tier",
      op: "eq",
      value: "critical",
    });
    expect(optionsFilter("tier", ['"critical"'], true)).toEqual({
      property: "tier",
      op: "ne",
      value: "critical",
    });
    expect(optionsFilter("tier", ['"a"', '"b"'], false)).toEqual({
      property: "tier",
      op: "in",
      values: ["a", "b"],
    });
    expect(optionsFilter("tier", ['"a"', '"b"'], true)).toEqual({
      not: { property: "tier", op: "in", values: ["a", "b"] },
    });
    expect(optionsFilter("tier", [], false)).toEqual({ property: "tier", op: "exists" });
    expect(optionsFilter("tier", [], true)).toEqual({ not: { property: "tier", op: "exists" } });
    expect(optionsFilter("archived", ["true"], true)).toEqual({
      property: "archived",
      op: "ne",
      value: true,
    });
  });

  it("says a range of days as instants, or a within", () => {
    expect(dateFilter("updatedAt", { from: "2026-10-01", to: "2026-10-15" })).toEqual([
      { property: "updatedAt", op: "gte", value: midnight("2026-10-01") },
      { property: "updatedAt", op: "lt", value: midnight("2026-10-16") },
    ]);
    expect(dateFilter("updatedAt", { within: "7d" })).toEqual([
      { property: "updatedAt", op: "within", value: "7d" },
    ]);
    expect(dateFilter("updatedAt", {})).toEqual([]);
  });

  it("replaces a chip's conditions where its first stood", () => {
    const [a, b, c, d] = ["a", "b", "c", "d"].map((property) => ({ property, op: "exists" }));
    expect(replaceConditions([a, b, c, d], [1, 3], [{ property: "x", op: "exists" }])).toEqual([
      a,
      { property: "x", op: "exists" },
      c,
    ]);
    expect(replaceConditions([a, b, c], [1], [])).toEqual([a, c]);
  });
});
