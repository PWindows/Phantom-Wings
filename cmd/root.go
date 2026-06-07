package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	log2 "log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NYTimes/logrotate"
	"github.com/apex/log"
	"github.com/apex/log/handlers/multi"
	"github.com/gammazero/workerpool"
	"github.com/mitchellh/colorstring"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/pwindows/phantom-wings/config"
	"github.com/pwindows/phantom-wings/environment"
	"github.com/pwindows/phantom-wings/internal/cron"
	"github.com/pwindows/phantom-wings/internal/database"
	"github.com/pwindows/phantom-wings/loggers/cli"
	"github.com/pwindows/phantom-wings/remote"
	"github.com/pwindows/phantom-wings/router"
	"github.com/pwindows/phantom-wings/server"
	"github.com/pwindows/phantom-wings/sftp"
	"github.com/pwindows/phantom-wings/system"
)

var (
	configPath = config.DefaultLocation
	debug      = false
)

var rootCommand = &cobra.Command{
	Use:   "wings",
	Short: "Runs the API server allowing programmatic control of game servers for Phantom Panel.",
	PreRun: func(cmd *cobra.Command, args []string) {
		initConfig()
		initLogging()
		if tls, _ := cmd.Flags().GetBool("auto-tls"); tls {
			if host, _ := cmd.Flags().GetString("tls-hostname"); host == "" {
				fmt.Println("A TLS hostname must be provided when running wings with automatic TLS, e.g.:\n\n    ./wings --auto-tls --tls-hostname my.example.com")
				os.Exit(1)
			}
		}
	},
	Run: rootCmdRun,
}

var versionCommand = &cobra.Command{
	Use:   "version",
	Short: "Prints the current executable version and exits.",
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Printf("wings v%s\nCopyright © 2018 - %d Dane Everitt & Contributors\n", system.Version, time.Now().Year())
	},
}

func Execute() {
	if err := rootCommand.Execute(); err != nil {
		log2.Fatalf("failed to execute command: %s", err)
	}
}

func init() {
	rootCommand.PersistentFlags().StringVar(&configPath, "config", config.DefaultLocation, "set the location for the configuration file")
	rootCommand.PersistentFlags().BoolVar(&debug, "debug", false, "pass in order to run wings in debug mode")

	rootCommand.Flags().Bool("pprof", false, "if the pprof profiler should be enabled. The profiler will bind to localhost:6060 by default")
	rootCommand.Flags().Int("pprof-block-rate", 0, "enables block profile support, may have performance impacts")
	rootCommand.Flags().Int("pprof-port", 6060, "If provided with --pprof, the port it will run on")
	rootCommand.Flags().Bool("auto-tls", false, "pass in order to have wings generate and manage its own SSL certificates using Let's Encrypt")
	rootCommand.Flags().String("tls-hostname", "", "required with --auto-tls, the FQDN for the generated SSL certificate")
	rootCommand.Flags().Bool("ignore-certificate-errors", false, "ignore certificate verification errors when executing API calls")

	rootCommand.AddCommand(versionCommand)
	rootCommand.AddCommand(configureCmd)
	rootCommand.AddCommand(newDiagnosticsCommand())
	rootCommand.AddCommand(newSelfupdateCommand())
}

func rootCmdRun(cmd *cobra.Command, _ []string) {
	printLogo()
	log.Debug("running in debug mode")
	log.WithField("config_file", configPath).Info("loading configuration from file")

	checkDockerSnapAndExit()

	if ok, _ := cmd.Flags().GetBool("ignore-certificate-errors"); ok {
		log.Warn("running with --ignore-certificate-errors: TLS certificate host chains and name will not be verified")
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	if err := config.ConfigureTimezone(); err != nil {
		log.WithField("error", err).Fatal("failed to detect system timezone or use supplied configuration value")
		return
	}
	log.WithField("timezone", config.Get().System.Timezone).Info("configured wings with system timezone")
	if err := config.ConfigureDirectories(); err != nil {
		log.WithField("error", err).Fatal("failed to configure system directories for phantom")
		return
	}
	if err := config.EnsurePhantomUser(); err != nil {
		log.WithField("error", err).Fatal("failed to create phantom system user")
		return
	}
	if err := config.ConfigurePasswd(); err != nil {
		log.WithField("error", err).Fatal("failed to create passwd files for phantom")
	}
	log.WithFields(log.Fields{
		"username": config.Get().System.Username,
		"uid":      config.Get().System.User.Uid,
		"gid":      config.Get().System.User.Gid,
	}).Info("configured system user successfully")
	if err := config.EnableLogRotation(); err != nil {
		log.WithField("error", err).Fatal("failed to configure log rotation on the system")
		return
	}

	t := config.Get().Token
	pclient := remote.New(
		config.Get().PanelLocation,
		remote.WithCredentials(t.ID, t.Token),
		remote.WithCustomHeaders(config.Get().RemoteQuery.CustomHeaders),
		remote.WithHttpClient(&http.Client{
			Timeout: time.Second * time.Duration(config.Get().RemoteQuery.Timeout),
		}),
	)

	if err := database.Initialize(); err != nil {
		log.WithField("error", err).Fatal("failed to initialize database")
		return
	}

	manager, err := server.NewManager(cmd.Context(), pclient)
	if err != nil {
		log.WithField("error", err).Fatal("failed to load server configurations")
		return
	}

	if err := environment.ConfigureEnvironment(cmd.Context()); err != nil {
		log.WithField("error", err).Fatal("failed to configure process environment")
		return
	}

	if err := config.WriteToDisk(config.Get()); err != nil {
		if !errors.Is(err, syscall.EROFS) {
			log.WithField("error", err).Error("failed to write configuration to disk")
		} else {
			log.WithField("error", err).Debug("failed to write configuration to disk")
		}
	}

	for _, s := range manager.All() {
		log.WithField("server", s.ID()).Info("finished loading configuration for server")
	}

	states, err := manager.ReadStates()
	if err != nil {
		log.WithField("error", err).Error("failed to retrieve locally cached server states from disk, assuming all servers in offline state")
	}

	ticker := time.NewTicker(time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := manager.PersistStates(); err != nil {
					log.WithField("error", err).Warn("failed to persist server states to disk")
				}
			case <-cmd.Context().Done():
				ticker.Stop()
				return
			}
		}
	}()

	pool := workerpool.New(4)
	for _, serv := range manager.All() {
		s := serv

		if err := s.EnsureDataDirectoryExists(); err != nil {
			s.Log().Error("could not create root data directory for server: not loading server...")
			continue
		}

		pool.Submit(func() {
			s.Log().Info("configuring server environment and restoring to previous state")
			var st string
			if state, exists := states[s.ID()]; exists {
				st = state
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), time.Second*30)
			defer cancel()

			r, err := s.Environment.IsRunning(ctx)
			if err != nil && !isEnvironmentNotFound(err) {
				s.Log().WithField("error", err).Error("error checking server environment status")
			}

			if !r && (st == environment.ProcessRunningState || st == environment.ProcessStartingState) {
				if err := s.HandlePowerAction(server.PowerActionStart); err != nil {
					s.Log().WithField("error", err).Warn("failed to return server to running state")
				}
			} else if r || (!r && s.IsRunning()) {
				s.Log().Info("detected server is running, re-attaching to process...")
				s.Environment.SetState(environment.ProcessRunningState)
				if err := s.Environment.Attach(ctx); err != nil {
					s.Log().WithField("error", err).Warn("failed to attach to running server environment")
				}
			} else {
				s.Environment.SetState(environment.ProcessOfflineState)
			}

			if state := s.Environment.State(); state == environment.ProcessStartingState || state == environment.ProcessRunningState {
				s.Log().Debug("re-syncing server configuration for already running server")
				if err := s.Sync(); err != nil {
					s.Log().WithError(err).Error("failed to re-sync server configuration")
				}
			}
		})
	}

	pool.StopWait()
	defer func() {
		for _, s := range manager.All() {
			s.CtxCancel()
		}
	}()

	if s, err := cron.Scheduler(cmd.Context(), manager); err != nil {
		log.WithField("error", err).Fatal("failed to initialize cron system")
	} else {
		log.WithField("subsystem", "cron").Info("starting cron processes")
		s.Start()
	}

	go func() {
		if err := sftp.New(manager).Run(); err != nil {
			log.WithError(err).Fatal("failed to initialize the sftp server")
			return
		}
	}()

	go func() {
		log.Info("updating server states on Panel: marking installing/restoring servers as normal")
		if err := pclient.ResetServersState(cmd.Context()); err != nil {
			log.WithField("error", err).Error("failed to reset server states on Panel: some instances may be stuck in an installing/restoring state unexpectedly")
		}
	}()

	sys := config.Get().System
	if err := os.MkdirAll(sys.ArchiveDirectory, 0o755); err != nil {
		log.WithField("error", err).Error("failed to create archive directory")
	}

	if err := os.MkdirAll(sys.BackupDirectory, 0o755); err != nil {
		log.WithField("error", err).Error("failed to create backup directory")
	}

	autotls, _ := cmd.Flags().GetBool("auto-tls")
	tlshostname, _ := cmd.Flags().GetString("tls-hostname")
	if autotls && tlshostname == "" {
		autotls = false
	}

	api := config.Get().Api
	log.WithFields(log.Fields{
		"use_ssl":      api.Ssl.Enabled,
		"use_auto_tls": autotls,
		"host_address": api.Host,
		"host_port":    api.Port,
	}).Info("configuring internal webserver")

	s := &http.Server{
		Addr:      api.Host + ":" + strconv.Itoa(api.Port),
		Handler:   router.Configure(manager, pclient),
		TLSConfig: config.DefaultTLSConfig,
	}

	profile, _ := cmd.Flags().GetBool("pprof")
	if profile {
		if r, _ := cmd.Flags().GetInt("pprof-block-rate"); r > 0 {
			runtime.SetBlockProfileRate(r)
		}
		runtime.SetMutexProfileFraction(100)

		profilePort, _ := cmd.Flags().GetInt("pprof-port")
		go func() {
			http.ListenAndServe(fmt.Sprintf("localhost:%d", profilePort), nil)
		}()
	}

	if autotls {
		m := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(path.Join(sys.RootDirectory, "/.tls-cache")),
			HostPolicy: autocert.HostWhitelist(tlshostname),
		}

		log.WithField("hostname", tlshostname).Info("webserver is now listening with auto-TLS enabled; certificates will be automatically generated by Let's Encrypt")

		s.TLSConfig.GetCertificate = m.GetCertificate
		s.TLSConfig.NextProtos = append(s.TLSConfig.NextProtos, acme.ALPNProto)

		go func() {
			if err := http.ListenAndServe(":http", m.HTTPHandler(nil)); err != nil {
				log.WithError(err).Error("failed to serve autocert http server")
			}
		}()
		if err := s.ListenAndServeTLS("", ""); err != nil {
			log.WithFields(log.Fields{"auto_tls": true, "tls_hostname": tlshostname, "error": err}).Fatal("failed to configure HTTP server using auto-tls")
		}
		return
	}

	if api.Ssl.Enabled {
		if err := s.ListenAndServeTLS(api.Ssl.CertificateFile, api.Ssl.KeyFile); err != nil {
			log.WithFields(log.Fields{"auto_tls": false, "error": err}).Fatal("failed to configure HTTPS server")
		}
		return
	}
	s.TLSConfig = nil
	if err := s.ListenAndServe(); err != nil {
		log.WithField("error", err).Fatal("failed to configure HTTP server")
	}
}

func initConfig() {
	if !filepath.IsAbs(configPath) {
		d, err := filepath.Abs(configPath)
		if err != nil {
			log2.Fatalf("cmd/root: failed to get path to config file: %s", err)
		}
		configPath = d
	}
	err := config.FromFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			exitWithConfigurationNotice()
		}
		log2.Fatalf("cmd/root: error while reading configuration file: %s", err)
	}
	if debug && !config.Get().Debug {
		config.SetDebugViaFlag(debug)
	}
}

func initLogging() {
	dir := config.Get().System.LogDirectory
	if err := os.MkdirAll(path.Join(dir, "/install"), 0o700); err != nil {
		log2.Fatalf("cmd/root: failed to create install directory path: %s", err)
	}
	p := filepath.Join(dir, "/wings.log")
	w, err := logrotate.NewFile(p)
	if err != nil {
		log2.Fatalf("cmd/root: failed to create wings log: %s", err)
	}
	log.SetLevel(log.InfoLevel)
	if config.Get().Debug {
		log.SetLevel(log.DebugLevel)
	}
	log.SetHandler(multi.New(cli.Default, cli.New(w.File, false)))
	log.WithField("path", p).Info("writing log files to disk")
}

func printLogo() {
	fmt.Printf(colorstring.Color(`
                 ____
__ [yellow][bold]Phantom[reset] _____/___/_______ _______ ______
\_____\    \/\/    /   /       /  __   /   ___/
   \___\          /   /   /   /  /_/  /___   /
        \___/\___/___/___/___/___    /______/
                            /_______/ [bold]%s[reset]

© PWindows™ 2026 — Phantom Wings
Website:  https://pwindows.qzz.io
 Source:  https://github.com/pwindows/phantom-wings
License:  https://github.com/pwindows/phantom-wings/blob/main/LICENSE

This software is made available under the terms of the MIT license.
The above copyright notice and this permission notice shall be included
in all copies or substantial portions of the Software.%s`), system.Version, "\n\n")
}

func exitWithConfigurationNotice() {
	fmt.Printf(colorstring.Color(`
[_red_][white][bold]Error: Configuration File Not Found[reset]

Phantom Wings was not able to locate your configuration file, and therefore is not
able to complete its boot process. Please ensure you have copied your instance
configuration file into the default location below.

Default Location: %s

[yellow]This is not a bug with this software. Please do not make a bug report
for this issue, it will be closed.[reset]

`), configNotFoundLocation)
	os.Exit(1)
}