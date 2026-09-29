package main

import (
	"embed"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	serverURL := flag.String("server", "", "override server URL")
	bindCode := flag.String("bind", "", "bind code (CLI mode)")
	username := flag.String("user", "", "username (CLI bind)")
	password := flag.String("pass", "", "password (CLI bind)")
	nogui := flag.Bool("nogui", false, "headless agent mode (no window)")
	cfgFile := flag.String("config", "remote.json", "config path")
	asService := flag.Bool("service", false, "run as Windows SYSTEM input service")
	lockInput := flag.Bool("lock-input", false, "Winlogon desktop input helper (started by SYSTEM service)")
	installSvc := flag.Bool("install-service", false, "install SYSTEM input service (admin)")
	uninstallSvc := flag.Bool("uninstall-service", false, "uninstall SYSTEM input service (admin)")
	windowKind := flag.String("window", "main", "window kind: main | session")
	sessionID := flag.Uint64("session", 0, "session id (session window)")
	token := flag.String("token", "", "JWT for session window")
	flag.Parse()

	if *installSvc {
		runInstallServiceCLI()
		return
	}
	if *uninstallSvc {
		runUninstallServiceCLI()
		return
	}

	cfgPath = *cfgFile
	if _, err := initLogger(); err != nil {
		log.Printf("init logger failed: %v", err)
	}

	if *lockInput {
		runLockInputHelper()
		return
	}

	if *asService {
		if err := runAsService(); err != nil {
			LogErrorErr("service", err)
			log.Fatal(err)
		}
		return
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		LogErrorErr("load config", err)
		log.Fatal(err)
	}
	if *serverURL != "" {
		cfg.ServerURL = *serverURL
	}
	cfg.AutoStart = IsAutoStartEnabled()
	setConfig(cfg)
	_ = saveConfig(cfgPath, cfg)

	if *windowKind == "session" {
		setSessionWindow(*sessionID, *token, cfg.ServerURL)
	}

	if *bindCode != "" || (*username != "" && *password != "") {
		if _, err := registerDevice(cfg.ServerURL, *bindCode, *username, *password); err != nil {
			LogErrorErr("cli bind", err)
			log.Fatal(err)
		}
		LogInfo("bound successfully via CLI")
	}

	if *nogui {
		runHeadless()
		return
	}

	app := NewApp()
	title := "Remote"
	width, height := 918, 646
	minW, minH := 720, 560
	if isSessionWindow() {
		title = "Remote 远程桌面"
		width, height = 1280, 800
		minW, minH = 800, 500
	}

	windowsOpts := &windows.Options{
		WebviewIsTransparent: false,
		WindowIsTranslucent:  false,
		DisableWindowIcon:    false,
	}
	if isSessionWindow() {
		windowsOpts.WebviewUserDataPath = filepath.Join(os.TempDir(), "remoto-session-"+strconv.FormatUint(sessionWindow.SessionID, 10))
	}

	err = wails.Run(&options.App{
		Title:             title,
		Width:             width,
		Height:            height,
		MinWidth:          minW,
		MinHeight:         minH,
		HideWindowOnClose: !isSessionWindow(),
		AssetServer:       &assetserver.Options{Assets: assets},
		BackgroundColour:  &options.RGBA{R: 15, G: 17, B: 21, A: 255},
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		OnBeforeClose:     app.beforeClose,
		Bind:              []interface{}{app},
		Windows:           windowsOpts,
	})
	if err != nil {
		LogErrorErr("wails run", err)
		log.Fatal(err)
	}
}

func runHeadless() {
	cfg := getConfig()
	if cfg.DeviceToken != "" && cfg.AutoConnect {
		if err := runtimeAgent.Start(); err != nil {
			LogErrorErr("auto connect", err)
		}
	} else if cfg.DeviceToken == "" {
		LogError("not bound; use -bind CODE or -user/-pass, or run without -nogui")
		log.Fatal("not bound; use -bind CODE or -user/-pass, or run without -nogui")
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	runtimeAgent.Stop()
	LogInfo("client exit")
}
