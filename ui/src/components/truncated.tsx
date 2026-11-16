import { cn } from "cn";
import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

// Truncated cuts what a table cell shows to a width, with an ellipsis, so a
// long value can't squeeze the other columns. Cut short, it shows all of
// it, title or else the text it holds, on hover, on focus of it or a link
// in it, and on a tap; screen readers read it whole either way.
export function Truncated({
  title,
  className,
  children,
}: Readonly<{ title?: string; className?: string; children: ReactNode }>) {
  const ref = useRef<HTMLSpanElement>(null);
  const [cut, setCut] = useState(false);
  const [linked, setLinked] = useState(false);
  const [open, setOpen] = useState(false);
  const touched = useRef(false);
  const full = title ?? (typeof children === "string" ? children : undefined);

  useLayoutEffect(() => {
    const span = ref.current;
    if (!span) return;
    const measure = () => setCut(span.scrollWidth > span.clientWidth);
    measure();
    setLinked(span.querySelector("a, button") !== null);
    const observer = new ResizeObserver(measure);
    observer.observe(span);
    return () => observer.disconnect();
  }, []);

  const text = (
    <span
      ref={ref}
      // A cell that's only text takes focus when cut, so a keyboard reaches all of it.
      tabIndex={cut && !linked ? 0 : undefined}
      className={cn(
        "block max-w-64 min-w-0 truncate rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        className,
      )}
    >
      {children}
    </span>
  );
  if (!full) return text;
  return (
    <TooltipProvider>
      <Tooltip open={open && cut} onOpenChange={setOpen}>
        <TooltipTrigger
          asChild
          onPointerDown={(event) => {
            touched.current = event.pointerType === "touch";
          }}
          onClick={(event) => {
            // A tap on a link follows it; on text, it shows or hides the rest.
            if (!touched.current || linked) return;
            event.preventDefault();
            setOpen((was) => !was);
          }}
        >
          {text}
        </TooltipTrigger>
        <TooltipContent className="max-w-sm break-words">{full}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
