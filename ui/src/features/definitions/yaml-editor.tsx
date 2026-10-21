import Editor, { loader } from "@monaco-editor/react";
import "monaco-editor/esm/vs/editor/editor.all.js";
import "monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution.js";
import { createWebWorker } from "monaco-editor/esm/vs/common/workers.js";
import * as monaco from "monaco-editor/esm/vs/editor/editor.api.js";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import { configureMonacoYaml, type MonacoYaml } from "monaco-yaml";
import { useEffect, useState } from "react";
import { useTheme } from "@/components/theme-provider";
import YamlWorker from "./yaml.worker?worker";

export type Marker = { line: number; column: number; message: string };

export type YamlEditorProps = {
  path: string;
  value: string;
  onChange: (value: string) => void;
  readOnly: boolean;
  markers: Marker[];
  schema: object;
};

self.MonacoEnvironment = {
  getWorker: (_, label) => (label === "yaml" ? new YamlWorker() : new EditorWorker()),
};
loader.config({ monaco });

// monaco-yaml creates its worker through monaco.editor.createWebWorker with a
// module ID, which that function stopped taking in monaco-editor 0.54. The
// module ID form lives on as monaco-editor's top-level createWebWorker.
const monacoForYaml = {
  ...monaco,
  editor: {
    ...monaco.editor,
    createWebWorker: createWebWorker as unknown as typeof monaco.editor.createWebWorker,
  },
};

const schemaUri = "https://goldport.dev/schemas/definition.json";
let monacoYaml: MonacoYaml | undefined;

function useSchema(schema: object) {
  useEffect(() => {
    const options = {
      enableSchemaRequest: false,
      schemas: [{ uri: schemaUri, fileMatch: ["*"], schema }],
    };
    if (monacoYaml) monacoYaml.update(options);
    else monacoYaml = configureMonacoYaml(monacoForYaml, options);
  }, [schema]);
}

function useDark() {
  const { theme } = useTheme();
  return (
    theme === "dark" || (theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches)
  );
}

export default function YamlEditor({
  path,
  value,
  onChange,
  readOnly,
  markers,
  schema,
}: YamlEditorProps) {
  const [mounted, setMounted] = useState(false);
  const dark = useDark();
  useSchema(schema);

  useEffect(() => {
    const model = mounted ? monaco.editor.getModel(monaco.Uri.parse(path)) : null;
    if (!model) return;
    monaco.editor.setModelMarkers(
      model,
      "goldport",
      markers.map((marker) => {
        const line = Math.min(marker.line, model.getLineCount());
        return {
          severity: monaco.MarkerSeverity.Error,
          message: marker.message,
          startLineNumber: line,
          startColumn: marker.column,
          endLineNumber: line,
          endColumn: model.getLineMaxColumn(line),
        };
      }),
    );
  }, [mounted, markers, path]);

  return (
    <Editor
      path={path}
      language="yaml"
      value={value}
      theme={dark ? "vs-dark" : "vs"}
      loading="Loading the editor"
      onMount={() => setMounted(true)}
      onChange={(text) => onChange(text ?? "")}
      options={{
        readOnly,
        minimap: { enabled: false },
        tabSize: 2,
        scrollBeyondLastLine: false,
        fontSize: 13,
      }}
    />
  );
}
