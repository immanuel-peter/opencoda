package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
)

// TokenValidator validates Bearer tokens against CodaToken CRs.
type TokenValidator struct {
	client client.Client
}

func NewTokenValidator(c client.Client) *TokenValidator {
	return &TokenValidator{client: c}
}

func (t *TokenValidator) Validate(ctx context.Context, authHeader string) bool {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return false
	}
	tokenID, secret := parts[0], parts[1]
	var tokens opencodav1alpha1.CodaTokenList
	if err := t.client.List(ctx, &tokens); err != nil {
		return false
	}
	hash := sha256.Sum256([]byte(secret))
	hashHex := hex.EncodeToString(hash[:])
	for _, tok := range tokens.Items {
		if tok.Spec.TokenID != tokenID {
			continue
		}
		if tok.Spec.SecretHash != hashHex {
			return false
		}
		if tok.Spec.ExpiresAt != nil && tok.Spec.ExpiresAt.Time.Before(time.Now()) {
			return false
		}
		return true
	}
	return false
}

// CreateToken creates a CodaToken CR with hashed secret.
func CreateToken(ctx context.Context, c client.Client, namespace, tokenID string) (string, error) {
	secret := fmt.Sprintf("%s-%d", tokenID, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(secret))
	cr := &opencodav1alpha1.CodaToken{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tokenID,
			Namespace: namespace,
		},
		Spec: opencodav1alpha1.CodaTokenSpec{
			TokenID:    tokenID,
			SecretHash: hex.EncodeToString(hash[:]),
		},
	}
	if err := c.Create(ctx, cr); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%s", tokenID, secret), nil
}
