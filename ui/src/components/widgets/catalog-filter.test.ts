import { describe, expect, it } from "vite-plus/test";
import { toCatalogLink } from "./catalog-filter";

describe("widget links into the catalog", () => {
  it("links a filter's conditions as they are", () => {
    const owner = { property: "owner", op: "in", values: ["payments", "search"] };
    const failed = { property: "status", op: "ne", value: "failed" };
    const either = { or: [{ property: "tier", op: "eq", value: "critical" }] };
    expect(toCatalogLink({ and: [owner, failed, either] })).toEqual({
      filter: [owner, failed, either],
    });
    expect(toCatalogLink(owner)).toEqual({ filter: [owner] });
    expect(toCatalogLink(undefined)).toEqual({});
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
