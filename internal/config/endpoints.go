package config

func GetPasswordResetURL(sessionToken string) string {
	return "https://app.warmbly.com/auth/reset-password/confirm?session=" + sessionToken
}
