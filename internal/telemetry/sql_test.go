package telemetry

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/XSAM/otelsql"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

func TestSummarize(t *testing.T) {
	for query, want := range map[string]string{
		`SELECT id FROM entities WHERE blueprint_id = $1`:                                       "SELECT entities",
		"\n\t\tINSERT INTO ingestion_runs (integration_id) VALUES ($1) RETURNING id":            "INSERT ingestion_runs",
		`UPDATE runs SET status = $2 WHERE id = $1`:                                             "UPDATE runs",
		`DELETE FROM ingestion_runs WHERE id IN (SELECT id FROM ingestion_runs LIMIT $2)`:       "DELETE ingestion_runs",
		`select (count(*))::float8 from entities e where e.properties @> $1::jsonb`:             "SELECT entities",
		`SELECT g.value FROM (SELECT e.properties -> 'tier' AS value FROM entities e) g`:        "SELECT",
		`WITH recent AS (SELECT id FROM runs) UPDATE "runs" SET seen = true FROM recent`:        "UPDATE runs",
		`SELECT pg_try_advisory_xact_lock(hashtext('integration-sync:' || $1))`:                 "SELECT",
		`SELECT 'FROM secrets' AS label FROM config_documents d JOIN config_versions v ON true`: "SELECT config_documents",
		`BEGIN`: "SQL",
	} {
		if got, _, _ := summarize(query); got != want {
			t.Errorf("summarize(%q) = %q, want %q", query, got, want)
		}
	}
}

// TestSQLSpans checks a statement's span is named after it, with the
// database conventions' attributes, under the work that ran it, and that
// spans with no parent, no statement or asked to be quiet are left out.
func TestSQLSpans(t *testing.T) {
	InstallSQL()
	db, mock, err := sqlmock.NewWithDSN("telemetry-sql-spans")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// otelsql keeps the provider in place when the database opens, as gofr
	// opens it before installing its own.
	traced, err := otelsql.Open("sqlmock", "telemetry-sql-spans")
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	exporter := recordSpans(t)

	mock.ExpectExec(`UPDATE runs`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO entities`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM runs`).WillReturnResult(sqlmock.NewResult(0, 1))
	ctx, span := Start(t.Context(), "action run")
	if _, err := traced.ExecContext(ctx, `UPDATE runs SET status = $1`, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := traced.ExecContext(QuietSQL(ctx), `INSERT INTO entities (id) VALUES ($1)`, "a"); err != nil {
		t.Fatal(err)
	}
	span.End()
	if _, err := traced.ExecContext(t.Context(), `DELETE FROM runs`); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	var names []string
	for _, s := range spans {
		names = append(names, s.Name)
	}
	if len(spans) != 2 || spans[0].Name != "UPDATE runs" || spans[1].Name != "action run" {
		t.Fatalf("spans = %v, want the update under the action run, and nothing for the quiet insert or the statement without a parent", names)
	}
	update := spans[0]
	if update.Parent.SpanID() != spans[1].SpanContext.SpanID() {
		t.Error("the statement's span isn't under the work that ran it")
	}
	attrs := attributesOf(update)
	if attrs[semconv.DBSystemNameKey].AsString() != "postgresql" || attrs[semconv.DBOperationNameKey].AsString() != "UPDATE" ||
		attrs[semconv.DBCollectionNameKey].AsString() != "runs" || attrs[semconv.DBQueryTextKey].AsString() != `UPDATE runs SET status = $1` {
		t.Errorf("attributes = %v", attrs)
	}
}
