import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { Badge } from "@/components/ui/badge";
import type { ActionSpec } from "@/features/actions";

// ActionPreview sums up a new action, or workflow, while it's being written.
export function ActionPreview({ spec }: Readonly<{ spec: ActionSpec }>) {
  return (
    <dl className="grid gap-3 text-sm">
      <div className="grid gap-1.5">
        <dt className="text-muted-foreground">{spec.kind === "Workflow" ? "Group" : "Runs On"}</dt>
        <dd className="flex items-center gap-2">
          {spec.kind === "Workflow" ? spec.group : <BlueprintLabel blueprint={spec.blueprint} />}
          {spec.destructive && <Badge variant="destructive">Destructive</Badge>}
        </dd>
      </div>
      {spec.inputs.length > 0 && (
        <div className="grid gap-1.5">
          <dt className="text-muted-foreground">Inputs</dt>
          <dd className="flex flex-wrap gap-1">
            {spec.inputs.map((input) => (
              <Badge key={input.identifier} variant="outline">
                {input.title}
              </Badge>
            ))}
          </dd>
        </div>
      )}
      <div className="grid gap-1.5">
        <dt className="text-muted-foreground">Steps</dt>
        <dd>
          <ol className="grid list-decimal gap-1 pl-5">
            {spec.steps.map((step, index) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: steps have no identity of their own.
              <li key={index}>{step.title}</li>
            ))}
          </ol>
        </dd>
      </div>
    </dl>
  );
}
