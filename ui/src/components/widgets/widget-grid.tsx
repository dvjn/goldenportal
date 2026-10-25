import { useQuery } from "@tanstack/react-query";
import { cn } from "cn";
import { TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";
import { Markdown } from "@/components/markdown";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  type TextData,
  type Widget,
  type WidgetContext,
  type WidgetSource,
  widgetQueryOptions,
} from "@/features/widgets";
import { GroupWidget } from "./group-widget";
import { PropertiesWidget } from "./properties-widget";
import { ScorecardWidget } from "./scorecard-widget";
import { StatWidget } from "./stat-widget";
import { TableWidget } from "./table-widget";

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

// WidgetGrid lays widgets out on a 12-column grid, in order. Source is the
// document they're written in, which their data loads through.
export function WidgetGrid({
  widgets,
  source,
  context,
}: Readonly<{ widgets: Widget[]; source: WidgetSource; context: WidgetContext }>) {
  return (
    <div className="grid grid-cols-12 gap-4">
      {widgets.map((widget) => (
        <div key={widget.identifier} className={cn("col-span-12 grid", spans[widget.width])}>
          <WidgetBody source={source} widget={widget} context={context} />
        </div>
      ))}
    </div>
  );
}

export type WidgetProps = Readonly<{
  source: WidgetSource;
  widget: Widget;
  context: WidgetContext;
}>;

function WidgetBody(props: WidgetProps) {
  switch (props.widget.type) {
    case "stat":
      return <StatWidget {...props} />;
    case "bar":
    case "donut":
      return <GroupWidget {...props} />;
    case "table":
      return <TableWidget {...props} />;
    case "scorecard":
      return <ScorecardWidget {...props} />;
    case "properties":
      return <PropertiesWidget {...props} />;
    case "markdown":
      return <MarkdownWidget {...props} />;
  }
}

// MarkdownWidget shows its text, with its {{ }} variables filled in for
// whoever is looking. A dashboard comes to them already filled in, while a
// blueprint's pages come as published, so there the text loads like any
// widget's data.
function MarkdownWidget({ source, widget, context }: WidgetProps) {
  const loads = "blueprint" in source;
  const data = useQuery({
    ...widgetQueryOptions<TextData>(source, widget.identifier, context),
    enabled: loads,
  });
  if (!loads) return <MarkdownCard widget={widget} text={widget.text ?? ""} />;
  if (data.error) return <WidgetError widget={widget} error={data.error} />;
  if (!data.data) return <WidgetLoading widget={widget} />;
  return <MarkdownCard widget={widget} text={data.data.text} />;
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
    <Card className={cn("gap-3", flush && "pb-0")}>
      {(widget.title || action) && (
        <CardHeader>
          <CardTitle className="text-sm">{widget.title}</CardTitle>
          {widget.description && <CardDescription>{widget.description}</CardDescription>}
          {action && <CardAction className="text-xs">{action}</CardAction>}
        </CardHeader>
      )}
      <CardContent className={cn("flex-1", flush && "px-0")}>{children}</CardContent>
    </Card>
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
      <div className={cn("animate-pulse rounded-md bg-muted", height)} aria-hidden />
      <span className="sr-only">Loading</span>
    </WidgetCard>
  );
}
