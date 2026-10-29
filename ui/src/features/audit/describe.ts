import { roleLabels } from "@/features/access";
import type { Role } from "@/features/auth";
import { formatDateTime } from "@/lib/time";
import type { AuditChange, AuditEntry } from "./api";

type Details = AuditEntry["details"];

const matchLabels: Record<string, string> = {
  oidc_subject: "the subject of an earlier sign-in",
  external_id: "the SCIM external ID",
  email: "email",
  bootstrap_admin: "the bootstrap admin list",
};

function role(value: unknown) {
  return roleLabels[value as Role] ?? String(value);
}

function roleChange(details: Details): AuditChange {
  return details.changes?.role ?? { before: "", after: "" };
}

function matchedBy(details: Details) {
  return `Matched by ${matchLabels[String(details.matched_by)] ?? String(details.matched_by)}`;
}

function changedFields(details: Details) {
  const fields = Object.keys(details.changes ?? {});
  return fields.length > 0 ? `Changed ${fields.join(", ")}` : "";
}

function revisionChanges(details: Details) {
  const parts = [`${details.added} added, ${details.changed} changed, ${details.removed} removed`];
  const deleted = Number(details.deleted_entities);
  if (deleted > 0) parts.push(deleted === 1 ? "1 entity deleted" : `${deleted} entities deleted`);
  if (details.message) parts.push(`"${details.message}"`);
  return parts.join(", ");
}

function run(details: Details) {
  return [`Run ${details.run}`, details.trigger].filter(Boolean).join(", ");
}

const describers: Record<string, (details: Details) => string> = {
  "auth.signed_in": matchedBy,
  "auth.sign_in_refused": (details) =>
    details.matched_by === "none"
      ? "No provisioned user has this identity"
      : `${matchedBy(details)}, but the user is not active`,
  "grant.created": (details) => `Granted ${role(roleChange(details).after)}`,
  "grant.changed": (details) =>
    `From ${role(roleChange(details).before)} to ${role(roleChange(details).after)}`,
  "grant.removed": (details) => `Removed ${role(roleChange(details).before)}`,
  "service_token.created": (details) =>
    `${role(details.role)} token, expires ${formatDateTime(String(details.expires_at))}`,
  "user.updated": changedFields,
  "group.updated": changedFields,
  "definitions.applied": revisionChanges,
  "definitions.rolled_back": (details) =>
    `Back to revision ${details.rollback_of}, ${revisionChanges(details)}`,
  "entity.updated": changedFields,
  "integration.synced": run,
  "integration.pushed": (details) =>
    `${run(details)}, ${details.upserted} upserted, ${details.deleted} deleted`,
  "ingest_token.created": (details) => `Expires ${formatDateTime(String(details.expires_at))}`,
};

export function describeDetails(entry: AuditEntry) {
  return describers[entry.action]?.(entry.details) ?? "";
}

/** A value as the changes table shows it. */
export function formatValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "";
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map(formatValue).join(", ");
  return JSON.stringify(value);
}
