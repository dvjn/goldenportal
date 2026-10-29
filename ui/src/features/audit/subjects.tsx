import { Link, type LinkOptions, linkOptions } from "@tanstack/react-router";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { AuditActor, AuditEntry, AuditParent, AuditResource } from "./api";
import { actorKind, resourceKind } from "./labels";

const goneActions = /\.(deleted|revoked|deprovisioned)$/;

function userLink(id: string) {
  return id ? linkOptions({ to: "/settings/access", search: { user: id } }) : undefined;
}

function integrationLink(id: string) {
  return linkOptions({
    to: "/settings/catalog-setup",
    search: { tab: "integrations", integration: id },
  });
}

const resourceLinks: Record<string, (resource: AuditResource) => LinkOptions | undefined> = {
  user: (resource) => userLink(resource.id),
  group: () => linkOptions({ to: "/settings/access" }),
  service_token: () => linkOptions({ to: "/settings/service-tokens" }),
  scim_token: () => linkOptions({ to: "/settings/system" }),
  definitions_revision: (resource) =>
    linkOptions({
      to: "/settings/catalog-setup/history/$revision",
      params: { revision: Number(resource.id) },
    }),
  blueprint: (resource) =>
    linkOptions({ to: "/catalog/$blueprint", params: { blueprint: resource.id } }),
  entity: (resource) =>
    resource.parent &&
    linkOptions({
      to: "/catalog/$blueprint/$identifier",
      params: { blueprint: resource.parent.id, identifier: resource.id },
    }),
  integration: (resource) => integrationLink(resource.id),
  ingest_token: (resource) => resource.parent && integrationLink(resource.parent.id),
};

const actorLinks: Record<string, (actor: AuditActor) => LinkOptions | undefined> = {
  user: (actor) => userLink(actor.id),
  service_token: () => linkOptions({ to: "/settings/service-tokens" }),
  scim: () => linkOptions({ to: "/settings/system" }),
};

export function actorLink(actor: AuditActor) {
  return actorLinks[actor.kind]?.(actor);
}

/** The page of a resource, unless the entry's action removed it. */
export function resourceLink(entry: AuditEntry) {
  if (goneActions.test(entry.action)) return undefined;
  return resourceLinks[entry.resource.kind]?.(entry.resource);
}

export function parentLink(parent: AuditParent) {
  return resourceLinks[parent.kind]?.(parent);
}

export function actorName(actor: AuditActor) {
  return actor.name || actor.email || actor.id || actorKind(actor.kind).label;
}

export function TwoLineText({
  title,
  subtitle,
  link,
}: {
  title: string;
  subtitle?: ReactNode;
  link?: LinkOptions;
}) {
  return (
    <div className="flex min-w-0 flex-col">
      {link ? (
        <Link {...link} className="max-w-64 truncate leading-5 font-medium hover:underline">
          {title}
        </Link>
      ) : (
        <span className="max-w-64 truncate leading-5 font-medium">{title}</span>
      )}
      {subtitle && (
        <span className="max-w-64 truncate text-xs leading-4 text-muted-foreground">
          {subtitle}
        </span>
      )}
    </div>
  );
}

export function SubjectLabel({
  icon: Icon,
  link,
  title,
  subtitle,
}: {
  icon: LucideIcon;
  link?: LinkOptions;
  title: string;
  subtitle?: ReactNode;
}) {
  return (
    <div className="flex min-w-0 items-start gap-2">
      <span className="flex h-5 shrink-0 items-center">
        <Icon className="size-4 text-muted-foreground" aria-hidden />
      </span>
      <TwoLineText title={title} subtitle={subtitle} link={link} />
    </div>
  );
}

export function ActorLabel({ actor }: { actor: AuditActor }) {
  const kind = actorKind(actor.kind);
  const name = actorName(actor);
  const subtitle = actor.email && actor.email !== name ? actor.email : kind.label;

  return <SubjectLabel icon={kind.icon} link={actorLink(actor)} title={name} subtitle={subtitle} />;
}

export function ResourceLabel({ entry }: { entry: AuditEntry }) {
  const { resource } = entry;
  if (!resource.kind) return <span className="text-muted-foreground">Not set</span>;
  const kind = resourceKind(resource.kind);
  const parent = resource.parent;

  return (
    <SubjectLabel
      icon={kind.icon}
      link={resourceLink(entry)}
      title={resource.name || resource.id}
      subtitle={parent ? `${kind.label} in ${parent.name || parent.id}` : kind.label}
    />
  );
}
