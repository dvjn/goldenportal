import { cn } from "cn";

export type BarColor = "gray" | "blue" | "green" | "yellow" | "orange" | "red" | "purple" | "teal";

export const barRowLinkClass =
  "-mx-2 block rounded-md px-2 py-1.5 hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

const barColorClasses: Record<BarColor, string> = {
  gray: "bg-zinc-500",
  blue: "bg-blue-500",
  green: "bg-green-500",
  yellow: "bg-yellow-500",
  orange: "bg-orange-500",
  red: "bg-red-500",
  purple: "bg-purple-500",
  teal: "bg-teal-500",
};

const toneClasses = {
  primary: "bg-primary",
  muted: "bg-muted-foreground",
};

export function BarRow({
  label,
  detail,
  fraction,
  tone = "primary",
  color,
}: Readonly<{
  label: string;
  detail: string;
  fraction: number;
  tone?: keyof typeof toneClasses;
  color?: BarColor;
}>) {
  const percent = Math.round(fraction * 100);
  const fill = color ? barColorClasses[color] : toneClasses[tone];
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-3 text-sm">
        <span className="min-w-0 truncate">{label}</span>
        <span className="shrink-0 text-muted-foreground tabular-nums">{detail}</span>
      </div>
      <div
        className="h-2 overflow-hidden rounded-full bg-muted"
        role="img"
        aria-label={`${label}: ${percent}%`}
      >
        <div className={cn("h-full rounded-full", fill)} style={{ width: `${percent}%` }} />
      </div>
    </div>
  );
}
