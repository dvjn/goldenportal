package token

import (
	"context"
	"errors"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/config"
)

func SCIMToken(source config.Config) (string, error) {
	token := source.Get("GOLDPORT_SCIM_TOKEN")
	if token == "" {
		return "", errors.New("set the required setting: GOLDPORT_SCIM_TOKEN")
	}

	return token, nil
}

type seeder interface {
	Seed(ctx context.Context, kind, name, token string) (bool, error)
}

// SeedSCIMToken returns a start hook that stores the SCIM token from the
// configuration, so a fresh install can provision users before anyone can
// sign in to create a token.
func SeedSCIMToken(tokens seeder, token string) func(*gofr.Context) error {
	return func(ctx *gofr.Context) error {
		seeded, err := tokens.Seed(ctx, KindSCIM, "GOLDPORT_SCIM_TOKEN", token)
		if err != nil {
			return err
		}
		if seeded {
			ctx.Logger.Info("seeded the SCIM token from GOLDPORT_SCIM_TOKEN")
		}

		return nil
	}
}
