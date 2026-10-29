import type { ReactNode } from "react";
import { Table } from "@/components/ui/table";

const cellLayout =
  "[&_td]:px-3 [&_th]:px-3 [&_td:first-child]:pl-5 [&_th:first-child]:pl-5 [&_td:last-child]:pr-5 [&_th:last-child]:pr-5";

const cellAlignment = {
  baseline: "[&_td]:align-baseline",
  top: "[&_td]:align-top",
};

export function TableFrame({
  footer,
  align = "baseline",
  children,
}: Readonly<{ footer?: ReactNode; align?: keyof typeof cellAlignment; children: ReactNode }>) {
  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-xs">
      <Table className={`${cellLayout} ${cellAlignment[align]}`}>{children}</Table>
      {footer ? <div className="flex justify-center border-t px-5 py-3">{footer}</div> : null}
    </div>
  );
}
