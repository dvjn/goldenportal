import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ExternalLink } from "lucide-react";
import { CardTitleBar } from "@/components/card-sections";
import { EntityLink } from "@/components/catalog/entity-link";
import { EmptyValue, isWebUrl, PropertyValue } from "@/components/catalog/property-value";
import { Code, CodeBlock } from "@/components/code";
import { Detail, DetailList } from "@/components/detail-list";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { type Run, type RunStep, runDocs } from "@/features/actions";
import { hasRole, useMe } from "@/features/auth";
import type { Property } from "@/features/blueprints";
import { formatAbsolute, formatDuration, formatRelative } from "@/lib/format";
import { ApprovalView } from "./approval-view";
import { FanoutView } from "./fanout-view";
import { JsonBlock } from "./json-block";
import { StepStatus, stepLook } from "./run-status";
import { Requester, Took } from "./runs-table";

// RunDetails lays out a run: its steps in order, with what it gave and was
// given beside them.
export function RunDetails({ run }: Readonly<{ run: Run }>) {
  // The action says how its inputs read; a deleted one leaves only their names.
  const { data: action } = useQuery(runDocs(run.kind).queries.detail(run.action));
  const inputs = (action?.published?.spec ?? action?.draft?.spec)?.inputs ?? [];

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <div className="grid min-w-0 gap-4">
        {Object.keys(run.inputs).length > 0 && <InputsCard run={run} inputs={inputs} />}
        <StepsCard run={run} />
        <ResultCard run={run} />
      </div>
      <AboutCard run={run} />
    </div>
  );
}

function AboutCard({ run }: Readonly<{ run: Run }>) {
  const builder = hasRole(useMe().role, "builder");
  return (
    <Card>
      <CardTitleBar title="About" />
      <CardContent>
        <DetailList>
          <Detail label="Requested By">
            <Requester run={run} />
          </Detail>
          <Detail label="Requested">
            <span title={formatAbsolute(run.createdAt)}>{formatRelative(run.createdAt)}</span>
          </Detail>
          {run.finishedAt && (
            <Detail label="Took">
              <span className="tabular-nums">
                <Took run={run} />
              </span>
            </Detail>
          )}
          <Detail label={run.kind === "workflow" ? "Workflow" : "Action"}>
            {builder ? <DocLink run={run} /> : run.title}
            <span className="text-muted-foreground tabular-nums">v{run.version}</span>
          </Detail>
          {run.parent && (
            <Detail label="Part Of">
              <Link to="/runs/$id" params={{ id: run.parent }} className="hover:underline">
                Run that started it
              </Link>
            </Detail>
          )}
          {run.resolvedBy && <Detail label="Resolved By">{run.resolvedBy}</Detail>}
          {run.resolution && <Detail label="Resolution">{run.resolution}</Detail>}
        </DetailList>
      </CardContent>
    </Card>
  );
}

// DocLink opens what the run ran, for builders, who can open its document.
function DocLink({ run }: Readonly<{ run: Run }>) {
  if (run.kind === "workflow") {
    return (
      <Link
        to="/workflows/$identifier"
        params={{ identifier: run.action }}
        className="hover:underline"
      >
        {run.title}
      </Link>
    );
  }
  return (
    <Link to="/actions/$identifier" params={{ identifier: run.action }} className="hover:underline">
      {run.title}
    </Link>
  );
}

function InputsCard({ run, inputs }: Readonly<{ run: Run; inputs: Property[] }>) {
  return (
    <Card>
      <CardTitleBar title="Inputs" />
      <CardContent>
        <DetailList>
          {Object.entries(run.inputs).map(([identifier, value]) => {
            const input = inputs.find((i) => i.identifier === identifier);
            return (
              <Detail key={identifier} label={input?.title ?? identifier}>
                <InputValue input={input} value={value} />
              </Detail>
            );
          })}
        </DetailList>
      </CardContent>
    </Card>
  );
}

// InputValue shows a value as its input's property would, and related
// entities by identifier, which is what a run keeps of them.
function InputValue({ input, value }: Readonly<{ input?: Property; value: unknown }>) {
  if (input?.relation) {
    const identifiers = Array.isArray(value) ? value.map(String) : [String(value)];
    return identifiers.map((identifier) => (
      <EntityLink key={identifier} blueprint={input.relation ?? ""} identifier={identifier}>
        <Code>{identifier}</Code>
      </EntityLink>
    ));
  }
  if (input) return <PropertyValue property={input} value={value} />;
  return <JsonValue value={value} />;
}

// JsonValue shows a value with no property to say how: text as it is,
// links to open, and anything else as JSON.
function JsonValue({ value }: Readonly<{ value: unknown }>) {
  if (value === null || value === undefined || value === "") return <EmptyValue />;
  if (typeof value === "string") {
    if (isWebUrl(value)) {
      return (
        <a href={value} target="_blank" rel="noreferrer" className="hover:underline">
          {value}
        </a>
      );
    }
    return <span>{value}</span>;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return <span className="tabular-nums">{String(value)}</span>;
  }
  return <CodeBlock className="w-full">{JSON.stringify(value, null, 2)}</CodeBlock>;
}

// ResultCard is what the run gave: its summary, links and outputs.
function ResultCard({ run }: Readonly<{ run: Run }>) {
  const outputs = Object.entries(run.outputs ?? {});
  if (!run.summary && run.links.length === 0 && outputs.length === 0) return null;
  return (
    <Card>
      <CardTitleBar title="Result" />
      <CardContent className="grid gap-4">
        {run.summary && <p className="text-sm">{run.summary}</p>}
        {run.links.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {run.links.map((link) => (
              <Badge key={link.label} variant="outline" asChild>
                <a href={link.url} target="_blank" rel="noreferrer" title={link.url}>
                  <ExternalLink />
                  {link.label}
                </a>
              </Badge>
            ))}
          </div>
        )}
        {outputs.length > 0 && (
          <DetailList>
            {outputs.map(([name, value]) => (
              <Detail key={name} label={<code className="font-mono">{name}</code>}>
                <JsonValue value={value} />
              </Detail>
            ))}
          </DetailList>
        )}
      </CardContent>
    </Card>
  );
}

function StepsCard({ run }: Readonly<{ run: Run }>) {
  return (
    <Card className="gap-0 pb-0">
      <CardTitleBar title="Steps" />
      <CardContent className="px-0">
        <ol aria-label="Steps">
          {(run.steps ?? []).map((step) => (
            <StepItem key={step.position} run={run} step={step} />
          ))}
        </ol>
      </CardContent>
    </Card>
  );
}

// StepItem is one step of the run: how it went, how long it took and how
// many tries, then what went wrong or what it gave. An approval step shows
// what it asked approvers instead, and an action step the runs it started.
function StepItem({ run, step }: Readonly<{ run: Run; step: RunStep }>) {
  const label = stepLook(step).label;
  return (
    <li
      className="grid grid-cols-[1rem_minmax(0,1fr)] gap-x-3 border-b px-4 py-3 last:border-b-0"
      aria-label={`${step.title}: ${label}`}
    >
      <span className="pt-0.5">
        <StepStatus step={step} />
      </span>
      <div className="grid min-w-0 gap-2">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
          <span className="font-medium">{step.title}</span>
          <Badge variant="secondary" className="font-mono">
            {step.type}
          </Badge>
          <span className="ml-auto flex items-center gap-3 text-xs text-muted-foreground tabular-nums">
            {step.attempts > 1 && <span>{step.attempts} attempts</span>}
            {step.startedAt && step.finishedAt && (
              <span title={formatAbsolute(step.finishedAt)}>
                {formatDuration(step.startedAt, step.finishedAt)}
              </span>
            )}
          </span>
        </div>
        {step.error && (
          <p className="font-mono text-xs break-words text-destructive">{step.error}</p>
        )}
        {step.type === "approval" ? (
          <ApprovalView run={run} step={step} />
        ) : step.type === "action" ? (
          <FanoutView run={run} step={step} />
        ) : (
          <>
            {step.detail !== undefined && step.detail !== null && (
              <JsonBlock title="Detail" value={step.detail} />
            )}
            {step.output !== undefined && step.output !== null && (
              <JsonBlock title="Output" value={step.output} />
            )}
          </>
        )}
      </div>
    </li>
  );
}
