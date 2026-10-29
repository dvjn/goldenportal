import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { DescriptionList, DescriptionRow } from "@/components/common/description-list";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { Entity, EntityName } from "../api";
import { DescribedLabel } from "./described-label";

export const collapsedLinkCount = 6;

type LinkGroup = { key: string; label: string; description?: string; entities: EntityName[] };

function EntityLink({ entity }: Readonly<{ entity: EntityName }>) {
  return (
    <Link
      to="/catalog/$blueprint/$identifier"
      params={{ blueprint: entity.blueprint, identifier: entity.identifier }}
      className="truncate text-primary underline-offset-4 hover:underline"
    >
      {entity.title || entity.identifier}
    </Link>
  );
}

function EntityLinks({ entities }: Readonly<{ entities: EntityName[] }>) {
  const [expanded, setExpanded] = useState(false);
  const hidden = expanded ? 0 : Math.max(0, entities.length - collapsedLinkCount);
  const shown = entities.slice(0, entities.length - hidden);

  return (
    <ul className="flex flex-col gap-1">
      {shown.map((entity) => (
        <li key={`${entity.blueprint}/${entity.identifier}`} className="flex min-w-0">
          <EntityLink entity={entity} />
        </li>
      ))}
      {hidden > 0 ? (
        <li>
          <Button variant="link" size="sm" className="h-auto p-0" onClick={() => setExpanded(true)}>
            Show {hidden} more
          </Button>
        </li>
      ) : null}
    </ul>
  );
}

function GroupLabel({ group }: Readonly<{ group: LinkGroup }>) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <DescribedLabel label={group.label} description={group.description} />
      {group.entities.length > 1 ? (
        <span className="text-xs tabular-nums">{group.entities.length}</span>
      ) : null}
    </span>
  );
}

function LinkGroups({ groups, empty }: Readonly<{ groups: LinkGroup[]; empty: string }>) {
  if (groups.length === 0) return <p className="text-sm text-muted-foreground">{empty}</p>;
  return (
    <DescriptionList>
      {groups.map((group) => (
        <DescriptionRow key={group.key} label={<GroupLabel group={group} />}>
          <EntityLinks entities={group.entities} />
        </DescriptionRow>
      ))}
    </DescriptionList>
  );
}

function outgoingGroups(entity: Entity): LinkGroup[] {
  return entity.relations
    .filter((group) => group.entities.length > 0)
    .map((group) => ({
      key: group.key,
      label: group.title || group.key,
      entities: group.entities,
    }));
}

function incomingGroups(entity: Entity): LinkGroup[] {
  return entity.incoming
    .filter((group) => group.entities.length > 0)
    .map((group) =>
      group.key
        ? { key: group.key, label: group.title || group.key, entities: group.entities }
        : {
            key: `${group.source}/${group.relation}`,
            label: group.source_title || group.source,
            description: `Linked as ${group.title || group.relation}`,
            entities: group.entities,
          },
    );
}

export function EntityRelationsPanel({ entity }: Readonly<{ entity: Entity }>) {
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>Relations</CardTitle>
        </CardHeader>
        <CardContent>
          <LinkGroups groups={outgoingGroups(entity)} empty="This entity has no relations." />
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Referenced by</CardTitle>
        </CardHeader>
        <CardContent>
          <LinkGroups groups={incomingGroups(entity)} empty="No other entity points here." />
        </CardContent>
      </Card>
    </div>
  );
}
