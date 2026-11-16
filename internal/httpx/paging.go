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

// PageTotal is how many items match in all when a page of size from
// offset, holding got of them, shows it: it has room to spare, or the list
// is empty. A full page, or one past the end, needs a count.
func PageTotal(offset, got, size int) (total int, known bool) {
	if got < size && (got > 0 || offset == 0) {
		return offset + got, true
	}

	return 0, false
}

// Paging reads the page and size parameters of a list request. Absent
// ones mean the first page of the smallest size; anything else that isn't a
// page from 1 or an allowed size is a 400.
func Paging(param func(name string) string) (page, size int, err error) {
	page = 1
	if value := param("page"); value != "" {
		if page, err = strconv.Atoi(value); err != nil || page < 1 {
			return 0, 0, BadRequest("page must be a whole number from 1")
		}
	}
	size, err = Size(param)

	return page, size, err
}

// Size reads the size parameter of a list request: absent is the
// smallest, and anything else that isn't an allowed size is a 400.
func Size(param func(name string) string) (int, error) {
	value := param("size")
	if value == "" {
		return PageSizes[0], nil
	}
	size, err := strconv.Atoi(value)
	if err != nil || size == 0 {
		size = -1
	}

	return CheckSize(size)
}

// CheckSize checks a page size already read, where zero means absent,
// and returns the size to use.
func CheckSize(size int) (int, error) {
	if size == 0 {
		return PageSizes[0], nil
	}
	if !slices.Contains(PageSizes, size) {
		return 0, BadRequest("size must be 10, 20, 50 or 100")
	}

	return size, nil
}

// CheckPaging checks a page and size already read, where zero means absent,
// and returns the page and size to use. It is Paging for callers that take
// them from a request struct, such as MCP tools.
func CheckPaging(page, size int) (int, int, error) {
	switch {
	case page == 0:
		page = 1
	case page < 1:
		return 0, 0, BadRequest("page must be a whole number from 1")
	}
	size, err := CheckSize(size)
	if err != nil {
		return 0, 0, err
	}

	return page, size, nil
}
