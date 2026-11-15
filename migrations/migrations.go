package migrations

import "gofr.dev/pkg/gofr/migration"

func All() map[int64]migration.Migrate {
	return map[int64]migration.Migrate{
		20261024150000: createUsersAndSessions(),
		20261024170000: createAPITokens(),
		20261024190000: createGroups(),
		20261024210000: createAuditEvents(),
		20261025100000: addSCIMProvisioning(),
		20261025120000: createConfigDocuments(),
		20261025140000: createEntities(),
		20261025160000: createIngestion(),
		20261025180000: createScorecardResults(),
		20261029120000: addIngestionRunError(),
		20261101120000: addEntityValueIndexes(),
		20261102120000: createOAuth(),
		20261102130000: addOAuthClientMetadata(),
		20261103180000: createRuns(),
		20261104120000: addActionIngestionRuns(),
		20261105120000: connectorItems(),
		20261105140000: addIngestionRunWarning(),
		20261105150000: addEntityMissedAt(),
		20261106120000: addRunningIngestionRuns(),
		20261107120000: oneFormPerSetting(),
		20261108130000: indexPushRuns(),
		20261109120000: writeOnlyChanges(),
		20261109130000: reconcileByScope(),
		20261109150000: retentionAndCursors(),
		20261110120000: renameToConnections(),
		20261110140000: sourcesOnBlueprints(),
		20261116120000: evaluatedVersions(),
		20261116140000: dropErroredResults(),
		20261116150000: oauthGrantsPerEndpoint(),
		20261117120000: indexableQueryShapes(),
	}
}
