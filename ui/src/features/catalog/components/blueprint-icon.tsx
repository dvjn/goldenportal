import type { LucideProps } from "lucide-react";
import { cn } from "@/lib/utils";
import { blueprintIcon } from "../icons";

export function BlueprintIcon({ icon, ...props }: Readonly<{ icon?: string } & LucideProps>) {
  const Icon = blueprintIcon(icon);
  return <Icon aria-hidden="true" {...props} />;
}

export function BlueprintIconTile({
  icon,
  className,
}: Readonly<{ icon: string | undefined; className?: string }>) {
  return (
    <div
      className={cn(
        "flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary",
        className,
      )}
    >
      <BlueprintIcon icon={icon} className="size-5" />
    </div>
  );
}
