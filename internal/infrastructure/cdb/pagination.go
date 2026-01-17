package cdb

import "encoding/base64"

func EncodePageState(state []byte) string {
	return base64.StdEncoding.EncodeToString(state)
}

func DecodePageState(state string) []byte {
	resp, err := base64.StdEncoding.DecodeString(state)
	if err != nil {
		return nil
	}

	return resp
}
