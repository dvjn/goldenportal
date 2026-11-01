import { Link } from "@tanstack/react-router";
import { IconTile } from "@/components/catalog/blueprint-icon";

// DocTitle opens a blueprint or integration from a table: its icon, then
// its title over its identifier.
export function DocTitle({
  to,
  icon,
  color,
  title,
  identifier,
}: Readonly<{
  to: "/blueprints/$identifier" | "/integrations/$identifier";
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
