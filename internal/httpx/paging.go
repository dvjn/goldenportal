package httpx

import (
	"slices"
	"strconv"
)

// PageSizes are the page sizes a list endpoint allows, smallest first.
var PageSizes = []int{10, 20, 50, 100}

// Page is one page of a list, and how many items match in all.
type Page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// Paging reads the page and size parameters of a list request. Absent
// ones mean the first page of the smallest size; anything else that isn't a
// page from 1 or an allowed size is a 400.
func Paging(param func(name string) string) (page, size int, err error) {
	page, size = 1, PageSizes[0]
	if value := param("page"); value != "" {
		if page, err = strconv.Atoi(value); err != nil || page < 1 {
			return 0, 0, BadRequest("page must be a whole number from 1")
		}
	}
	if value := param("size"); value != "" {
		if size, err = strconv.Atoi(value); err != nil || !slices.Contains(PageSizes, size) {
			return 0, 0, BadRequest("size must be 10, 20, 50 or 100")
		}
	}

	return page, size, nil
}

// CheckPaging checks a page and size already read, where zero means absent,
// and returns the page and size to use. It is Paging for callers that take
// them from a request struct, such as MCP tools.
func CheckPaging(page, size int) (int, int, error) {
	param := func(name string) string {
		n := page
		if name == "size" {
			n = size
		}
		if n == 0 {
			return ""
		}

		return strconv.Itoa(n)
	}

	return Paging(param)
}
