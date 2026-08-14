package confenge

import "testing"

func TestAllowSilentEnrollMintProductionFailClosed(t *testing.T) {
	prod := Config{AppEnv: "production"}
	if prod.AllowSilentEnrollMint() {
		t.Fatal("production must not allow silent HUMAN_CONFIRMED mint")
	}
	prod2 := Config{AppEnv: "prod"}
	if prod2.AllowSilentEnrollMint() {
		t.Fatal("prod must not allow silent mint")
	}
	dev := Config{AppEnv: "dev"}
	if !dev.AllowSilentEnrollMint() {
		t.Fatal("dev may allow mint for Mailpit/sink")
	}
	empty := Config{}
	if !empty.AllowSilentEnrollMint() {
		t.Fatal("empty APP_ENV (local/test) may allow mint")
	}
}

func TestIsProduction(t *testing.T) {
	if !(Config{AppEnv: "production"}).IsProduction() {
		t.Fatal("production")
	}
	if (Config{AppEnv: "dev"}).IsProduction() {
		t.Fatal("dev is not production")
	}
}
