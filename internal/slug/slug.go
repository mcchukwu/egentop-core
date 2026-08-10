package slug

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/gosimple/slug"
	"github.com/mcchukwu/egentop/internal/apperrors"
)

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

func Generate(name string) string {
	return slug.Make(name)
}

func GenerateWithSuffix(name string) (string, error) {
	base := Generate(name)

	if base == "" {
		base = "org"
	}

	suffix, err := randomSuffix(5)
	if err != nil {
		return "", apperrors.ErrInternalServer
	}

	return fmt.Sprintf("%s-%s", base, suffix), nil
}

func randomSuffix(lenght int) (string, error) {
	result := make([]byte, lenght)

	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", apperrors.ErrInternalServer
		}

		result[i] = charset[n.Int64()]
	}

	return string(result), nil
}
