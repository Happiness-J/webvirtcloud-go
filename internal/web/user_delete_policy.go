package web

func userDeleteAllowed(actorID, targetID, role string, lastEnabledAdmin bool) bool {
	if actorID == "" || actorID == targetID {
		return false
	}
	return !(role == "admin" && lastEnabledAdmin)
}
