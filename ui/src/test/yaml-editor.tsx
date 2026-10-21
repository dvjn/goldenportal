import type { YamlEditorProps } from "@/features/definitions/yaml-editor";

// YamlEditor stands in for Monaco, which cannot run in jsdom.
export function YamlEditor({ path, value, onChange, readOnly, markers }: YamlEditorProps) {
  return (
    <div>
      <textarea
        aria-label={`YAML of ${path}`}
        value={value}
        readOnly={readOnly}
        onChange={(event) => onChange(event.target.value)}
      />
      <ul aria-label="Editor markers">
        {markers.map((marker) => (
          <li key={`${marker.line}:${marker.message}`}>
            Line {marker.line}: {marker.message}
          </li>
        ))}
      </ul>
    </div>
  );
}
