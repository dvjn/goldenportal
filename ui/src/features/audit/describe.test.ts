import { describe, expect, it } from "vite-plus/test";
import type { AuditEntry } from "./api";
import { describeDetails, formatValue } from "./describe";

function entry(action: string, details: AuditEntry["details"]): AuditEntry {
  return {
    id: 1,
    at: "2026-10-22T09:00:00Z",
    actor: { kind: "user", id: "u-1", name: "Alice", email: "alice@example.test" },
    action,
    resource: { kind: "", id: "", name: "" },
    details,
    client: { ip: "", user_agent: "" },
  };
}

describe("describeDetails", () => {
  it("names the roles of a grant change", () => {
    const changes = { role: { before: "maintainer", after: "admin" } };
    expect(describeDetails(entry("grant.changed", { changes }))).toBe("From Maintainer to Admin");
    expect(
      describeDetails(
        entry("grant.removed", { changes: { role: { before: "admin", after: "" } } }),
      ),
    ).toBe("Removed Admin");
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

  it("names the fields of an entity change", () => {
    const changes = {
      title: { before: "Payments", after: "Payments API" },
      owner: { before: ["payments"], after: ["platform"] },
    };
    expect(describeDetails(entry("entity.updated", { changes }))).toBe("Changed title, owner");
  });

  it("names the run of a sync and the counts of a push", () => {
    expect(describeDetails(entry("integration.synced", { run: 12, trigger: "schedule" }))).toBe(
      "Run 12, schedule",
    );
    expect(
      describeDetails(
        entry("integration.pushed", { run: 13, trigger: "push", upserted: 4, deleted: 1 }),
      ),
    ).toBe("Run 13, push, 4 upserted, 1 deleted");
    expect(
      describeDetails(entry("ingest_token.created", { expires_at: "2026-11-24T09:00:00Z" })),
    ).toMatch(/^Expires Nov 24, 2026/);
  });
});

describe("formatValue", () => {
  it("shows strings as they are, lists joined and anything else as JSON", () => {
    expect(formatValue("Payments")).toBe("Payments");
    expect(formatValue(["a", "b"])).toBe("a, b");
    expect(formatValue({ tier: 1 })).toBe('{"tier":1}');
    expect(formatValue(null)).toBe("");
  });
});
