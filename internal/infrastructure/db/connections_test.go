package db

import (
	"strings"
	"testing"
)

func TestTransactionConnectionConfiguration(t *testing.T) {
	t.Setenv("DB_POOL_MODE", "transaction")
	const pooled = "postgres://app:fixture@pool.example:6432/app?sslmode=verify-full&statement_timeout=0&pool_max_conns=25"
	const direct = "postgres://app:fixture@db.example:5432/app?sslmode=verify-full&pool_max_conns=2"
	pool, session, transaction, err := connectionConfigs(pooled, direct)
	if err != nil {
		t.Fatal(err)
	}
	if !transaction || pool.ConnConfig.Port != 6432 || session.Port != 5432 {
		t.Fatal("pooled and direct endpoints were not kept separate")
	}
	if pool.AfterConnect == nil || pool.ConnConfig.RuntimeParams["statement_timeout"] != "" || pool.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] != "" {
		t.Fatal("pooler startup must verify role timeouts without sending unsupported parameters")
	}
	if session.RuntimeParams["statement_timeout"] != statementTimeoutMs || session.RuntimeParams["idle_in_transaction_session_timeout"] != idleInTxnTimeoutMs || session.RuntimeParams["pool_max_conns"] != "" {
		t.Fatal("direct sessions must enforce timeouts and omit pool-only options")
	}
	if pool.ConnConfig.TLSConfig == nil || session.TLSConfig == nil || pool.ConnConfig.TLSConfig.InsecureSkipVerify || session.TLSConfig.InsecureSkipVerify {
		t.Fatal("both connection paths must preserve TLS verification")
	}
	if name := pool.ConnConfig.RuntimeParams["application_name"]; !strings.HasPrefix(name, "warmbly-") || session.RuntimeParams["application_name"] != name {
		t.Fatalf("connections are not labelled with the process that opened them: %q", name)
	}
}

func TestApplicationNameFromTheDSNWins(t *testing.T) {
	pool, session, _, err := connectionConfigs("postgres://app:fixture@db.example:5432/app?application_name=ops", "")
	if err != nil {
		t.Fatal(err)
	}
	if pool.ConnConfig.RuntimeParams["application_name"] != "ops" || session.RuntimeParams["application_name"] != "ops" {
		t.Fatal("an operator's application_name was replaced")
	}
}

func TestDirectConnectionDefaults(t *testing.T) {
	t.Setenv("DB_POOL_MODE", "")
	const endpoint = "postgres://app:fixture@localhost:5432/app?sslmode=disable"
	pool, direct, transaction, err := connectionConfigs(endpoint, "")
	if err != nil {
		t.Fatal(err)
	}
	if transaction || pool.AfterConnect != nil || direct.Host != pool.ConnConfig.Host || direct.Port != pool.ConnConfig.Port {
		t.Fatal("direct installations must retain their connection behavior")
	}
	if pool.ConnConfig.RuntimeParams["statement_timeout"] != statementTimeoutMs || pool.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] != idleInTxnTimeoutMs {
		t.Fatal("direct clients must enforce startup timeouts")
	}
}

func TestConnectionConfigurationRefusesUnsafeTargets(t *testing.T) {
	const endpoint = "postgres://app:fixture@localhost:6432/app"
	for _, tc := range []struct {
		name, mode, primary, direct string
	}{
		{"missing direct", "transaction", endpoint, ""},
		{"same endpoint", "transaction", endpoint, endpoint},
		{"different database", "transaction", endpoint, "postgres://app:fixture@localhost:5432/other"},
		{"invalid mode", "typo", endpoint, ""},
		{"invalid primary", "transaction", "postgres://app:do-not-log-this@host:invalid/app", "postgres://localhost/app"},
		{"invalid direct", "transaction", endpoint, "postgres://app:do-not-log-this@host:invalid/app"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DB_POOL_MODE", tc.mode)
			_, _, _, err := connectionConfigs(tc.primary, tc.direct)
			if err == nil {
				t.Fatal("unsafe configuration was accepted")
			}
			if strings.Contains(err.Error(), "do-not-log-this") || strings.Contains(err.Error(), "postgres://") {
				t.Fatal("connection configuration error contains credentials")
			}
		})
	}
}
