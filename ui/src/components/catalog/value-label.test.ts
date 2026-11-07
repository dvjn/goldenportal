import { describe, expect, it } from "vite-plus/test";
import { titleCase, valueLabel } from "./value-label";

describe("value labels", () => {
  it("writes a boolean as Yes or No", () => {
    expect(valueLabel(true)).toBe("Yes");
    expect(valueLabel(false)).toBe("No");
  });

  it("writes an enum value in Title Case unless the blueprint titles it", () => {
    const property = {
      enum: ["failed", "on-prem", "gold_plus", "other"],
      display: { titles: { other: "Something Else" } },
    };
    expect(valueLabel("failed", { property })).toBe("Failed");
    expect(valueLabel("on-prem", { property })).toBe("On Prem");
    expect(valueLabel("gold_plus", { property })).toBe("Gold Plus");
    expect(valueLabel("other", { property })).toBe("Something Else");
    expect(titleCase("on-prem")).toBe("On Prem");
  });

  it("writes a relation by its entity's title, or else its identifier", () => {
    const titles = new Map([["payments", "Payments"]]);
    expect(valueLabel("payments", { titles })).toBe("Payments");
    expect(valueLabel("search", { titles })).toBe("search");
  });

  it("writes a scorecard tier by its title", () => {
    const titles = new Map([
      ["", "No Tier"],
      ["gold", "Gold Standard"],
    ]);
    expect(valueLabel("gold", { titles })).toBe("Gold Standard");
    expect(valueLabel("", { titles })).toBe("No Tier");
  });

  it("leaves other values as they are", () => {
    expect(valueLabel("1.2", { property: {} })).toBe("1.2");
    expect(valueLabel(72)).toBe("72");
  });
});
