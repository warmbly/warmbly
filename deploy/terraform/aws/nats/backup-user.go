//go:build ignore

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: go run backup-user.go /private/authority-directory /private/new-backup.json")
	}
	seed, err := os.ReadFile(filepath.Join(os.Args[1], "account.seed"))
	if err != nil {
		panic("read account signing key")
	}
	account, err := nkeys.FromSeed(seed)
	if err != nil {
		panic("decode account signing key")
	}
	defer account.Wipe()
	user, err := nkeys.CreateUser()
	if err != nil {
		panic(err)
	}
	defer user.Wipe()
	public, err := user.PublicKey()
	if err != nil {
		panic(err)
	}
	claim := jwt.NewUserClaims(public)
	claim.Name = "warmbly-stream-backup"
	claim.Pub.Allow.Add("$JS.API.INFO", "$JS.API.STREAM.INFO.warmbly", "$JS.API.STREAM.SNAPSHOT.warmbly", "$JS.SNAPSHOT.ACK.warmbly.>")
	claim.Sub.Allow.Add("_INBOX.>")
	token, err := claim.Encode(account)
	if err != nil {
		panic(err)
	}
	userSeed, err := user.Seed()
	if err != nil {
		panic(err)
	}
	creds, err := jwt.FormatUserConfig(token, userSeed)
	if err != nil {
		panic(err)
	}
	data, err := json.Marshal(map[string]string{"NATS_CREDS_B64": base64.StdEncoding.EncodeToString(creds)})
	if err != nil {
		panic(err)
	}
	f, err := os.OpenFile(os.Args[2], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		panic("create new backup credential file")
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		panic("write backup credential file")
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	fmt.Println("Created snapshot-only credential; no secret values printed")
}
