import { describe, expect, it } from "vite-plus/test";
import type { AuditEntry } from "./api";
import { describeDetails } from "./describe";

function entry(action: string, details: Record<string, unknown>): AuditEntry {
  return {
    id: 1,
    at: "2026-10-22T09:00:00Z",
    actor: { label: "alice@example.test" },
    action,
    target: { kind: "", id: "", name: "" },
    details,
  };
}

describe("describeDetails", () => {
  it("names the roles of a grant change", () => {
    expect(
      describeDetails(entry("grant.changed", { role: "admin", previous_role: "maintainer" })),
    ).toBe("From Maintainer to Admin");
  });

  it("says how a sign-in matched the user", () => {
    expect(describeDetails(entry("auth.signed_in", { matched_by: "external_id" }))).toBe(
      "Matched by the SCIM external ID",
    );
    expect(describeDetails(entry("auth.sign_in_refused", { matched_by: "none" }))).toBe(
      "No provisioned user has this identity",
    );
  });

  it("describes nothing for an action without details", () => {
    expect(describeDetails(entry("scim_token.rotated", {}))).toBe("");
  });
});
