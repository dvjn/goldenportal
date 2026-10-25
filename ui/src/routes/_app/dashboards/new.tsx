import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { type DashboardSpec, dashboards } from "@/features/dashboards";

// Widgets lay out on 12 columns, in order. Filters can compare with $me.
const template = `kind: Dashboard
metadata:
  name: example
  title: Example
  icon: layout-dashboard
spec:
  order: 1
  widgets:
    services:
      type: stat
      title: Services
      blueprint: service
    byOwner:
      type: bar
      title: Services by Owner
      blueprint: service
      groupBy: owner
`;

export const Route = createFileRoute("/_app/dashboards/new")({
  component: NewDashboardPage,
});

function NewDashboardPage() {
  const navigate = useNavigate();

  return (
    <NewDocPage
      docs={dashboards}
      title="New Dashboard"
      template={template}
      onCreated={(identifier) =>
        navigate({
          to: "/dashboards/$identifier",
          params: { identifier },
          search: { tab: "definition" },
        })
      }
      preview={(spec) => <Preview spec={spec} />}
    />
  );
}

// Preview sketches the layout; widgets load their data once the draft is saved.
function Preview({ spec }: Readonly<{ spec: DashboardSpec }>) {
  return (
    <div className="grid grid-cols-12 gap-2">
      {spec.widgets.map((widget) => (
        <div
          key={widget.identifier}
          className="grid gap-0.5 rounded-md border border-dashed px-2.5 py-2 text-xs"
          style={{ gridColumn: `span ${widget.width} / span ${widget.width}` }}
        >
          <span className="truncate font-medium">{widget.title || widget.identifier}</span>
          <span className="text-muted-foreground">{widget.type}</span>
        </div>
      ))}
    </div>
  );
}
