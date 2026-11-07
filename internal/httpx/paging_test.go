package httpx

import (
	"net/http"
	"testing"
)

func TestPaging(t *testing.T) {
	tests := []struct {
		name       string
		page, size string
		wantPage   int
		wantSize   int
		bad        bool
	}{
		{name: "absent means first page, smallest size", wantPage: 1, wantSize: 10},
		{name: "given", page: "3", size: "50", wantPage: 3, wantSize: 50},
		{name: "largest size", size: "100", wantPage: 1, wantSize: 100},
		{name: "page zero", page: "0", bad: true},
		{name: "negative page", page: "-1", bad: true},
		{name: "page not a number", page: "x", bad: true},
		{name: "size not allowed", size: "15", bad: true},
		{name: "size zero", size: "0", bad: true},
		{name: "size not a number", size: "big", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			param := func(name string) string {
				if name == "page" {
					return tt.page
				}
				return tt.size
			}
			page, size, err := Paging(param)
			if tt.bad {
				e, ok := err.(Error)
				if !ok || e.Code != http.StatusBadRequest {
					t.Fatalf("Paging() error = %v, want a 400", err)
				}
				return
			}
			if err != nil || page != tt.wantPage || size != tt.wantSize {
				t.Errorf("Paging() = %d, %d, %v; want %d, %d", page, size, err, tt.wantPage, tt.wantSize)
			}
		})
	}
}

func TestCheckPaging(t *testing.T) {
	if page, size, err := CheckPaging(0, 0); err != nil || page != 1 || size != 10 {
		t.Errorf("CheckPaging(0, 0) = %d, %d, %v", page, size, err)
	}
	if page, size, err := CheckPaging(2, 20); err != nil || page != 2 || size != 20 {
		t.Errorf("CheckPaging(2, 20) = %d, %d, %v", page, size, err)
	}
	if _, _, err := CheckPaging(0, 7); err == nil {
		t.Error("CheckPaging(0, 7) accepted a size that isn't allowed")
	}
	if _, _, err := CheckPaging(-1, 0); err == nil {
		t.Error("CheckPaging(-1, 0) accepted a negative page")
	}
}
