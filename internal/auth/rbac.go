package auth

import "strings"

type Permission string

const (
	PermExecutionRead  Permission = "execution:read"
	PermExecutionWrite Permission = "execution:write"
	PermServerRead     Permission = "server:read"
	PermServerWrite    Permission = "server:write"
	PermUserWrite      Permission = "user:write"
	PermAuditRead      Permission = "audit:read"
)

func HasPermission(role string, perm Permission) bool {
	switch strings.ToLower(role) {
	case "admin":
		return true
	case "operator":
		switch perm {
		case PermExecutionRead, PermExecutionWrite, PermServerRead, PermServerWrite:
			return true
		default:
			return false
		}
	case "viewer":
		switch perm {
		case PermExecutionRead, PermServerRead:
			return true
		default:
			return false
		}
	default:
		return false
	}
}
