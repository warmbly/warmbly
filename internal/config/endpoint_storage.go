package config

import "github.com/google/uuid"

var (
	StorageEndpointEmailBody = func(userID, emailID, emailMessageID uuid.UUID) string {
		return "users/" + userID.String() + "/emails/" + emailID.String() + "/" + emailMessageID.String() + ".emsg"
	}
)
