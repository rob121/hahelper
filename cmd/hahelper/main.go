package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	haplog "github.com/brutella/hap/log"
	"github.com/rob121/hahelper/internal/bridge"
	"github.com/rob121/hahelper/internal/config"
	"github.com/rob121/hahelper/internal/exposure"
	"github.com/rob121/hahelper/internal/haclient"
	"github.com/rob121/hahelper/internal/homekit"
	"github.com/rob121/hahelper/internal/ui"
	"github.com/rob121/vhelp"
)

func main() {
	log.SetFlags(log.LstdFlags)

	vhelp.Load("config")
	v, err := vhelp.Get("config")
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	cfgPath := v.ConfigFileUsed()
	if err := config.MigrateLegacyFile(cfgPath); err != nil {
		log.Printf("config migrate: %v", err)
	}

	cfg, err := config.Parse(v)
	if err != nil {
		log.Fatalf("parse config: %v", err)
	}
	log.Printf("config loaded from %s", cfgPath)

	if !cfg.HAP.VerboseLog {
		haplog.Info.Disable()
	}

	exp, err := exposure.Load(cfg.HAP.StorePath)
	if err != nil {
		log.Fatalf("exposure registry: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		cfgMu  sync.RWMutex
		cfgPtr = cfg
		haSvc  *haclient.Service
	)

	brMgr := bridge.NewManager(&cfgPtr, nil, exp)
	brMgr.Start(ctx)

	connectHA := func() {
		cfgMu.RLock()
		c := cfgPtr
		cfgMu.RUnlock()
		if !c.HAConfigured() {
			brMgr.SetHA(nil)
			return
		}
		if haSvc != nil {
			haSvc.Close()
		}
		svc, err := haclient.New(c.HomeAssistant.URL, c.HomeAssistant.Token, c.HomeAssistant.WSReconnect)
		if err != nil {
			log.Printf("home assistant client: %v", err)
			brMgr.SetHA(nil)
			return
		}
		connCtx, connCancel := context.WithTimeout(ctx, 30*time.Second)
		if err := svc.Connect(connCtx); err != nil {
			log.Printf("home assistant websocket: %v", err)
		} else {
			log.Printf("home assistant connected to %s", c.HomeAssistant.URL)
		}
		connCancel()
		haSvc = svc
		brMgr.SetHA(haSvc)
	}

	onHAChange := func() {
		c, err := config.LoadFile(cfgPath)
		if err != nil {
			log.Printf("reload config from %s: %v", cfgPath, err)
			connectHA()
			return
		}
		cfgMu.Lock()
		cfgPtr = c
		cfgMu.Unlock()
		log.Printf("home assistant config reloaded from %s", cfgPath)
		connectHA()
	}

	uiSrv := ui.New(cfgPath, &cfgPtr, &haSvc, exp, &brMgr, onHAChange)
	admin := &http.Server{
		Addr:    cfg.UI.Listen,
		Handler: uiSrv.Handler(),
	}
	go func() {
		log.Printf("admin UI http://%s (LAN — trusted network)", cfg.UI.Listen)
		if err := admin.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("admin server: %v", err)
		}
	}()

	connectHA()

	pin := cfg.EffectivePin()
	if uri, err := homekit.SetupURI(pin, homekit.CategoryBridge, cfg.Bridge.SetupID); err == nil {
		log.Printf("HomeKit bridge %q — PIN %s — setup URI %s", cfg.Bridge.Name, homekit.FormatPIN(pin), uri)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = admin.Shutdown(shutdownCtx)
	if haSvc != nil {
		haSvc.Close()
	}
}
