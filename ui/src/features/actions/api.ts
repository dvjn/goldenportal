import type { Role } from "@/features/auth";
import type { Property } from "@/features/blueprints";
import type { Filter } from "@/features/catalog";
import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import { type ApiDataResponse, api } from "@/lib/api";

// Access is who can run an action: anyone with one of the roles, in one of
// the groups, or on an entity matching the filter. Admins always can.
export type Access = { roles?: Role[]; groups?: string[]; filter?: Filter };

// Output is a named value: jq in an HTTP step, a reference in outputs.
export type Output = { name: string; value: unknown };

export type HttpStep = {
  method: string;
  url: string;
  body?: unknown;
  // success are the statuses that count as working; any 2xx when empty.
  success?: number[];
  output?: Output[];
};

export type EntityStep = {
  operation: "upsert" | "delete";
  integration: string;
  blueprint: string;
  identifier: string;
  title?: string;
  properties?: Record<string, unknown>;
};

export type CheckStep = {
  blueprint: string;
  filter?: Filter;
  count?: number;
  min?: number;
  max?: number;
  message: string;
};

export type OutputStep = { summary?: string; links?: RunLink[] };

// Review is one thing approvers see: text in markdown, or a value as JSON.
export type Review = { title?: string; markdown?: string; json?: unknown };

export type ApprovalStep = {
  approvers: Access;
  // require is how many approvals it needs; one rejection ends the run.
  require: number;
  // timeout is how long it waits, written as the definition does, such as 24h.
  timeout: string;
  message?: string;
  review?: Review[];
};

// ActionStep runs an action on each of the entities given, as whoever asked
// for the run, and waits for them all.
export type ActionStep = {
  name: string;
  // entities is a reference to an identifier or a list of them.
  entities: unknown;
  inputs?: Record<string, unknown>;
  // maxFailures is how many runs can fail before the step does.
  maxFailures: number;
};

export type StepType = "http" | "entity" | "check" | "output" | "approval" | "action";

export type Step = {
  id?: string;
  title: string;
  type: StepType;
  // timeout bounds one attempt, such as 30s.
  timeout: string;
  retry?: { attempts: number; backoff: string };
  // continue keeps the run going when the step fails.
  continue?: boolean;
  http?: HttpStep;
  entity?: EntityStep;
  check?: CheckStep;
  output?: OutputStep;
  approval?: ApprovalStep;
  action?: ActionStep;
};

// ActionSpec is an action, which runs on one entity of its blueprint, or a
// workflow, which isn't tied to one. Both show in their group of the
// self-service catalog; a workflow needs one.
export type ActionSpec = {
  kind?: "Action" | "Workflow";
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  color?: string;
  blueprint: string;
  group?: string;
  // scope narrows the entities it applies to.
  scope?: Filter;
  run: Access;
  // destructive actions can't be run by agents.
  destructive: boolean;
  inputs: Property[];
  steps: Step[];
  outputs: Output[];
};

export type Action = ConfigDoc<ActionSpec>;
export type ActionSummary = DocSummary & {
  facts?: { blueprint: string; group?: string; destructive: boolean };
};

export const actions = configDocs<ActionSpec>("actions");

export type Workflow = ConfigDoc<ActionSpec>;
export type WorkflowSummary = DocSummary & {
  facts?: { group?: string; destructive: boolean };
};

export const workflows = configDocs<ActionSpec>("workflows");

// Offer is something in the self-service catalog: a workflow the caller
// can run, or an action they may be able to run on an entity of blueprint,
// which the server decides once one is picked.
export type Offer = {
  kind: "workflow" | "action";
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  color?: string;
  blueprint?: string;
  blueprintTitle?: string;
  destructive: boolean;
  inputs: Property[];
};

// SelfServiceCatalog is what people can run, by group, in the order the
// server sets.
export type SelfServiceCatalog = { groups: { title: string; items: Offer[] }[] };

// runDocs are the documents of what a run of kind ran.
export function runDocs(kind: RunKind) {
  return kind === "workflow" ? workflows : actions;
}

// Available is an action the signed-in user can run on an entity, with what
// it asks for.
export type Available = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  color?: string;
  destructive: boolean;
  inputs: Property[];
};

// A waiting run is paused at an approval step; a rejected one ended there.
export type RunStatus =
  | "queued"
  | "running"
  | "waiting"
  | "succeeded"
  | "failed"
  | "rejected"
  | "cancelled"
  | "interrupted";

export type StepStatus = "pending" | "running" | "waiting" | "done" | "failed" | "interrupted";

export type Decision = {
  email: string;
  name: string;
  decision: "approve" | "reject";
  comment?: string;
  decidedAt: string;
};

// ApprovalDetail is what an approval step shows approvers, with its
// references filled in.
export type ApprovalDetail = { require: number; message?: string; review?: Review[] };

export type RunLink = { label: string; url: string };

export type RunStep = {
  position: number;
  // step is the step's id, when it has one.
  step?: string;
  type: StepType;
  title: string;
  status: StepStatus;
  attempts: number;
  output?: unknown;
  error?: string;
  detail?: unknown;
  startedAt: string | null;
  finishedAt: string | null;
  // dueAt is when a waiting step gives up.
  dueAt?: string;
  decisions?: Decision[];
};

// RunKind is what ran: an action on an entity, or a workflow on none.
export type RunKind = "action" | "workflow";

export type Run = {
  id: string;
  kind: RunKind;
  // action is the identifier of the action or workflow that ran.
  action: string;
  title: string;
  version: number;
  // entity is as it was when the run was asked for; workflow runs have
  // none, so its fields are empty.
  entity: { blueprint: string; identifier: string; title: string };
  requester: { id: string; email: string; name: string; role: Role; groups: string[] };
  // via is "mcp" for runs an agent asked for.
  via: "web" | "mcp";
  inputs: Record<string, unknown>;
  status: RunStatus;
  summary: string;
  links: RunLink[];
  outputs: Record<string, unknown>;
  error?: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  resolvedBy?: string;
  resolution?: string;
  // steps are left out of lists.
  steps?: RunStep[];
  // canDecide is set when the signed-in user can approve or reject the
  // step the run waits at, now.
  canDecide?: boolean;
  // parent is the workflow run whose action step started this one.
  parent?: string;
};

// FanoutRun is one run an action step started, or one it couldn't, which
// has no id and says why in error.
export type FanoutRun = {
  id?: string;
  entity: string;
  title?: string;
  status: RunStatus | "";
  outputs?: Record<string, unknown>;
  error?: string;
};

// FanoutOutput is what an action step gives once its runs are over.
export type FanoutOutput = { runs: FanoutRun[]; succeeded: number; failed: number };

export type RunPage = { runs: Run[]; total: number };

// RunFilters picks a page of runs, newest first. Members only ever get their
// own; builders get everyone's unless mine is set. entity needs blueprint.
export type RunFilters = {
  q?: string;
  action?: string;
  // workflow lists one workflow's runs instead of an action's.
  workflow?: string;
  // parent lists the runs a run's action steps started.
  parent?: string;
  blueprint?: string;
  entity?: string;
  statuses?: RunStatus[];
  mine?: boolean;
  page?: number;
  size?: number;
};

export type RunRequest = {
  // entity is what an action runs on; a workflow runs on none.
  entity?: string;
  inputs: Record<string, unknown>;
  // idempotencyKey makes a request sent again give the run the first made.
  idempotencyKey?: string;
};

// Resolution is how an interrupted step went: done carries the run on after it.
export type Resolution = { outcome: "done" | "failed"; comment: string };

export type DecisionRequest = { decision: Decision["decision"]; comment: string };

export const runKeys = {
  all: ["runs"] as const,
  approvals: ["runs", "approvals"] as const,
  list: (filters: RunFilters) => [...runKeys.all, "list", filters] as const,
  detail: (id: string) => [...runKeys.all, id] as const,
  available: (blueprint: string, entity: string) =>
    ["catalog", "actions", blueprint, entity] as const,
  catalog: ["self-service"] as const,
  rows: (blueprint: string, entities: string[]) =>
    ["catalog", "row-actions", blueprint, entities] as const,
};

const runPath = (id: string) => `runs/${encodeURIComponent(id)}`;

export async function listAvailable(blueprint: string, entity: string): Promise<Available[]> {
  return (
    await api
      .get(`catalog/${encodeURIComponent(blueprint)}/actions`, { searchParams: { entity } })
      .json<ApiDataResponse<Available[]>>()
  ).data;
}

// RowActions are the actions a page of a blueprint's entities offers the
// signed-in user: each once, and by entity, which of them it offers.
export type RowActions = { actions: Available[]; entities: Record<string, string[]> };

// maxRows is how many entities one request can ask about.
export const maxRows = 100;

export async function listRowActions(blueprint: string, entities: string[]): Promise<RowActions> {
  // Identifiers may hold commas, so each is a parameter of its own.
  const searchParams = new URLSearchParams(
    entities.slice(0, maxRows).map((entity) => ["entity", entity]),
  );
  return (
    await api
      .get(`catalog/${encodeURIComponent(blueprint)}/row-actions`, { searchParams })
      .json<ApiDataResponse<RowActions>>()
  ).data;
}

export async function startRun(action: string, request: RunRequest): Promise<Run> {
  return (
    await api
      .post(`actions/${encodeURIComponent(action)}/runs`, { json: request })
      .json<ApiDataResponse<Run>>()
  ).data;
}

export async function listRuns(filters: RunFilters): Promise<RunPage> {
  const searchParams = new URLSearchParams();
  if (filters.q) searchParams.set("q", filters.q);
  if (filters.blueprint) searchParams.set("blueprint", filters.blueprint);
  if (filters.entity) searchParams.set("entity", filters.entity);
  if (filters.parent) searchParams.set("parent", filters.parent);
  if (filters.statuses?.length) searchParams.set("status", filters.statuses.join(","));
  if (filters.mine) searchParams.set("mine", "true");
  if (filters.page) searchParams.set("page", String(filters.page));
  if (filters.size) searchParams.set("size", String(filters.size));
  const path = filters.action
    ? `actions/${encodeURIComponent(filters.action)}/runs`
    : filters.workflow
      ? `workflows/${encodeURIComponent(filters.workflow)}/runs`
      : "runs";
  return (await api.get(path, { searchParams }).json<ApiDataResponse<RunPage>>()).data;
}

export async function startWorkflow(identifier: string, request: RunRequest): Promise<Run> {
  return (
    await api
      .post(`workflows/${encodeURIComponent(identifier)}/runs`, { json: request })
      .json<ApiDataResponse<Run>>()
  ).data;
}

export async function getSelfServiceCatalog(): Promise<SelfServiceCatalog> {
  return (await api.get("self-service").json<ApiDataResponse<SelfServiceCatalog>>()).data;
}

export async function getRun(id: string): Promise<Run> {
  return (await api.get(runPath(id)).json<ApiDataResponse<Run>>()).data;
}

export async function cancelRun(id: string): Promise<Run> {
  return (await api.post(`${runPath(id)}/cancel`).json<ApiDataResponse<Run>>()).data;
}

export async function retryRun(id: string): Promise<Run> {
  return (await api.post(`${runPath(id)}/retry`).json<ApiDataResponse<Run>>()).data;
}

export async function resolveRun(id: string, resolution: Resolution): Promise<Run> {
  return (
    await api.post(`${runPath(id)}/resolve`, { json: resolution }).json<ApiDataResponse<Run>>()
  ).data;
}

export async function decideRun(id: string, request: DecisionRequest): Promise<Run> {
  return (await api.post(`${runPath(id)}/decide`, { json: request }).json<ApiDataResponse<Run>>())
    .data;
}

// listApprovals lists the runs waiting on the signed-in user's approval,
// oldest first.
export async function listApprovals(): Promise<Run[]> {
  return (await api.get("approvals").json<ApiDataResponse<Run[]>>()).data;
}

// approvalDetail reads what an approval step shows, which only approval
// steps have.
export function approvalDetail(step: RunStep): ApprovalDetail | undefined {
  if (step.type !== "approval" || typeof step.detail !== "object" || step.detail === null) {
    return undefined;
  }
  return step.detail as ApprovalDetail;
}

// fanoutOutput reads what an action step gave, which only finished action
// steps have.
export function fanoutOutput(step: RunStep): FanoutOutput | undefined {
  if (step.type !== "action" || typeof step.output !== "object" || step.output === null) {
    return undefined;
  }
  return step.output as FanoutOutput;
}

// finished reports whether a run is over, one way or another.
export function finished(status: RunStatus) {
  return status !== "queued" && status !== "running" && status !== "waiting";
}
