package main

const clipMaxBytes = 256 * 1024

func clipTrim(s string) string {
	if len(s) <= clipMaxBytes {
		return s
	}
	return s[:clipMaxBytes]
}
