package format

import (
	"fmt"
	"strings"
)

// UserTag returns "tag|uuid" (used for limiter maps and identifiers).
func UserTag(tag string, uuid string) string {
	return fmt.Sprintf("%s|%s", tag, uuid)
}

// UserEmail returns "email: tag|uuid" matching the V2bX format for rich debugging logs.
func UserEmail(tag string, uuid string) string {
	return fmt.Sprintf("email: %s|%s", tag, uuid)
}

// ExtractUUID extracts the raw UUID/token from a user string which may be formatted as
// "email: tag|uuid", "tag|uuid", or plain "uuid".
func ExtractUUID(user string) string {
	user = strings.TrimPrefix(user, "email: ")
	if idx := strings.LastIndex(user, "|"); idx != -1 {
		return user[idx+1:]
	}
	return user
}

// ExtractTagUUID returns "tag|uuid" suitable for limiter lookups from a user string which may
// be "email: tag|uuid", "tag|uuid", or plain "uuid".
func ExtractTagUUID(inboundTag, user string) string {
	user = strings.TrimPrefix(user, "email: ")
	if strings.Contains(user, "|") {
		return user
	}
	return UserTag(inboundTag, user)
}
