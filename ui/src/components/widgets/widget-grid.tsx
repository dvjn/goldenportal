import { useQuery } from "@tanstack/react-query";
import { cn } from "cn";
import { ArrowRight, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";
import { CardTitleBar } from "@/components/card-sections";
import { Markdown } from "@/components/markdown";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  type GroupData,
  type LinkData,
  type StatData,
  type TableData,
  type TextData,
  type TierData,
  type Widget,
  type WidgetContext,
  type WidgetData,
  type WidgetSource,
  widgetsQueryOptions,
} from "@/features/widgets";
import { GroupWidget } from "./group-widget";
import { PropertiesWidget } from "./properties-widget";
import { ScorecardWidget } from "./scorecard-widget";
import { StatWidget } from "./stat-widget";
import { TableWidget } from "./table-widget";
import { linkable, WidgetLink } from "./widget-link";

// Spans are spelled out so Tailwind sees them. Below lg, half-width or
// narrower widgets pair up and wider ones take the row.
const spans = [
  "",
  "md:col-span-6 lg:col-span-1",
  "md:col-span-6 lg:col-span-2",
  "md:col-span-6 lg:col-span-3",
  "md:col-span-6 lg:col-span-4",
  "md:col-span-6 lg:col-span-5",
  "md:col-span-6 lg:col-span-6",
  "md:col-span-12 lg:col-span-7",
  "md:col-span-12 lg:col-span-8",
  "md:col-span-12 lg:col-span-9",
  "md:col-span-12 lg:col-span-10",
  "md:col-span-12 lg:col-span-11",
  "md:col-span-12 lg:col-span-12",
];

// loads reports whether a widget's data comes from the server. Properties
// widgets show the page's entity, and a dashboard's markdown comes with it
// already filled in.
function loads(widget: Widget, source: WidgetSource) {
  if (widget.type === "properties") return false;
  return widget.type !== "markdown" || "blueprint" in source;
}

// WidgetGrid lays widgets out on a 12-column grid, in order, each as tall
// as it needs. Source is the document they're written in. Their data loads
// in one request, again whenever the context changes, and each widget
// shows its own share, or why it couldn't load.
export function WidgetGrid({
  widgets,
  source,
  context,
}: Readonly<{
  widgets: Widget[];
  source: WidgetSource;
  context: WidgetContext;
}>) {
  const results = useQuery({
    ...widgetsQueryOptions(source, context),
    enabled: widgets.some((widget) => loads(widget, source)),
  });
  return (
    <div className="grid grid-cols-12 items-start gap-4">
      {widgets.map((widget) => {
        const result = results.data?.widgets[widget.identifier];
        let error = results.error ?? undefined;
        if (result?.error !== undefined) error = new Error(result.error);
        else if (results.data && !result && loads(widget, source)) {
          error = new Error("The widget's data didn't come back.");
        }
        return (
          <div key={widget.identifier} className={cn("col-span-12 grid", spans[widget.width])}>
            <WidgetBody
              source={source}
              widget={widget}
              context={context}
              data={result?.data}
              error={error}
            />
          </div>
        );
      })}
    </div>
  );
}

// WidgetTitle is a widget's title, with its description, such as the
// period it covers, on hover rather than taking up the card.
export function WidgetTitle({ widget }: Readonly<{ widget: Widget }>) {
  if (!widget.description) return <>{widget.title}</>;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-help underline decoration-muted-foreground/50 decoration-dotted underline-offset-4">
          {widget.title}
        </span>
      </TooltipTrigger>
      <TooltipContent>{widget.description}</TooltipContent>
    </Tooltip>
  );
}

// WidgetProps are what a widget shows: its data, once loaded, as its type
// has it, or why it couldn't load.
export type WidgetProps<D extends WidgetData = WidgetData> = Readonly<{
  source: WidgetSource;
  widget: Widget;
  context: WidgetContext;
  data?: D;
  error?: Error;
}>;

// The server answers each widget with data of its type.
function WidgetBody(props: WidgetProps) {
  switch (props.widget.type) {
    case "stat":
      return <StatWidget {...(props as WidgetProps<StatData>)} />;
    case "bar":
    case "donut":
      return <GroupWidget {...(props as WidgetProps<GroupData>)} />;
    case "table":
      return <TableWidget {...(props as WidgetProps<TableData>)} />;
    case "scorecard":
      return <ScorecardWidget {...(props as WidgetProps<TierData>)} />;
    case "properties":
      return <PropertiesWidget {...props} />;
    case "markdown":
      return <MarkdownWidget {...(props as WidgetProps<TextData>)} />;
  }
}

// MarkdownWidget shows its text, with its {{ }} variables filled in for
// whoever is looking. A dashboard comes to them already filled in, while a
// blueprint's pages come as published, so there the text loads like any
// widget's data.
function MarkdownWidget({ source, widget, data, error }: WidgetProps<TextData>) {
  if (!("blueprint" in source)) return <MarkdownCard widget={widget} text={widget.text ?? ""} />;
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) return <WidgetLoading widget={widget} />;
  return <MarkdownCard widget={widget} text={data.text} />;
}

function MarkdownCard({ widget, text }: Readonly<{ widget: Widget; text: string }>) {
  return (
    <WidgetCard widget={widget}>
      <Markdown>{text}</Markdown>
    </WidgetCard>
  );
}

// WidgetCard is the frame every widget shares: its title, an action such as
// a link to everything it summarizes, and its content.
export function WidgetCard({
  widget,
  action,
  flush,
  children,
}: Readonly<{ widget: Widget; action?: ReactNode; flush?: boolean; children: ReactNode }>) {
  return (
    <Card className={cn(flush && "gap-0 pb-0")}>
      {(widget.title || action) && (
        <CardTitleBar
          title={<WidgetTitle widget={widget} />}
          action={action && <span className="text-xs">{action}</span>}
        />
      )}
      <CardContent className={cn("flex-1", flush && "px-0")}>{children}</CardContent>
    </Card>
  );
}

// ViewAll is a card's link to everything it summarizes, when it has one.
// It reads the same however much there is.
export function ViewAll({ link }: Readonly<{ link: LinkData | undefined }>) {
  if (!linkable(link)) return null;
  return (
    <WidgetLink
      link={link}
      className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
    >
      View all
      <ArrowRight className="size-3.5" aria-hidden />
    </WidgetLink>
  );
}

// WidgetError says why a widget couldn't load, in its frame.
export function WidgetError({ widget, error }: Readonly<{ widget: Widget; error: Error }>) {
  return (
    <WidgetCard widget={widget}>
      <p className="flex items-start gap-2 text-sm text-muted-foreground">
        <TriangleAlert className="mt-0.5 size-4 shrink-0 text-amber-500" aria-hidden />
        {error.message}
      </p>
    </WidgetCard>
  );
}

// WidgetLoading holds a widget's place while it loads.
export function WidgetLoading({
  widget,
  height = "h-16",
}: Readonly<{ widget: Widget; height?: string }>) {
  return (
    <WidgetCard widget={widget}>
      <Skeleton className={height} aria-hidden />
      <span className="sr-only">Loading</span>
    </WidgetCard>
  );
}
