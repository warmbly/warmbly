package crypt

import (
	"regexp"

	"github.com/google/uuid"
)

func IsValidUUID(u string) bool {
	_, err := uuid.Parse(u)
	return err == nil
}

func IsValidHexColor(s string) bool {
	return regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`).MatchString(s)
}
