import { Link } from "@tanstack/react-router";
import { DescriptionList, DescriptionRow } from "@/components/common/description-list";
import { TableFrame } from "@/components/common/table-frame";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useCan } from "@/features/auth";
import type { Blueprint } from "@/features/catalog";
import { documentKey, entityCountLabel } from "../format";
import { BlueprintTile, IntegrationIcon } from "./icons";
import { DetailHeader, DetailSection, EmptyRow } from "./master-detail";

type RelationRow = {
  direction: "Outgoing" | "Incoming";
  key: string;
  title: string;
  blueprint: string;
  blueprintTitle: string;
  many: boolean;
};

function relationRows(blueprint: Blueprint, blueprints: Blueprint[]): RelationRow[] {
  const outgoing = blueprint.relations.map((relation) => ({
    direction: "Outgoing" as const,
    key: relation.key,
    title: relation.title,
    blueprint: relation.target,
    blueprintTitle: relation.target_title,
    many: relation.many,
  }));
  const incoming = blueprint.inverses.map((inverse) => ({
    direction: "Incoming" as const,
    key: inverse.key ?? inverse.relation,
    title: inverse.title,
    blueprint: inverse.source,
    blueprintTitle: inverse.source_title,
    many:
      blueprints
        .find((candidate) => candidate.identifier === inverse.source)
        ?.relations.find((relation) => relation.key === inverse.relation)?.many ?? false,
  }));
  return [...outgoing, ...incoming];
}

export function BlueprintDetail({
  blueprint,
  blueprints,
  onSelect,
  onShowIntegration,
}: Readonly<{
  blueprint: Blueprint;
  blueprints: Blueprint[];
  onSelect: (identifier: string) => void;
  onShowIntegration: (identifier: string) => void;
}>) {
  const can = useCan();
  const relations = relationRows(blueprint, blueprints);

  return (
    <div className="space-y-8 p-5">
      <div className="space-y-1">
        <DetailHeader
          icon={<BlueprintTile icon={blueprint.icon} className="size-7" />}
          title={blueprint.title}
          meta={
            <>
              <Badge variant="outline" className="font-mono">
                {blueprint.identifier}
              </Badge>
              <span className="text-sm text-muted-foreground">
                {entityCountLabel(blueprint.entity_count)}
              </span>
            </>
          }
          actions={
            <>
              <Button asChild variant="outline" size="sm">
                <Link to="/catalog/$blueprint" params={{ blueprint: blueprint.identifier }}>
                  Open catalog
                </Link>
              </Button>
              {can("catalog.model") ? (
                <Button asChild variant="outline" size="sm">
                  <Link
                    to="/settings/catalog-setup"
                    search={{
                      tab: "definitions",
                      document: documentKey("Blueprint", blueprint.identifier),
                    }}
                  >
                    Edit definition
                  </Link>
                </Button>
              ) : null}
            </>
          }
        />
        {blueprint.description ? (
          <p className="text-sm text-muted-foreground">{blueprint.description}</p>
        ) : null}
      </div>

      <DescriptionList>
        <DescriptionRow label="Fed by">
          {blueprint.fed_by.length === 0 ? (
            <span className="text-muted-foreground">People in the portal</span>
          ) : (
            <span className="flex flex-wrap gap-1">
              {blueprint.fed_by.map((integration) => (
                <Badge key={integration.identifier} asChild variant="outline">
                  <button type="button" onClick={() => onShowIntegration(integration.identifier)}>
                    <IntegrationIcon icon={integration.connector} className="size-3" />
                    {integration.title}
                  </button>
                </Badge>
              ))}
            </span>
          )}
        </DescriptionRow>
      </DescriptionList>

      <DetailSection title="Properties">
        <TableFrame>
          <TableHeader>
            <TableRow>
              <TableHead>Identifier</TableHead>
              <TableHead>Title</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Format</TableHead>
              <TableHead>Enum values</TableHead>
              <TableHead>Display</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {blueprint.properties.length === 0 ? (
              <EmptyRow columns={6} message="This blueprint has no properties." />
            ) : null}
            {blueprint.properties.map((property) => (
              <TableRow key={property.key}>
                <TableCell className="font-mono text-xs">{property.key}</TableCell>
                <TableCell>{property.title}</TableCell>
                <TableCell className="text-muted-foreground">{property.type}</TableCell>
                <TableCell className="text-muted-foreground">{property.format}</TableCell>
                <TableCell className="whitespace-normal">
                  <span className="flex flex-wrap gap-1">
                    {property.enum?.map((value) => (
                      <Badge key={value} variant="muted" className="font-mono">
                        {value}
                      </Badge>
                    ))}
                  </span>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {property.display}
                  {property.hidden ? ", hidden" : ""}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </TableFrame>
      </DetailSection>

      <DetailSection title="Relations">
        <TableFrame>
          <TableHeader>
            <TableRow>
              <TableHead>Direction</TableHead>
              <TableHead>Relation</TableHead>
              <TableHead>Blueprint</TableHead>
              <TableHead>Cardinality</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {relations.length === 0 ? (
              <EmptyRow columns={4} message="This blueprint has no relations." />
            ) : null}
            {relations.map((relation) => (
              <TableRow key={`${relation.direction}.${relation.blueprint}.${relation.key}`}>
                <TableCell>
                  <Badge variant={relation.direction === "Outgoing" ? "outline" : "muted"}>
                    {relation.direction}
                  </Badge>
                </TableCell>
                <TableCell>
                  <span className="font-mono text-xs">{relation.key}</span>
                  {relation.title ? (
                    <span className="ml-2 text-muted-foreground">{relation.title}</span>
                  ) : null}
                </TableCell>
                <TableCell>
                  <button
                    type="button"
                    className="underline-offset-4 hover:underline"
                    onClick={() => onSelect(relation.blueprint)}
                  >
                    {relation.blueprintTitle}
                  </button>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {relation.many ? "many" : "one"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </TableFrame>
      </DetailSection>

      {blueprint.derived.length > 0 ? (
        <DetailSection title="Derived">
          <TableFrame>
            <TableHeader>
              <TableRow>
                <TableHead>Identifier</TableHead>
                <TableHead>Title</TableHead>
                <TableHead>Path</TableHead>
                <TableHead>Blueprint</TableHead>
                <TableHead>Cardinality</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {blueprint.derived.map((derived) => (
                <TableRow key={derived.key}>
                  <TableCell className="font-mono text-xs">{derived.key}</TableCell>
                  <TableCell>{derived.title}</TableCell>
                  <TableCell className="font-mono text-xs break-all whitespace-normal text-muted-foreground">
                    {derived.path}
                  </TableCell>
                  <TableCell>
                    <button
                      type="button"
                      className="underline-offset-4 hover:underline"
                      onClick={() => onSelect(derived.target)}
                    >
                      {derived.target_title}
                    </button>
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {derived.many ? "many" : "one"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </TableFrame>
        </DetailSection>
      ) : null}
    </div>
  );
}
