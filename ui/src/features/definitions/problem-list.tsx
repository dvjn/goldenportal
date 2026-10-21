import type { Problem } from "./api";

export function where(problem: Problem) {
  return [
    problem.line ? `Line ${problem.line}` : undefined,
    problem.kind ? `${problem.kind} ${problem.identifier ?? ""}`.trim() : undefined,
    problem.path,
  ]
    .filter(Boolean)
    .join(", ");
}

export function ProblemList({ problems }: { problems: Problem[] }) {
  return (
    <ul className="flex flex-col gap-1 text-sm">
      {problems.map((problem) => (
        <li key={`${problem.document}:${problem.line}:${problem.path}:${problem.message}`}>
          <span className="font-medium">{where(problem)}</span>
          {where(problem) && ": "}
          {problem.message}
        </li>
      ))}
    </ul>
  );
}
