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

  it("summarizes a definitions revision", () => {
    expect(
      describeDetails(
        entry("definitions.applied", {
          added: 1,
          changed: 2,
          removed: 0,
          deleted_entities: 3,
          message: "add teams",
        }),
      ),
    ).toBe('1 added, 2 changed, 0 removed, 3 entities deleted, "add teams"');
    expect(
      describeDetails(
        entry("definitions.rolled_back", {
          added: 0,
          changed: 1,
          removed: 0,
          deleted_entities: 0,
          message: "",
          rollback_of: 4,
        }),
      ),
    ).toBe("Back to revision 4, 0 added, 1 changed, 0 removed");
  });

  it("names the blueprint and the fields of an entity change", () => {
    expect(
      describeDetails(
        entry("entity.updated", { blueprint: "service", changed: ["title", "owner"] }),
      ),
    ).toBe("Blueprint service, changed title, owner");
    expect(describeDetails(entry("entity.deleted", { blueprint: "service" }))).toBe(
      "Blueprint service",
    );
  });

  it("names the run of a sync and the integration of an ingest token", () => {
    expect(describeDetails(entry("integration.synced", { run: 12 }))).toBe("Run 12");
    expect(describeDetails(entry("ingest_token.revoked", { integration: "ci" }))).toBe("For ci");
    expect(
      describeDetails(
        entry("ingest_token.created", { integration: "ci", expires_at: "2026-11-24T09:00:00Z" }),
      ),
    ).toMatch(/^For ci, expires Nov 24, 2026/);
  });
});
