import { Link } from "@tanstack/react-router";
import { Badge } from "@/components/ui/badge";
import type { EntityName } from "../api";
import { EmptyValue } from "./property-value";

const visibleLinks = 3;

function refTitle(ref: EntityName): string {
  return ref.title || ref.identifier;
}

export function RelationLinks({
  refs,
  emptyLabel,
}: Readonly<{ refs: EntityName[] | undefined; emptyLabel?: string }>) {
  if (!refs || refs.length === 0) return <EmptyValue label={emptyLabel} />;
  const shown = refs.slice(0, visibleLinks);
  const rest = refs.slice(visibleLinks);

  return (
    <span className="flex flex-wrap gap-1">
      {shown.map((ref) => (
        <Badge key={`${ref.blueprint}/${ref.identifier}`} asChild variant="outline">
          <Link
            to="/catalog/$blueprint/$identifier"
            params={{ blueprint: ref.blueprint, identifier: ref.identifier }}
            className="max-w-48"
          >
            <span className="truncate">{refTitle(ref)}</span>
          </Link>
        </Badge>
      ))}
      {rest.length > 0 ? (
        <Badge variant="muted" title={rest.map(refTitle).join(", ")}>
          +{rest.length}
        </Badge>
      ) : null}
    </span>
  );
}
