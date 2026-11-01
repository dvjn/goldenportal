import type { RowData } from "@tanstack/react-table";
import { DataTableColumnHeader, dataTableColumns } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import type { DocSummary } from "@/features/config-docs";
import { formatAbsolute, formatRelative } from "@/lib/format";
import { type DocPath, DocTitle } from "./doc-title";

// docColumns are the columns every list of configuration documents shares:
// the document, titled by kind, whether it's published or has a draft, and
// when it last changed.
export function docColumns<T extends DocSummary & RowData>(kind: string, to: DocPath) {
  const column = dataTableColumns<T>();
  return {
    // The identifier is part of the value so search finds documents by either.
    title: column.accessor((doc) => `${doc.title} ${doc.identifier}`, {
      id: "title",
      header: ({ column }) => <DataTableColumnHeader column={column} title={kind} />,
      cell: ({ row }) => (
        <DocTitle
          to={to}
          icon={row.original.icon}
          color={row.original.color}
          title={row.original.title}
          identifier={row.original.identifier}
        />
      ),
    }),
    status: column.accessor((doc) => doc.publishedVersion, {
      id: "status",
      header: ({ column }) => <DataTableColumnHeader column={column} title="Status" />,
      enableGlobalFilter: false,
      cell: ({ row }) => <DocStatus doc={row.original} />,
    }),
    updated: column.accessor((doc) => new Date(doc.updatedAt), {
      id: "updated",
      header: ({ column }) => <DataTableColumnHeader column={column} title="Updated" />,
      sortFn: "datetime",
      enableGlobalFilter: false,
      cell: ({ getValue }) => (
        <span className="text-muted-foreground" title={formatAbsolute(getValue())}>
          {formatRelative(getValue())}
        </span>
      ),
    }),
  };
}

// DocStatus says whether a document is published and whether a draft of
// it is waiting; one never published is only a draft.
export function DocStatus({ doc }: Readonly<{ doc: DocSummary }>) {
  return (
    <span className="flex gap-1.5">
      {doc.publishedVersion > 0 && <Badge variant="success">Published</Badge>}
      {doc.hasDraft && <Badge variant="muted">Draft</Badge>}
    </span>
  );
}
