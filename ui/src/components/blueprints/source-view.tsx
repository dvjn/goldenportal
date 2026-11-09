import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { CardSection } from "@/components/card-sections";
import { Code } from "@/components/code";
import { Detail, DetailList } from "@/components/detail-list";
import type { Property, Source } from "@/features/blueprints";

// SourceView shows how one source makes entities: its filter and the
// expression behind each field, titled by the blueprint's properties.
export function SourceView({
  source,
  properties,
  title,
  icon,
  action,
  children,
}: Readonly<{
  source: Source;
  properties: Property[];
  title?: string;
  icon?: ReactNode;
  action?: ReactNode;
  children?: ReactNode;
}>) {
  const rows: [string, string | undefined][] = [
    ["Filter", source.filter],
    ["Identifier", source.entity.identifier],
    ["Title", source.entity.title],
    ...source.entity.properties.map((field): [string, string] => [
      properties.find((p) => p.identifier === field.identifier)?.title ?? field.identifier,
      field.expression,
    ]),
  ];

  return (
    <CardSection
      action={action}
      heading={
        <>
          {icon}
          <Link
            to="/connections/$identifier"
            params={{ identifier: source.connection }}
            className="min-w-0 truncate hover:underline"
          >
            {title ?? source.connection}
          </Link>
          <span className="text-muted-foreground" aria-hidden>
            ·
          </span>
          <code className="font-mono text-muted-foreground">{source.resource}</code>
        </>
      }
    >
      {children}
      <DetailList>
        {rows
          .filter(([, value]) => value)
          .map(([label, value]) => (
            <Detail key={label} label={label}>
              <Code>{value}</Code>
            </Detail>
          ))}
      </DetailList>
    </CardSection>
  );
}
