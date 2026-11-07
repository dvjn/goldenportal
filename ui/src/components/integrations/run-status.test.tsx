import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { RunStatus as ActionRunStatus, runStatuses } from "@/components/actions/run-status";
import { RunStatus, runOutcome, runOutcomes } from "@/components/integrations/run-status";
import type { Run } from "@/features/integrations";

const base = { finishedAt: "2025-01-01T00:00:00Z", error: "", failed: 0, warning: "" } as Run;

describe("run status mark", () => {
  it("sums an integration run up as one outcome", () => {
    expect(runOutcome({ ...base, finishedAt: null } as unknown as Run)).toBe("running");
    expect(runOutcome({ ...base, error: "boom" })).toBe("failed");
    expect(runOutcome({ ...base, failed: 2 })).toBe("warnings");
    expect(runOutcome({ ...base, warning: "careful" })).toBe("warnings");
    expect(runOutcome(base)).toBe("succeeded");
  });

  it("labels a run with warnings in Title Case", () => {
    expect(runOutcomes.warnings.label).toBe("Succeeded with Warnings");
  });

  it("shows both kinds of run as an icon with a label", () => {
    render(
      <>
        <RunStatus run={{ ...base, failed: 1 }} label />
        <ActionRunStatus status="rejected" label />
      </>,
    );
    expect(screen.getByLabelText("Succeeded with Warnings")).toBeInTheDocument();
    expect(screen.getByText("Succeeded with Warnings")).toBeInTheDocument();
    expect(screen.getByLabelText(runStatuses.rejected.label)).toBeInTheDocument();
  });
});
