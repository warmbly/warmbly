package utils

import "regexp"

func IsValidJSONKey(key string) bool {
	const maxKeyLength = 255
	if len(key) == 0 || len(key) > maxKeyLength {
		return false
	}
	matched, err := regexp.MatchString(`^[a-zA-Z0-9_]+$`, key)
	if err != nil {
		return false
	}
	if !matched {
		return false
	}
	return true
}
