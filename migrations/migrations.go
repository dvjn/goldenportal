package migrations

import "gofr.dev/pkg/gofr/migration"

func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20261024150000: createUsersAndSessions(),
		20261024170000: createAPITokens(),
	}
}
