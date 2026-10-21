import { roleLabels } from "@/features/access";
import type { Role } from "@/features/auth";
import { formatDateTime } from "@/lib/time";
import type { AuditEntry } from "./api";

type Details = Record<string, unknown>;

const matchLabels: Record<string, string> = {
  oidc_subject: "the subject of an earlier sign-in",
  external_id: "the SCIM external ID",
  email: "email",
  bootstrap_admin: "the bootstrap admin list",
};

function role(value: unknown) {
  return roleLabels[value as Role] ?? String(value);
}

function matchedBy(details: Details) {
  return `Matched by ${matchLabels[String(details.matched_by)] ?? String(details.matched_by)}`;
}

const describers: Record<string, (details: Details) => string> = {
  "auth.signed_in": matchedBy,
  "auth.sign_in_refused": (details) =>
    details.matched_by === "none"
      ? "No provisioned user has this identity"
      : `${matchedBy(details)}, but the user is not active`,
  "grant.created": (details) => `Granted ${role(details.role)}`,
  "grant.changed": (details) => `From ${role(details.previous_role)} to ${role(details.role)}`,
  "grant.removed": (details) => `Removed ${role(details.previous_role)}`,
  "api_token.created": (details) =>
    `${role(details.role)} token, expires ${formatDateTime(String(details.expires_at))}`,
};

export function describeDetails(entry: AuditEntry) {
  return describers[entry.action]?.(entry.details) ?? "";
}
