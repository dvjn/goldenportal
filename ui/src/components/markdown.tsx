import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

// Elements are styled one by one, since there's no typography plugin.
const components: Components = {
  p: ({ node: _, ...props }) => <p className="leading-6 not-first:mt-3" {...props} />,
  a: ({ node: _, ...props }) => (
    <a
      className="font-medium underline underline-offset-4"
      target="_blank"
      rel="noreferrer"
      {...props}
    />
  ),
  ul: ({ node: _, ...props }) => <ul className="my-3 ml-5 list-disc [&>li]:mt-1" {...props} />,
  ol: ({ node: _, ...props }) => <ol className="my-3 ml-5 list-decimal [&>li]:mt-1" {...props} />,
  h1: ({ node: _, ...props }) => <h3 className="mt-4 font-semibold first:mt-0" {...props} />,
  h2: ({ node: _, ...props }) => <h3 className="mt-4 font-semibold first:mt-0" {...props} />,
  h3: ({ node: _, ...props }) => <h4 className="mt-3 font-medium first:mt-0" {...props} />,
  code: ({ node: _, ...props }) => (
    <code className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]" {...props} />
  ),
  pre: ({ node: _, ...props }) => (
    <pre className="my-3 overflow-x-auto rounded-md bg-muted p-3 text-xs" {...props} />
  ),
  table: ({ node: _, ...props }) => (
    <div className="my-3 rounded-md border">
      <Table {...props} />
    </div>
  ),
  thead: ({ node: _, ...props }) => <TableHeader {...props} />,
  tbody: ({ node: _, ...props }) => <TableBody {...props} />,
  tr: ({ node: _, ...props }) => <TableRow {...props} />,
  th: ({ node: _, ...props }) => <TableHead {...props} />,
  td: ({ node: _, ...props }) => <TableCell {...props} />,
};

// GitHub's flavor adds tables, task lists and strikethrough, as in its comments.
const plugins = [remarkGfm];

// Markdown shows text written in Markdown. Raw HTML in it shows as text
// rather than running, since no plugin parses it.
export function Markdown({ children }: Readonly<{ children: string }>) {
  return (
    <div className="text-sm">
      <ReactMarkdown components={components} remarkPlugins={plugins}>
        {children}
      </ReactMarkdown>
    </div>
  );
}
