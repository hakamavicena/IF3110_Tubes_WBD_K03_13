package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

type ctxKey int

const (
	correlationKey ctxKey = iota
	actorKey
)

var validCorrelationID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

func NewCorrelationID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func ValidCorrelationID(id string) bool { return validCorrelationID.MatchString(id) }

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}

func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey).(string)
	return id
}

func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey, actor)
}

func Actor(ctx context.Context) string {
	a, _ := ctx.Value(actorKey).(string)
	return a
}
