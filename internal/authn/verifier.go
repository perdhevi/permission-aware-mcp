package authn

import "context"

type TokenVerifier interface {
	Verify(context.Context, string) (*Identity, error)
}
