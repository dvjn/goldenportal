import ReactMarkdown, { type Components } from "react-markdown";

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
};

// Markdown shows text written in Markdown, without raw HTML.
export function Markdown({ children }: Readonly<{ children: string }>) {
  return (
    <div className="text-sm">
      <ReactMarkdown components={components}>{children}</ReactMarkdown>
    </div>
  );
}
