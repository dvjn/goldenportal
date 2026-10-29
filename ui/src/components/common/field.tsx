import { cn } from "cn";
import type { ReactNode } from "react";
import { Label } from "@/components/ui/label";

export function RequiredMark() {
  return <span className="text-destructive">*</span>;
}

export function Field({
  label,
  htmlFor,
  required,
  className,
  children,
}: {
  label: string;
  htmlFor?: string;
  required?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div className={cn("space-y-2", className)}>
      <Label htmlFor={htmlFor}>
        {label} {required ? <RequiredMark /> : null}
      </Label>
      {children}
    </div>
  );
}
