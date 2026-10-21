import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";

declare module "vite-plus/test" {
  interface Matchers<T> extends TestingLibraryMatchers<unknown, T> {}
}
