import { describe, expect, it } from "vitest";
import { formatPercent, formatSeconds, percentOf } from "./format";

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

describe("formatSeconds", () => {
  it.each([
    [339.15, "5m39s"],
    [33396.86, "9h17m"],
    [308318, "3d14h"],
    [965.6, "16m6s"],
    [59.5, "59.5s"],
    [30, "30s"],
    [0, "0s"],
    [3600, "1h"],
    [3599.6, "1h"],
    [86399, "1d"],
    [59.96, "1m"],
    [-339.15, "-5m39s"],
  ])("writes %s as %s", (value, text) => {
    expect(formatSeconds(value)).toBe(text);
  });
});
