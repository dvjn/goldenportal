import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { type DashboardSpec, dashboards } from "@/features/dashboards";

// Widgets lay out on 12 columns, in order, and share the queries named
// above them. $ marks a variable, such as an input's $input.team, and
// fields are plain paths, such as owner.title; in takes values, a list or
// a variable. A stat counts its query unless it names a field to sum, avg,
// min or max, or a share; columns are paths, {title, path} or {title, jq},
// where jq reads a flat entity: .tier, not .properties.tier.
const template = `kind: Dashboard
metadata:
  name: example
  title: Example
  icon: layout-dashboard
spec:
  order: 1
  inputs:
    team:
      title: Team
      type: relation
      relation: team
      multiple: true
      default:
        query: {blueprint: team, filter: {property: members, op: contains, value: $me}}
        otherwise: everything
  queries:
    services:
      blueprint: service
      filter: {property: owner, op: in, values: $input.team}
  widgets:
    services:
      type: stat
      title: Services
      value: {query: services}
      link: {query: services}
    critical:
      type: stat
      title: Critical
      value: {query: services, share: {property: tier, op: eq, value: critical}}
    byOwner:
      type: bar
      title: Services by Owner
      query: services
      groupBy: owner
      link: {query: services, group: true}
    recent:
      type: table
      title: Recently Updated
      query: {blueprint: service, filter: {property: updatedAt, op: within, value: 7d}}
      columns: [tier, owner.title, {title: Tags, jq: '.tags | length'}]
      sort: -updatedAt
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
      parent={<Link to="/dashboards">Dashboards</Link>}
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
