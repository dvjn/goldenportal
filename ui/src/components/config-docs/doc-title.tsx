import { Link } from "@tanstack/react-router";
import { IconTile } from "@/components/catalog/blueprint-icon";

// DocPath is the page a document of each listed kind opens on.
export type DocPath =
  | "/blueprints/$identifier"
  | "/integrations/$identifier"
  | "/dashboards/$identifier";

// DocTitle opens a configuration document from a table: its icon, then its
// title over its identifier.
export function DocTitle({
  to,
  icon,
  color,
  title,
  identifier,
}: Readonly<{
  to: DocPath;
  icon?: string;
  color?: string;
  title: string;
  identifier: string;
}>) {
  return (
    <Link to={to} params={{ identifier }} className="group flex items-center gap-3">
      <IconTile icon={icon} color={color} />
      <span className="grid">
        <span className="font-medium group-hover:underline">{title}</span>
        <code className="font-mono text-xs text-muted-foreground">{identifier}</code>
      </span>
    </Link>
  );
}
