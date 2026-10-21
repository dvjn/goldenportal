import { lazy, Suspense } from "react";
import type { YamlEditorProps } from "./yaml-editor";

const YamlEditor = lazy(() => import("./yaml-editor"));

// LazyYamlEditor loads Monaco only when a page shows the editor, so the
// main bundle stays small.
export function LazyYamlEditor(props: YamlEditorProps) {
  return (
    <Suspense fallback={<p className="text-muted-foreground p-4 text-sm">Loading the editor</p>}>
      <YamlEditor {...props} />
    </Suspense>
  );
}
