import { describe, expect, it } from "vitest";
import { formatPercent, percentOf } from "./format";

describe("formatPercent", () => {
  it("rounds down, so 100% means everything", () => {
    expect(formatPercent(2, 3)).toBe("66%");
    expect(formatPercent(199, 200)).toBe("99%");
    expect(formatPercent(3, 3)).toBe("100%");
    expect(formatPercent(0, 5)).toBe("0%");
  });

  it("is not thrown off by float error", () => {
    expect(percentOf(29, 100)).toBe(29);
    expect(percentOf(57, 100)).toBe(57);
  });
});
