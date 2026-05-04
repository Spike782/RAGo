package user

import (
	"ai-chat/common/code"
	myredis "ai-chat/common/redis"
	"strings"
)

func GetOnlineStatus(email string) (bool, int64, code.Code) {
	email = normalizeEmail(email)
	if strings.TrimSpace(email) == "" {
		return false, 0, code.CodeInvalidParams
	}

	online, lastSeen, err := myredis.GetUserOnlineStatus(email)
	if err != nil {
		return false, 0, code.CodeServerBusy
	}
	return online, lastSeen, code.CodeSuccess
}
