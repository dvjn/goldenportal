import {
  BotIcon,
  BoxIcon,
  CableIcon,
  ClockIcon,
  HistoryIcon,
  KeyRoundIcon,
  LayersIcon,
  type LucideIcon,
  RefreshCwIcon,
  UploadIcon,
  UserIcon,
  UsersIcon,
} from "lucide-react";
import type { ActorKind } from "./api";

const actionLabels: Record<string, string> = {
  "auth.signed_in": "Signed in",
  "auth.signed_out": "Signed out",
  "auth.sign_in_refused": "Sign-in refused",
  "grant.created": "Granted a role",
  "grant.changed": "Changed a role",
  "grant.removed": "Removed a grant",
  "sessions.revoked": "Revoked sessions",
  "scim_token.rotated": "Rotated the SCIM token",
  "service_token.created": "Created a service token",
  "service_token.revoked": "Revoked a service token",
  "user.provisioned": "Provisioned a user",
  "user.updated": "Updated a user",
  "user.deprovisioned": "Deprovisioned a user",
  "group.provisioned": "Provisioned a group",
  "group.updated": "Updated a group",
  "group.deleted": "Deleted a group",
  "definitions.applied": "Applied definitions",
  "definitions.rolled_back": "Rolled back definitions",
  "entity.created": "Created an entity",
  "entity.updated": "Edited an entity",
  "entity.deleted": "Deleted an entity",
  "integration.synced": "Synced an integration",
  "integration.pushed": "Pushed to an integration",
  "ingest_token.created": "Created an ingest token",
  "ingest_token.revoked": "Revoked an ingest token",
};

type Kind = { label: string; icon: LucideIcon };

const resourceKinds: Record<string, Kind> = {
  user: { label: "User", icon: UserIcon },
  group: { label: "Group", icon: UsersIcon },
  service_token: { label: "Service token", icon: KeyRoundIcon },
  scim_token: { label: "SCIM token", icon: KeyRoundIcon },
  definitions_revision: { label: "Definitions revision", icon: HistoryIcon },
  blueprint: { label: "Blueprint", icon: LayersIcon },
  entity: { label: "Entity", icon: BoxIcon },
  integration: { label: "Integration", icon: CableIcon },
  ingest_token: { label: "Ingest token", icon: KeyRoundIcon },
};

const actorKinds: Record<ActorKind, Kind> = {
  user: { label: "User", icon: UserIcon },
  service_token: { label: "Service token", icon: BotIcon },
  ingest_token: { label: "Ingest token", icon: UploadIcon },
  scim: { label: "SCIM provisioning", icon: RefreshCwIcon },
  system: { label: "System", icon: ClockIcon },
};

const unknownKind = (kind: string): Kind => ({ label: kind, icon: BoxIcon });

export function actionLabel(action: string) {
  return actionLabels[action] ?? action;
}

export function resourceKind(kind: string) {
  return resourceKinds[kind] ?? unknownKind(kind);
}

export function resourceKindLabel(kind: string) {
  return resourceKind(kind).label;
}

export function actorKind(kind: string) {
  return actorKinds[kind as ActorKind] ?? unknownKind(kind);
}

export function actorKindLabel(kind: string) {
  return actorKind(kind).label;
}
