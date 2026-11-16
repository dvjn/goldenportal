package access

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
	gofrSQL "gofr.dev/pkg/gofr/datasource/sql"
	gofrHTTP "gofr.dev/pkg/gofr/http"
	"gofr.dev/pkg/gofr/logging"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/httpx/httpxtest"
)

func searchAs(t *testing.T, db *gofrSQL.DB, p *auth.Principal, q string) (any, error) {
	t.Helper()
	routes := httpxtest.New()
	Register(routes)
	req := httptest.NewRequest(http.MethodGet, "/api/web/settings/access/subjects?q="+url.QueryEscape(q), nil)
	ctx := req.Context()
	if p != nil {
		ctx = auth.WithPrincipal(ctx, *p)
	}
	req = req.WithContext(ctx)
	logger := logging.NewLogger(logging.FATAL)

	return routes.Handlers["GET settings/access/subjects"](&gofr.Context{
		Context: ctx, Request: gofrHTTP.NewRequest(req),
		Container: &container.Container{Logger: logger, SQL: db}, ContextLogger: logging.ContextLoggerFor(ctx, logger),
	})
}

func status(err error) int {
	var s interface{ StatusCode() int }
	if errors.As(err, &s) {
		return s.StatusCode()
	}

	return 0
}

func TestSearchSubjectsIsForAdmins(t *testing.T) {
	db, _, _ := gofrSQL.NewSQLMocks(t)
	if _, err := searchAs(t, db, nil, "ada"); status(err) != http.StatusUnauthorized {
		t.Errorf("signed out: err = %v, want unauthorized", err)
	}
	builder := auth.Principal{ID: "b", Role: auth.RoleBuilder}
	if _, err := searchAs(t, db, &builder, "ada"); status(err) != http.StatusForbidden {
		t.Errorf("builder: err = %v, want forbidden", err)
	}
}

func TestSearchSubjectsNeedsText(t *testing.T) {
	db, mock, _ := gofrSQL.NewSQLMocks(t)
	admin := auth.Principal{ID: "a", Role: auth.RoleAdmin}
	got, err := searchAs(t, db, &admin, "  ")
	if found, ok := got.([]Subject); err != nil || !ok || len(found) != 0 {
		t.Fatalf("blank search = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// The search text is trimmed, its LIKE wildcards match themselves, and the
// caller is left out.
func TestSearchSubjectsAsksForTheText(t *testing.T) {
	db, mock, metrics := gofrSQL.NewSQLMocks(t)
	metrics.EXPECT().RecordHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mock.ExpectQuery(searchSQL).WithArgs("50%_off", "admin-id", `50\%\_off`, searchLimit).
		WillReturnRows(sqlmock.NewRows([]string{"type", "id", "name", "email", "members"}).
			AddRow("group", "g1", "50%_off", "", 3).
			AddRow("user", "u1", "Grace Hopper", "grace@example.com", nil))
	admin := auth.Principal{ID: "admin-id", Role: auth.RoleAdmin}

	got, err := searchAs(t, db, &admin, " 50%_off ")
	if err != nil {
		t.Fatal(err)
	}
	found := got.([]Subject)
	if len(found) != 2 || found[0].MemberCount == nil || *found[0].MemberCount != 3 ||
		found[1].MemberCount != nil || found[1].Email != "grace@example.com" {
		t.Errorf("found = %+v", found)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
