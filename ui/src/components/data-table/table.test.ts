import { describe, expect, it } from "vite-plus/test";
import { writeUiState } from "@/hooks/use-ui-state";
import { pageSize, paginationToSearch } from "./table";

describe("page size", () => {
  it("is the URL's, or else the size last picked", () => {
    expect(pageSize({})).toBe(10);
    writeUiState("pageSize", 50);
    expect(pageSize({})).toBe(50);
    expect(pageSize({ size: 20 })).toBe(20);
    // A size no table offers is ignored.
    writeUiState("pageSize", 7);
    expect(pageSize({})).toBe(10);
  });

  it("stays out of the URL while it's the size last picked", () => {
    writeUiState("pageSize", 50);
    expect(paginationToSearch({ pageIndex: 1, pageSize: 50 })).toEqual({
      page: 2,
      size: undefined,
    });
    // A shared link's size stays while paging.
    expect(paginationToSearch({ pageIndex: 1, pageSize: 20 })).toEqual({ page: 2, size: 20 });
  });
});
