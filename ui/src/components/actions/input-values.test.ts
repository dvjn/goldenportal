import { describe, expect, it } from "vite-plus/test";
import type { Property } from "@/features/blueprints";
import { controlFor, fromDraft, toDraft } from "./input-values";

const input = (property: Partial<Property>): Property => ({
  identifier: "x",
  title: "X",
  type: "string",
  ...property,
});

describe("input values", () => {
  it("picks a control by type, format and values", () => {
    expect(controlFor(input({ enum: ["a", "b"] }))).toBe("enum");
    expect(controlFor(input({ type: "relation", relation: "team" }))).toBe("relation");
    expect(controlFor(input({ type: "integer" }))).toBe("number");
    expect(controlFor(input({ format: "date-time" }))).toBe("date-time");
    expect(controlFor(input({ type: "array", items: { type: "string", enum: ["a"] } }))).toBe(
      "picks",
    );
    expect(controlFor(input({ type: "array", items: { type: "number" } }))).toBe("list");
    expect(controlFor(input({ type: "boolean" }))).toBe("boolean");
  });

  it("leaves out what wasn't entered", () => {
    expect(fromDraft(input({}), undefined)).toEqual({});
    expect(fromDraft(input({}), "  ")).toEqual({});
    expect(fromDraft(input({ type: "array" }), [])).toEqual({});
  });

  it("sends values in their input's type", () => {
    expect(fromDraft(input({ type: "integer" }), "3")).toEqual({ value: 3 });
    expect(fromDraft(input({ type: "number" }), "x")).toEqual({ problem: "must be a number" });
    expect(fromDraft(input({ type: "boolean" }), "false")).toEqual({ value: false });
    expect(fromDraft(input({ type: "number", enum: [1, 2] }), "2")).toEqual({ value: 2 });
    expect(fromDraft(input({ type: "array", items: { type: "integer" } }), "1\n\n2")).toEqual({
      value: [1, 2],
    });
    expect(fromDraft(input({ type: "object" }), '{"a": 1}')).toEqual({ value: { a: 1 } });
    expect(fromDraft(input({ type: "object" }), "{")).toHaveProperty("problem");
  });

  it("sends one related entity, or a list when it takes many", () => {
    const one = input({ type: "relation", relation: "team" });
    const many = input({ type: "relation", relation: "team", many: true });
    expect(fromDraft(one, ["payments"])).toEqual({ value: "payments" });
    expect(fromDraft(many, ["payments", "search"])).toEqual({ value: ["payments", "search"] });
    expect(toDraft(many, "payments")).toEqual(["payments"]);
  });

  it("fills controls from values given before", () => {
    expect(toDraft(input({ type: "integer" }), 5)).toBe("5");
    expect(toDraft(input({ type: "array", items: { type: "string" } }), ["a", "b"])).toBe("a\nb");
    expect(toDraft(input({ type: "boolean" }), true)).toBe("true");
    expect(toDraft(input({}), null)).toBeUndefined();
    // A moment goes through the local time the control shows and back.
    const at = "2026-10-25T14:30:00Z";
    const dateTime = input({ format: "date-time" });
    expect(fromDraft(dateTime, toDraft(dateTime, at))).toEqual({ value: at });
  });
});
