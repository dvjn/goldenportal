import type { ReactNode } from "react";
import { Card, CardContent } from "@/components/ui/card";

export function StateCard({
  title,
  message,
  children,
}: {
  title: string;
  message: ReactNode;
  children?: ReactNode;
}) {
  return (
    <Card>
      <CardContent className="flex flex-col items-center gap-2 py-12 text-center text-sm text-muted-foreground">
        <div className="font-medium text-foreground">{title}</div>
        <div>{message}</div>
        {children}
      </CardContent>
    </Card>
  );
}
