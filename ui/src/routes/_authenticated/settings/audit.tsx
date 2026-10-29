import { createFileRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/common/page-header";
import { AuditLogView, type AuditSearch } from "@/features/audit";
import { requirePermission } from "@/features/auth";
import { usePageTitle } from "@/lib/use-page-title";

const datePattern = /^\d{4}-\d{2}-\d{2}$/;

function text(value: unknown) {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function date(value: unknown) {
  return typeof value === "string" && datePattern.test(value) ? value : undefined;
}

export const Route = createFileRoute("/_authenticated/settings/audit")({
  validateSearch: (search: Record<string, unknown>): AuditSearch => {
    const page = Math.floor(Number(search.page));
    return {
      q: text(search.q),
      actor: text(search.actor),
      actor_kind: text(search.actor_kind),
      action: text(search.action),
      resource_kind: text(search.resource_kind),
      from: date(search.from),
      to: date(search.to),
      sort: search.sort === "asc" ? "asc" : undefined,
      page: page > 1 ? page : undefined,
    };
  },
  beforeLoad: ({ context }) => requirePermission(context.queryClient, "audit.read"),
  component: AuditPage,
});

function AuditPage() {
  usePageTitle("Audit log");
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  return (
    <>
      <PageHeader
        title="Audit log"
        description="Who did what to which resource, from where: sign-ins, access, provisioning, definitions, entities, syncs, pushes and tokens."
      />
      <AuditLogView
        search={search}
        onSearchChange={(next) =>
          navigate({ search: (previous) => ({ ...previous, ...next }), replace: true })
        }
      />
    </>
  );
}
