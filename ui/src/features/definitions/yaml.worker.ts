// Vite needs the worker entry in the project to bundle monaco-yaml's worker,
// see https://github.com/remcohaszing/monaco-yaml#why-doesnt-it-work-with-vite
import "monaco-yaml/yaml.worker.js";
