package auth

type AuthData struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	Turnstile string `json:"turnstile"`
}

type ConfirmData struct {
	Session   string `json:"session"`
	Code      string `json:"code"`
	Turnstile string `json:"turnstile"`
}

type ResetPasswordStart struct {
	Email     string `json:"email"`
	Turnstile string `json:"turnstile"`
}

type ResetPasswordConfirm struct {
	Session   string `json:"session"`
	Password  string `json:"password"`
	Turnstile string `json:"turnstile"`
}
