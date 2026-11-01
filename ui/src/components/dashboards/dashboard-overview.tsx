import { CardTitleBar } from "@/components/card-sections";
import { EmptyValue } from "@/components/catalog/property-value";
import { tableCellClass } from "@/components/data-table";
import { Detail, DetailList } from "@/components/detail-list";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Dashboard, DashboardInput, DashboardSpec } from "@/features/dashboards";
import { formatAbsolute, formatRelative } from "@/lib/format";
import { AudienceValue, Everyone } from "./audience-value";

// DashboardOverview shows a dashboard read-only: where it sits on Home and
// who sees it there, the inputs it asks for and the widgets it shows. The
// Preview tab shows it as Home will.
export function DashboardOverview({
  spec,
  dashboard,
}: Readonly<{ spec: DashboardSpec; dashboard: Dashboard }>) {
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <div className="grid gap-4">
        <WidgetsCard spec={spec} />
        {spec.inputs.length > 0 && <InputsCard inputs={spec.inputs} />}
      </div>
      <AboutCard spec={spec} dashboard={dashboard} />
    </div>
  );
}

function AboutCard({ spec, dashboard }: Readonly<{ spec: DashboardSpec; dashboard: Dashboard }>) {
  const published = dashboard.published;
  return (
    <Card>
      <CardTitleBar title="About" />
      <CardContent className="grid gap-4 text-sm">
        {spec.description && <p className="text-muted-foreground">{spec.description}</p>}
        <DetailList>
          <Detail label="Order">
            <span className="font-medium tabular-nums">{spec.order}</span>
          </Detail>
          <Detail label="Audience">
            <AudienceValue audience={spec.visibility} none={<Everyone />} />
          </Detail>
          <Detail label="Default For">
            <AudienceValue audience={spec.defaultFor} none={<EmptyValue />} />
          </Detail>
          {published && (
            <>
              <Detail label="Version">
                <span className="font-medium tabular-nums">{published.version}</span>
              </Detail>
              <Detail label="Published">
                <span title={formatAbsolute(published.publishedAt)}>
                  {formatRelative(published.publishedAt)}
                </span>
              </Detail>
              {published.publishedBy && <Detail label="By">{published.publishedBy}</Detail>}
            </>
          )}
        </DetailList>
      </CardContent>
    </Card>
  );
}

function WidgetsCard({ spec }: Readonly<{ spec: DashboardSpec }>) {
  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Widgets" />
      <CardContent className="px-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className={tableCellClass}>Widget</TableHead>
              <TableHead className={tableCellClass}>Type</TableHead>
              <TableHead className={tableCellClass}>Width</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {spec.widgets.map((widget) => (
              <TableRow key={widget.identifier}>
                <TableCell className={tableCellClass}>
                  <div className="grid gap-0.5">
                    <span className="font-medium">{widget.title ?? widget.identifier}</span>
                    <code className="font-mono text-xs text-muted-foreground">
                      {widget.identifier}
                    </code>
                  </div>
                </TableCell>
                <TableCell className={tableCellClass}>
                  <Badge variant="secondary" className="font-mono">
                    {widget.type}
                  </Badge>
                </TableCell>
                <TableCell className={`${tableCellClass} tabular-nums`}>
                  {widget.width}/12
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function InputsCard({ inputs }: Readonly<{ inputs: DashboardInput[] }>) {
  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Inputs" />
      <CardContent className="px-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className={tableCellClass}>Input</TableHead>
              <TableHead className={tableCellClass}>Type</TableHead>
              <TableHead className={tableCellClass}>Default</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {inputs.map((input) => (
              <TableRow key={input.identifier}>
                <TableCell className={tableCellClass}>
                  <div className="grid gap-0.5">
                    <span className="font-medium">{input.title}</span>
                    <code className="font-mono text-xs text-muted-foreground">
                      {input.identifier}
                    </code>
                  </div>
                </TableCell>
                <TableCell className={tableCellClass}>
                  <Badge variant="secondary" className="font-mono">
                    {input.relation ?? input.type}
                    {input.multiple && "[]"}
                  </Badge>
                </TableCell>
                <TableCell className={tableCellClass}>
                  <InputDefault input={input} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

// InputDefault says what an input starts at: its values, or a query that
// finds them for whoever is looking.
function InputDefault({ input }: Readonly<{ input: DashboardInput }>) {
  if (input.defaultQuery) {
    return (
      <Badge
        variant="outline"
        title={`Finds ${input.defaultQuery.query.blueprint} entities for whoever is looking`}
      >
        Query
      </Badge>
    );
  }
  if (!input.default?.length) return <EmptyValue />;
  return <span>{input.default.join(", ")}</span>;
}
