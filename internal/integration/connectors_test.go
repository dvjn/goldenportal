package integration

import (
	"net/http"
	"slices"
	"testing"
)

func TestEveryConnectorReadsTheKindsItsDefinitionsAccept(t *testing.T) {
	for name, source := range Connectors(http.DefaultClient) {
		if !slices.Equal(source.Kinds(), name.Kinds()) {
			t.Errorf("%s reads %v, its definitions accept %v", name, source.Kinds(), name.Kinds())
		}
	}
}
