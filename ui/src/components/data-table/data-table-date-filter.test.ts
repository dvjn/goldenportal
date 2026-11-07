import { describe, expect, it } from "vite-plus/test";
import cases from "../../../../internal/filter/testdata/durations.json";
import { isDuration, withinStart } from "./data-table-date-filter";

// The Go tests read the same file, so the two parsers agree.
describe("durations", () => {
  const now = new Date("2030-01-01T00:00:00Z");
  for (const { input, seconds, error } of cases as {
    input: string;
    seconds?: number;
    error?: boolean;
  }[]) {
    it(`reads ${JSON.stringify(input)}`, () => {
      expect(isDuration(input)).toBe(!error);
      if (!error) {
        expect((now.getTime() - withinStart(input, now).getTime()) / 1000).toBe(seconds);
      }
    });
  }
});
