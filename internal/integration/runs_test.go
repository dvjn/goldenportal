package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gofr.dev/pkg/gofr/container"
)

func newRuns(t *testing.T) (*Runs, sqlmock.Sqlmock) {
	t.Helper()
	c, mocks := container.NewMockContainer(t)

	return NewRuns(c.SQL), mocks.SQL
}

func TestFinishSettlesTheStatus(t *testing.T) {
	cases := []struct {
		name    string
		run     Run
		failure string
		want    Status
	}{
		{"clean run", Run{Counts: Counts{Upserted: 3}}, "", StatusSucceeded},
		{"run with warnings", Run{Warnings: []string{"kept 2 entities"}}, "", StatusPartial},
		{"run that skipped records", Run{Counts: Counts{Skipped: 1}}, "", StatusPartial},
		{"run with an error", Run{Warnings: []string{"kept 2 entities"}}, "GitLab answered 401", StatusFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run.finish(at, tc.failure)

			if tc.run.Status != tc.want || !tc.run.FinishedAt.Equal(at) || tc.run.Error != tc.failure {
				t.Fatalf("finish() = %+v, want %s", tc.run, tc.want)
			}
		})
	}
}

func TestFinishTruncatesLongErrorsAndManyWarnings(t *testing.T) {
	run := Run{}
	for index := range 150 {
		run.Warnings = append(run.Warnings, fmt.Sprintf("warning %d", index))
	}

	run.finish(at, strings.Repeat("é", 1500))

	if len([]rune(run.Error)) != maxMessageLength || !strings.HasSuffix(run.Error, "…") {
		t.Fatalf("error has %d characters, want %d ending in an ellipsis", len([]rune(run.Error)), maxMessageLength)
	}
	if len(run.Warnings) != maxWarnings || run.Warnings[maxWarnings-1] != "and 51 more warnings" {
		t.Fatalf("warnings = %d ending %q, want %d ending with the rest counted", len(run.Warnings), run.Warnings[len(run.Warnings)-1], maxWarnings)
	}
}

func TestStartMarksOtherRunningRunsAsInterrupted(t *testing.T) {
	runs, db := newRuns(t)
	db.ExpectQuery(queryStartRun).WithArgs("gitlab", TriggerManual, "bob@example.test", "", "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "started_at"}).AddRow(7, at))
	db.ExpectExec(queryInterruptRuns).WithArgs("gitlab", int64(7), interrupted).WillReturnResult(sqlmock.NewResult(0, 1))

	run, err := runs.Start(context.Background(), Run{Integration: "gitlab", Trigger: TriggerManual, Actor: "bob@example.test"})

	if err != nil || run.ID != 7 || run.Status != StatusRunning || !run.StartedAt.Equal(at) {
		t.Fatalf("Start() = %+v, %v, want run 7 running", run, err)
	}
	if err := db.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordKeepsTheLastHundredRuns(t *testing.T) {
	runs, db := newRuns(t)
	run := Run{Integration: "ci", Trigger: TriggerPush, Actor: "pipelines", Kind: "service", Mode: modeUpsert, StartedAt: at}
	run.finish(at, "")
	db.ExpectQuery(queryRecordRun).
		WithArgs("ci", TriggerPush, "pipelines", "service", modeUpsert, StatusSucceeded, at, at, 0, 0, 0, 0, "[]", "").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(12))
	db.ExpectExec(queryPruneRuns).WithArgs("ci", keptRuns).WillReturnResult(sqlmock.NewResult(0, 1))

	recorded, err := runs.Record(context.Background(), run)

	if err != nil || recorded.ID != 12 {
		t.Fatalf("Record() = %+v, %v, want run 12", recorded, err)
	}
	if err := db.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListReadsTheRunsNewestFirst(t *testing.T) {
	runs, db := newRuns(t)
	columns := []string{
		"id", "integration", "trigger", "actor_label", "kind", "mode", "status", "started_at", "finished_at",
		"fetched", "upserted", "deleted", "skipped", "warnings", "error",
	}
	db.ExpectQuery(queryListRuns).WithArgs("gitlab", 100).WillReturnRows(sqlmock.NewRows(columns).
		AddRow(2, "gitlab", "manual", "bob@example.test", "", "", "running", at, nil, 0, 0, 0, 0, []byte("[]"), "").
		AddRow(1, "gitlab", "schedule", "schedule", "", "", "failed", at, at, 0, 0, 0, 0, []byte(`["slow"]`), "unreachable"))

	listed, err := runs.List(context.Background(), "gitlab", 100)

	if err != nil || len(listed) != 2 || !listed[0].FinishedAt.IsZero() || listed[1].Warnings[0] != "slow" || listed[1].Error != "unreachable" {
		t.Fatalf("List() = %+v, %v", listed, err)
	}
}
