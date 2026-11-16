import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import { Table } from "@/components/ui/table";

// labelCell is the class of the column that names each row: it takes the
// room the others leave and is cut short first, so numbers stay whole and
// the table fits its card where it can.
export const labelCell = "w-full max-w-0 min-w-32";

// WidgetTable is a widget's table. One too wide for its card scrolls, and
// fades at the edge while more of it is out of view.
export function WidgetTable({ children }: Readonly<{ children: ReactNode }>) {
  const ref = useRef<HTMLDivElement>(null);
  const [more, setMore] = useState(false);

  useLayoutEffect(() => {
    const container = ref.current?.querySelector<HTMLElement>("[data-slot=table-container]");
    if (!container) return;
    const update = () =>
      setMore(container.scrollLeft + container.clientWidth < container.scrollWidth - 1);
    update();
    container.addEventListener("scroll", update, { passive: true });
    const observer = new ResizeObserver(update);
    observer.observe(container);
    if (container.firstElementChild) observer.observe(container.firstElementChild);
    return () => {
      container.removeEventListener("scroll", update);
      observer.disconnect();
    };
  }, []);

  return (
    <div ref={ref} className="relative">
      <Table>{children}</Table>
      {more && (
        <div
          aria-hidden
          className="pointer-events-none absolute inset-y-0 right-0 w-8 bg-linear-to-l from-card to-transparent"
        />
      )}
    </div>
  );
}
