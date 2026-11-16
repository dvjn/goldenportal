import { describe, expect, it } from "vitest";
import { formatCount, formatPercent, formatSeconds, percentOf } from "./format";

describe("formatCount", () => {
  it("separates thousands", () => {
    expect(formatCount(51476)).toBe("51,476");
    expect(formatCount(7)).toBe("7");
  });
});

describe("formatPercent", () => {
  it("rounds to the nearest percent", () => {
    expect(formatPercent(2, 3)).toBe("67%");
    expect(formatPercent(24, 84)).toBe("29%");
    expect(formatPercent(1, 8)).toBe("13%");
    expect(formatPercent(3, 3)).toBe("100%");
    expect(formatPercent(0, 5)).toBe("0%");
  });

  it("reads a share that isn't none or all as neither", () => {
    expect(formatPercent(200, 51455)).toBe("<1%");
    expect(formatPercent(307, 51455)).toBe("1%");
    expect(formatPercent(1, 150)).toBe("1%");
    expect(formatPercent(198, 200)).toBe("99%");
    expect(formatPercent(199, 200)).toBe(">99%");
    expect(formatPercent(995, 1000)).toBe(">99%");
    expect(formatPercent(12.4, 100)).toBe("12%");
  });

  it("keeps a negative share's sign", () => {
    expect(formatPercent(-0.4, 100)).toBe("-<1%");
    expect(formatPercent(-12.6, 100)).toBe("-13%");
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
