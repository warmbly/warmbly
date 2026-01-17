package validate

func RoleName(name string) bool {
	if len(name) < 3 || len(name) > 100 {
		return false
	}
	return true
}
