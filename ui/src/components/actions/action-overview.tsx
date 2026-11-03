import { Link } from "@tanstack/react-router";
import { cn } from "cn";
import type { ReactNode } from "react";
import { stringify } from "yaml";
import { Rules } from "@/components/blueprints/blueprint-overview";
import { PropertyType } from "@/components/blueprints/property-type";
import { CardSection, CardTitleBar } from "@/components/card-sections";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { EmptyValue } from "@/components/catalog/property-value";
import { Code, CodeBlock } from "@/components/code";
import { AudienceValue } from "@/components/dashboards/audience-value";
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
import type { Action, ActionSpec, Step } from "@/features/actions";
import type { Property } from "@/features/blueprints";
import { formatAbsolute, formatRelative } from "@/lib/format";

// ActionOverview shows an action read-only: the steps it takes in order,
// the inputs it asks for, what it runs on, where it shows in the
// self-service catalog and who can run it. A workflow runs on none.
export function ActionOverview({ spec, action }: Readonly<{ spec: ActionSpec; action: Action }>) {
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <div className="grid min-w-0 gap-4">
        {spec.inputs.length > 0 && <InputsCard inputs={spec.inputs} />}
        <StepsCard steps={spec.steps} />
      </div>
      <AboutCard spec={spec} action={action} />
    </div>
  );
}

// Yaml shows a value as the definition writes it.
function Yaml({ value }: Readonly<{ value: unknown }>) {
  return <CodeBlock>{stringify(value).trimEnd()}</CodeBlock>;
}

function AboutCard({ spec, action }: Readonly<{ spec: ActionSpec; action: Action }>) {
  const published = action.published;
  return (
    <Card>
      <CardTitleBar title="About" />
      <CardContent className="grid gap-4 text-sm">
        {spec.description && <p className="text-muted-foreground">{spec.description}</p>}
        <DetailList>
          {spec.kind !== "Workflow" && (
            <Detail label="Runs On">
              <Link
                to="/blueprints/$identifier"
                params={{ identifier: spec.blueprint }}
                className="min-w-0 hover:underline"
              >
                <BlueprintLabel blueprint={spec.blueprint} />
              </Link>
            </Detail>
          )}
          <Detail label="Group">{spec.group || <EmptyValue />}</Detail>
          {spec.scope && (
            <Detail label="Scope">
              <Yaml value={spec.scope} />
            </Detail>
          )}
          <Detail label="Who Can Run" help="Admins always can.">
            <AudienceValue audience={spec.run} none={null} />
            {spec.run.filter && <Yaml value={spec.run.filter} />}
          </Detail>
          {spec.outputs.length > 0 && (
            <Detail label="Outputs">
              {spec.outputs.map((output) => (
                <Code key={output.name}>{output.name}</Code>
              ))}
            </Detail>
          )}
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

function InputsCard({ inputs }: Readonly<{ inputs: Property[] }>) {
  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Inputs" />
      <CardContent className="px-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className={tableCellClass}>Input</TableHead>
              <TableHead className={tableCellClass}>Type</TableHead>
              <TableHead className={tableCellClass}>Rules</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {inputs.map((input) => (
              <TableRow key={input.identifier} className="align-top">
                <TableCell className={cn(tableCellClass, "whitespace-normal")}>
                  <div className="grid gap-0.5">
                    <span className="font-medium">{input.title}</span>
                    <code className="font-mono text-xs text-muted-foreground">
                      {input.identifier}
                    </code>
                    {input.description && (
                      <span className="text-xs text-muted-foreground">{input.description}</span>
                    )}
                  </div>
                </TableCell>
                <TableCell className={tableCellClass}>
                  <PropertyType property={input} />
                </TableCell>
                <TableCell className={cn(tableCellClass, "whitespace-normal")}>
                  <Rules property={input} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function StepsCard({ steps }: Readonly<{ steps: Step[] }>) {
  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Steps" />
      <CardContent className="grid px-0">
        {steps.map((step, index) => (
          <CardSection
            // Steps needn't have ids, and their order is what they are.
            // biome-ignore lint/suspicious/noArrayIndexKey: steps have no identity of their own.
            key={index}
            heading={
              <>
                <span className="text-muted-foreground tabular-nums">{index + 1}</span>
                <span className="truncate">{step.title}</span>
                <Badge variant="secondary" className="font-mono">
                  {step.type}
                </Badge>
              </>
            }
            action={
              step.id && <code className="font-mono text-xs text-muted-foreground">{step.id}</code>
            }
          >
            <DetailList>
              {stepDetails(step).map(([label, value]) => (
                <Detail key={label} label={label}>
                  {value}
                </Detail>
              ))}
            </DetailList>
          </CardSection>
        ))}
      </CardContent>
    </Card>
  );
}

// stepDetails are the labelled parts of a step, as its type has them.
function stepDetails(step: Step): [string, ReactNode][] {
  const rows: [string, ReactNode][] = [];
  const add = (label: string, value: ReactNode) => {
    if (value !== undefined && value !== null && value !== "") rows.push([label, value]);
  };
  const code = (text?: string) => text && <Code>{text}</Code>;
  const { http, entity, check, output, approval, action } = step;
  if (http) {
    add(
      "Request",
      <>
        <Badge variant="outline" className="font-mono">
          {http.method}
        </Badge>
        <Code>{http.url}</Code>
      </>,
    );
    if (http.body !== undefined) add("Body", <Yaml value={http.body} />);
    if (http.success?.length) add("Succeeds On", code(http.success.join(", ")));
    for (const out of http.output ?? []) add(`Output ${out.name}`, code(String(out.value)));
  }
  if (entity) {
    add(
      "Operation",
      <Badge variant={entity.operation === "delete" ? "destructive" : "outline"}>
        {entity.operation}
      </Badge>,
    );
    add("Blueprint", <BlueprintLabel blueprint={entity.blueprint} />);
    add("Identifier", code(entity.identifier));
    add("Title", code(entity.title));
    if (entity.properties) add("Properties", <Yaml value={entity.properties} />);
    add("As", code(entity.integration));
  }
  if (check) {
    add("Blueprint", <BlueprintLabel blueprint={check.blueprint} />);
    if (check.filter) add("Filter", <Yaml value={check.filter} />);
    add("Expect", code(expectText(check)));
    add("Message", code(check.message));
  }
  if (output) {
    add("Summary", code(output.summary));
    for (const link of output.links ?? []) add(link.label, code(link.url));
  }
  if (approval) {
    add(
      "Approvers",
      <>
        <AudienceValue audience={approval.approvers} none={null} />
        {approval.approvers.filter && <Yaml value={approval.approvers.filter} />}
      </>,
    );
    add("Require", <span className="tabular-nums">{approval.require}</span>);
    add("Timeout", code(approval.timeout));
    add("Message", code(approval.message));
    for (const [index, review] of (approval.review ?? []).entries()) {
      add(
        `Review ${review.title ?? index + 1}`,
        <>
          <Badge variant="outline" className="font-mono">
            {review.json !== undefined ? "json" : "markdown"}
          </Badge>
          {code(review.json === undefined ? review.markdown : jsonText(review.json))}
        </>,
      );
    }
  }
  if (action) {
    add(
      "Action",
      <Link
        to="/actions/$identifier"
        params={{ identifier: action.name }}
        className="font-mono text-xs hover:underline"
      >
        {action.name}
      </Link>,
    );
    add("Entities", code(jsonText(action.entities)));
    if (action.inputs) add("Inputs", <Yaml value={action.inputs} />);
    add("Max Failures", <span className="tabular-nums">{action.maxFailures}</span>);
  }
  if (step.retry) add("Retries", <span className="tabular-nums">{step.retry.attempts}</span>);
  if (step.continue) add("On Failure", "Carries on");
  return rows;
}

// jsonText shows a reference as written, and any other value as JSON.
function jsonText(value: unknown) {
  return typeof value === "string" ? value : JSON.stringify(value);
}

function expectText(check: NonNullable<Step["check"]>) {
  if (check.count !== undefined) return `count = ${check.count}`;
  const bounds = [
    check.min !== undefined && `count ≥ ${check.min}`,
    check.max !== undefined && `count ≤ ${check.max}`,
  ].filter(Boolean);
  return bounds.join(", ");
}
