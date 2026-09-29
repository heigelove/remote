package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	logMaxBytes   = 5 << 20 // 5MB then rotate
	logKeepFiles  = 5
	logTailLines  = 200
	logDirName    = "logs"
	logFilePrefix = "remote"
)

type agentLogger struct {
	mu     sync.Mutex
	dir    string
	file   *os.File
	logger *log.Logger
	recent []string // ring of recent error lines for UI
}

var appLog = &agentLogger{}

func initLogger() (string, error) {
	dir, err := resolveLogDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	appLog.mu.Lock()
	appLog.dir = dir
	if err := appLog.openLocked(); err != nil {
		appLog.mu.Unlock()
		return dir, err
	}
	mw := io.MultiWriter(os.Stdout, appLog.file)
	appLog.logger = log.New(mw, "", log.LstdFlags|log.Lmicroseconds)
	log.SetOutput(mw)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	appLog.mu.Unlock()

	LogInfo("logger started, dir=%s", dir)
	return dir, nil
}

func resolveLogDir() (string, error) {
	// Prefer next to executable so installed agent always finds logs
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), logDirName), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, logDirName), nil
}

func (l *agentLogger) openLocked() error {
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	path := filepath.Join(l.dir, logFilePrefix+".log")
	if fi, err := os.Stat(path); err == nil && fi.Size() >= logMaxBytes {
		_ = rotateLogs(l.dir)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	l.file = f
	return nil
}

func rotateLogs(dir string) error {
	src := filepath.Join(dir, logFilePrefix+".log")
	stamp := time.Now().Format("20060102-150405")
	dst := filepath.Join(dir, fmt.Sprintf("%s-%s.log", logFilePrefix, stamp))
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	// prune old
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var archived []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, logFilePrefix+"-") && strings.HasSuffix(name, ".log") {
			archived = append(archived, filepath.Join(dir, name))
		}
	}
	if len(archived) <= logKeepFiles {
		return nil
	}
	// simple: delete oldest by name (timestamp sorted)
	for i := 0; i < len(archived)-logKeepFiles; i++ {
		_ = os.Remove(archived[i])
	}
	return nil
}

func (l *agentLogger) write(level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s", level, msg)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logger != nil {
		// rotate if needed
		if l.file != nil {
			if fi, err := l.file.Stat(); err == nil && fi.Size() >= logMaxBytes {
				_ = l.openLocked()
				if l.file != nil {
					l.logger.SetOutput(io.MultiWriter(os.Stdout, l.file))
					log.SetOutput(io.MultiWriter(os.Stdout, l.file))
				}
			}
		}
		l.logger.Println(line)
		if l.file != nil {
			_ = l.file.Sync()
		}
	} else {
		log.Println(line)
	}
	if level == "ERROR" || level == "WARN" {
		stamp := time.Now().Format("15:04:05")
		entry := stamp + " " + line
		l.recent = append(l.recent, entry)
		if len(l.recent) > logTailLines {
			l.recent = l.recent[len(l.recent)-logTailLines:]
		}
	}
}

func LogInfo(format string, args ...interface{})  { appLog.write("INFO", format, args...) }
func LogWarn(format string, args ...interface{})  { appLog.write("WARN", format, args...) }
func LogError(format string, args ...interface{}) { appLog.write("ERROR", format, args...) }

func LogErrorErr(context string, err error) {
	if err == nil {
		return
	}
	LogError("%s: %v", context, err)
}

func GetLogDir() string {
	appLog.mu.Lock()
	defer appLog.mu.Unlock()
	return appLog.dir
}

func GetLogFilePath() string {
	dir := GetLogDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, logFilePrefix+".log")
}

func GetRecentLogs(limit int) []string {
	if limit <= 0 {
		limit = 100
	}
	appLog.mu.Lock()
	n := len(appLog.recent)
	if n > 0 {
		if limit > n {
			limit = n
		}
		out := make([]string, limit)
		copy(out, appLog.recent[n-limit:])
		appLog.mu.Unlock()
		return out
	}
	appLog.mu.Unlock()
	return readLogTail(limit)
}

func readLogTail(limit int) []string {
	path := GetLogFilePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var filtered []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.Contains(ln, "[ERROR]") || strings.Contains(ln, "[WARN]") || strings.Contains(ln, "[INFO]") {
			filtered = append(filtered, ln)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered
}

func ClearRecentLogs() {
	appLog.mu.Lock()
	appLog.recent = nil
	appLog.mu.Unlock()
}
