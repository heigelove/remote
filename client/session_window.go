package main

type sessionWindowState struct {
	SessionID uint64
	Token     string
	ServerURL string
}

var sessionWindow *sessionWindowState

func setSessionWindow(id uint64, token, server string) {
	sessionWindow = &sessionWindowState{
		SessionID: id,
		Token:     token,
		ServerURL: server,
	}
}

func isSessionWindow() bool {
	return sessionWindow != nil && sessionWindow.SessionID != 0
}
