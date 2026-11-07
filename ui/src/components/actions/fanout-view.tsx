import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  type FanoutRun,
  fanoutOutput,
  type Run,
  type RunStep,
  runsQueryOptions,
} from "@/features/actions";
import { RunStatus } from "./run-status";

// childPage is how many runs a step that's still waiting lists; the server
// pages runs by at most this many.
const childPage = 100;

// FanoutView is an action step as its runs went: once it's over, what it
// gave, and while it waits, the runs it started so far, read again until
// they're done.
export function FanoutView({ run, step }: Readonly<{ run: Run; step: RunStep }>) {
  const output = fanoutOutput(step);
  const waiting = !output && (step.status === "waiting" || step.status === "running");
  const { data } = useQuery({
    ...runsQueryOptions({ parent: run.id, size: childPage }),
    enabled: waiting,
  });
  if (output) return <FanoutList runs={output.runs} />;
  if (!waiting || !data) return null;
  // Runs other action steps started, and already list, aren't this step's.
  const others = new Set(
    (run.steps ?? []).flatMap((s) => fanoutOutput(s)?.runs.map((r) => r.id) ?? []),
  );
  return (
    <FanoutList
      runs={data.items
        .filter((child) => !others.has(child.id))
        .map((child) => ({
          id: child.id,
          entity: child.entity.identifier,
          title: child.entity.title,
          status: child.status,
          error: child.error,
        }))}
    />
  );
}

function FanoutList({ runs }: Readonly<{ runs: FanoutRun[] }>) {
  const succeeded = runs.filter((r) => r.status === "succeeded").length;
  return (
    <div className="grid gap-2">
      <p className="text-sm tabular-nums">
        <span className="font-medium">{succeeded}</span>
        <span className="text-muted-foreground"> of {runs.length} succeeded</span>
      </p>
      <ul className="grid gap-1.5" aria-label="Runs">
        {runs.map((child) => (
          <li
            key={child.id ?? `refused:${child.entity}`}
            className="grid grid-cols-[1rem_minmax(0,1fr)] gap-x-2 text-sm"
          >
            <span className="pt-0.5">
              {/* One that was refused never ran, so it counts as failed. */}
              <RunStatus status={child.status || "failed"} />
            </span>
            <div className="grid min-w-0 gap-0.5">
              {child.id ? (
                <Link to="/runs/$id" params={{ id: child.id }} className="truncate hover:underline">
                  {child.title || child.entity}
                </Link>
              ) : (
                <span className="truncate">{child.title || child.entity}</span>
              )}
              {child.error && (
                <span className="text-xs break-words text-destructive">{child.error}</span>
              )}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
