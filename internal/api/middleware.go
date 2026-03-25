package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type Identity struct {
	ID   uuid.UUID
	Type string
}

type identityKey struct{}

func RequireIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identityID := r.Header.Get("x-identity-id")
		identityType := r.Header.Get("x-identity-type")
		if identityID == "" || identityType == "" {
			writeError(w, http.StatusUnauthorized, "missing identity")
			return
		}
		parsedID, err := uuid.Parse(identityID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid identity")
			return
		}
		identity := Identity{ID: parsedID, Type: identityType}
		ctx := context.WithValue(r.Context(), identityKey{}, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey{}).(Identity)
	return identity, ok
}
