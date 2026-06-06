package config

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"emperror.dev/errors"
	"github.com/acobaugh/osrelease"
	"github.com/apex/log"
	"github.com/creasty/defaults"
	"github.com/gbrlsnchs/jwt/v3"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v2"

	"github.com/pwindows/phantom-wings/system"
)

const DefaultLocation = "/etc/phantom/config.yml"

var DefaultTLSConfig = &tls.Config{
	NextProtos: []string{"h2", "http/1.1"},
	CipherSuites: []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	},
	PreferServerCipherSuites: true,
	MinVersion:               tls.VersionTLS12,
	MaxVersion:               tls.VersionTLS13,
	CurvePreferences:         []tls.CurveID{tls.X25519, tls.CurveP256},
}

var (
	mu            sync.RWMutex
	_config       *Configuration
	_jwtAlgo      *jwt.HMACSHA
	_debugViaFlag bool
)

var _writeLock sync.Mutex

type SftpConfiguration struct {
	Address  string `default:"0.0.0.0" json:"bind_address" yaml:"bind_address"`
	Port     int    `default:"2022" json:"bind_port" yaml:"bind_port"`
	ReadOnly bool   `default:"false" yaml:"read_only"`
	KeyOnly  bool   `default:"false" yaml:"key_only"`
}

type ApiConfiguration struct {
	Host string `default:"0.0.0.0" yaml:"host"`
	Port int    `default:"8080" yaml:"port"`
	Ssl  struct {
		Enabled         bool   `json:"enabled" yaml:"enabled"`
		CertificateFile string `json:"cert" yaml:"cert"`
		KeyFile         string `json:"key" yaml:"key"`
	}
	DisableRemoteDownload bool `json:"-" yaml:"disable_remote_download"`
	RemoteDownload        struct {
		MaxRedirects int `default:"10" json:"max_redirects" yaml:"max_redirects"`
	} `json:"remote_download" yaml:"remote_download"`
	UploadLimit    int64    `default:"100" json:"upload_limit" yaml:"upload_limit"`
	TrustedProxies []string `json:"trusted_proxies" yaml:"trusted_proxies"`
}

type RemoteQueryConfiguration struct {
	Timeout            int               `default:"30" yaml:"timeout"`
	BootServersPerPage int               `default:"50" yaml:"boot_servers_per_page"`
	CustomHeaders      map[string]string `yaml:"custom_headers"`
}

type SystemConfiguration struct {
	RootDirectory    string `default:"/var/lib/phantom" json:"-" yaml:"root_directory"`
	LogDirectory     string `default:"/var/log/phantom" json:"-" yaml:"log_directory"`
	Data             string `default:"/var/lib/phantom/volumes" json:"-" yaml:"data"`
	ArchiveDirectory string `default:"/var/lib/phantom/archives" json:"-" yaml:"archive_directory"`
	BackupDirectory  string `default:"/var/lib/phantom/backups" json:"-" yaml:"backup_directory"`
	TmpDirectory     string `default:"/tmp/phantom" json:"-" yaml:"tmp_directory"`
	Username         string `default:"phantom" yaml:"username"`
	Timezone         string `yaml:"timezone"`

	User struct {
		Rootless struct {
			Enabled      bool `yaml:"enabled" default:"false"`
			ContainerUID int  `yaml:"container_uid" default:"0"`
			ContainerGID int  `yaml:"container_gid" default:"0"`
		} `yaml:"rootless"`
		Uid    int `yaml:"uid"`
		Gid    int `yaml:"gid"`
		Passwd struct {
			Enable    bool   `json:"enable" yaml:"enable" default:"true"`
			Directory string `json:"directory" yaml:"directory" default:"/etc/phantom"`
		} `json:"passwd" yaml:"passwd"`
	} `json:"user" yaml:"user"`

	MachineID struct {
		Enable    bool   `json:"enable" yaml:"enable" default:"true"`
		Directory string `json:"directory" yaml:"directory" default:"/etc/phantom/machine-id"`
	} `json:"machine_id" yaml:"machine_id"`

	DiskCheckInterval     int64  `default:"150" yaml:"disk_check_interval"`
	ActivitySendInterval  int    `default:"60" yaml:"activity_send_interval"`
	ActivitySendCount     int    `default:"100" yaml:"activity_send_count"`
	CheckPermissionsOnBoot bool  `default:"true" yaml:"check_permissions_on_boot"`
	EnableLogRotate       bool   `default:"true" yaml:"enable_log_rotate"`
	WebsocketLogCount     int    `default:"150" yaml:"websocket_log_count"`
	Sftp                  SftpConfiguration `yaml:"sftp"`
	CrashDetection        CrashDetection    `yaml:"crash_detection"`
	CrashActivityLogLines int    `default:"2" yaml:"crash_detection_activity_lines"`
	Backups               Backups   `yaml:"backups"`
	Transfers             Transfers `yaml:"transfers"`
	OpenatMode            string `default:"auto" yaml:"openat_mode"`
}

type CrashDetection struct {
	CrashDetectionEnabled  bool `default:"true" yaml:"enabled"`
	DetectCleanExitAsCrash bool `default:"true" yaml:"detect_clean_exit_as_crash"`
	Timeout                int  `default:"60" json:"timeout"`
}

type Backups struct {
	WriteLimit                  int    `default:"0" yaml:"write_limit"`
	CompressionLevel            string `default:"best_speed" yaml:"compression_level"`
	RemoveBackupsOnServerDelete bool   `default:"true" yaml:"remove_backups_on_server_delete"`
}

type Transfers struct {
	DownloadLimit int `default:"0" yaml:"download_limit"`
}

type ConsoleThrottles struct {
	Enabled bool   `json:"enabled" yaml:"enabled" default:"true"`
	Lines   uint64 `json:"lines" yaml:"lines" default:"2000"`
	Period  uint64 `json:"line_reset_interval" yaml:"line_reset_interval" default:"100"`
}

type Token struct {
	ID    string
	Token string
}

type Configuration struct {
	Token                    Token               `json:"-" yaml:"-"`
	path                     string
	Debug                    bool
	AppName                  string              `default:"phantom" json:"app_name" yaml:"app_name"`
	Uuid                     string
	AuthenticationTokenId    string              `json:"token_id" yaml:"token_id"`
	AuthenticationToken      string              `json:"token" yaml:"token"`
	Api                      ApiConfiguration    `json:"api" yaml:"api"`
	System                   SystemConfiguration `json:"system" yaml:"system"`
	Docker                   DockerConfiguration `json:"docker" yaml:"docker"`
	Throttles                ConsoleThrottles
	PanelLocation            string                   `json:"-" yaml:"remote"`
	RemoteQuery              RemoteQueryConfiguration `json:"remote_query" yaml:"remote_query"`
	AllowedMounts            []string                 `json:"-" yaml:"allowed_mounts"`
	SearchRecursion          SearchRecursion          `yaml:"Search"`
	BlockBaseDirMount        bool                     `default:"true" json:"-" yaml:"BlockBaseDirMount"`
	AllowedOrigins           []string                 `json:"allowed_origins" yaml:"allowed_origins"`
	AllowCORSPrivateNetwork  bool                     `json:"allow_cors_private_network" yaml:"allow_cors_private_network"`
	IgnorePanelConfigUpdates bool                     `json:"ignore_panel_config_updates" yaml:"ignore_panel_config_updates"`
}

type SearchRecursion struct {
	BlacklistedDirs   []string `default:"[\"node_modules\", \".git\", \".wine\", \"appcache\", \"depotcache\", \"vendor\"]" yaml:"blacklisted_dirs" json:"blacklisted_dirs"`
	MaxRecursionDepth int      `default:"8" yaml:"max_recursion_depth" json:"max_recursion_depth"`
}

func NewAtPath(path string) (*Configuration, error) {
	var c Configuration
	if err := defaults.Set(&c); err != nil {
		return nil, err
	}
	c.path = path
	return &c, nil
}

func Set(c *Configuration) {
	mu.Lock()
	defer mu.Unlock()
	token := c.Token.Token
	if token == "" {
		c.Token.Token = c.AuthenticationToken
		token = c.Token.Token
	}
	if _config == nil || _config.Token.Token != token {
		_jwtAlgo = jwt.NewHS256([]byte(token))
	}
	_config = c
}

func SetDebugViaFlag(d bool) {
	mu.Lock()
	defer mu.Unlock()
	_config.Debug = d
	_debugViaFlag = d
}

func Get() *Configuration {
	mu.RLock()
	//goland:noinspection GoVetCopyLock
	c := *_config
	mu.RUnlock()
	return &c
}

func Update(callback func(c *Configuration)) {
	mu.Lock()
	defer mu.Unlock()
	callback(_config)
}

func GetJwtAlgorithm() *jwt.HMACSHA {
	mu.RLock()
	defer mu.RUnlock()
	return _jwtAlgo
}

func WriteToDisk(c *Configuration) error {
	_writeLock.Lock()
	defer _writeLock.Unlock()
	//goland:noinspection GoVetCopyLock
	ccopy := *c
	if _debugViaFlag {
		ccopy.Debug = false
	}
	if c.path == "" {
		return errors.New("cannot write configuration, no path defined in struct")
	}
	b, err := yaml.Marshal(&ccopy)
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.path, b, 0o600); err != nil {
		return err
	}
	return nil
}

func EnsurePelicanUser() error {
	sysName, err := getSystemName()
	if err != nil {
		return err
	}

	if sysName == "distroless" {
		_config.System.Username = system.FirstNotEmpty(os.Getenv("WINGS_USERNAME"), "phantom")
		_config.System.User.Uid = system.MustInt(system.FirstNotEmpty(os.Getenv("WINGS_UID"), "988"))
		_config.System.User.Gid = system.MustInt(system.FirstNotEmpty(os.Getenv("WINGS_GID"), "988"))
		return nil
	}

	if _config.System.User.Rootless.Enabled {
		log.Info("rootless mode is enabled, skipping user creation...")
		u, err := user.Current()
		if err != nil {
			return err
		}
		_config.System.Username = u.Username
		_config.System.User.Uid = system.MustInt(u.Uid)
		_config.System.User.Gid = system.MustInt(u.Gid)
		return nil
	}

	log.WithField("username", _config.System.Username).Info("checking for phantom system user")
	u, err := user.Lookup(_config.System.Username)
	if err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return err
		}
	} else {
		_config.System.User.Uid = system.MustInt(u.Uid)
		_config.System.User.Gid = system.MustInt(u.Gid)
		return nil
	}

	command := fmt.Sprintf("useradd --system --no-create-home --shell /usr/sbin/nologin %s", _config.System.Username)
	if strings.HasPrefix(sysName, "alpine") {
		command = fmt.Sprintf("adduser -S -D -H -G %[1]s -s /sbin/nologin %[1]s", _config.System.Username)
		if _, err := exec.Command("addgroup", "-S", _config.System.Username).Output(); err != nil {
			return err
		}
	}

	split := strings.Split(command, " ")
	if _, err := exec.Command(split[0], split[1:]...).Output(); err != nil {
		return err
	}
	u, err = user.Lookup(_config.System.Username)
	if err != nil {
		return err
	}
	_config.System.User.Uid = system.MustInt(u.Uid)
	_config.System.User.Gid = system.MustInt(u.Gid)
	return nil
}

func FromFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c, err := NewAtPath(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return err
	}
	c.Token = Token{
		ID:    os.Getenv("WINGS_TOKEN_ID"),
		Token: os.Getenv("WINGS_TOKEN"),
	}
	if c.Token.ID == "" {
		c.Token.ID = c.AuthenticationTokenId
	}
	if c.Token.Token == "" {
		c.Token.Token = c.AuthenticationToken
	}
	c.Token.ID, err = Expand(c.Token.ID)
	if err != nil {
		return err
	}
	c.Token.Token, err = Expand(c.Token.Token)
	if err != nil {
		return err
	}
	Set(c)
	return nil
}

func ConfigureDirectories() error {
	root := _config.System.RootDirectory
	log.WithField("path", root).Debug("ensuring root data directory exists")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if d, err := filepath.EvalSymlinks(_config.System.Data); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else if d != _config.System.Data {
		_config.System.Data = d
	}
	log.WithField("path", _config.System.Data).Debug("ensuring server data directory exists")
	if err := os.MkdirAll(_config.System.Data, 0o700); err != nil {
		return err
	}
	log.WithField("path", _config.System.TmpDirectory).Debug("ensuring temporary data directory exists")
	if err := os.MkdirAll(_config.System.TmpDirectory, 0o700); err != nil {
		return err
	}
	log.WithField("path", _config.System.ArchiveDirectory).Debug("ensuring archive data directory exists")
	if err := os.MkdirAll(_config.System.ArchiveDirectory, 0o700); err != nil {
		return err
	}
	log.WithField("path", _config.System.BackupDirectory).Debug("ensuring backup data directory exists")
	if err := os.MkdirAll(_config.System.BackupDirectory, 0o700); err != nil {
		return err
	}
	log.WithField("path", _config.System.User.Passwd.Directory).Debug("ensuring passwd directory exists")
	if err := os.MkdirAll(_config.System.User.Passwd.Directory, 0o700); err != nil {
		return err
	}
	log.WithField("path", _config.System.MachineID.Directory).Debug("ensuring machine-id directory exists")
	if err := os.MkdirAll(_config.System.MachineID.Directory, 0o700); err != nil {
		return err
	}
	return nil
}

func ConfigurePasswd() (err error) {
	if !_config.System.User.Passwd.Enable {
		return
	}
	log.WithField("filepath", filepath.Join(_config.System.User.Passwd.Directory, "passwd")).
		Debug("ensuring passwd file exists")
	if err = os.WriteFile(filepath.Join(_config.System.User.Passwd.Directory, "passwd"),
		[]byte(fmt.Sprintf("container:x:%d:%d::/home/container:/usr/sbin/nologin",
			_config.System.User.Uid, _config.System.User.Gid)), 0644); err != nil {
		return fmt.Errorf("could not write passwd file: %w", err)
	}
	log.WithField("filepath", filepath.Join(_config.System.User.Passwd.Directory, "group")).
		Debug("ensuring group file exists")
	if err = os.WriteFile(filepath.Join(_config.System.User.Passwd.Directory, "group"),
		[]byte(fmt.Sprintf("container:x:%d:container",
			_config.System.User.Gid)), 0644); err != nil {
		return fmt.Errorf("could not write group file: %w", err)
	}
	return
}

func EnableLogRotation() error {
	if !_config.System.EnableLogRotate {
		log.Info("skipping log rotate configuration, disabled in wings config file")
		return nil
	}
	if st, err := os.Stat("/etc/logrotate.d"); err != nil && !os.IsNotExist(err) {
		return err
	} else if (err != nil && os.IsNotExist(err)) || !st.IsDir() {
		return nil
	}
	if _, err := os.Stat("/etc/logrotate.d/wings"); err == nil || !os.IsNotExist(err) {
		return err
	}
	log.Info("no log rotation configuration found: adding file now")
	f, err := os.Create("/etc/logrotate.d/wings")
	if err != nil {
		return err
	}
	defer f.Close()
	t, err := template.New("logrotate").Parse(`{{.LogDirectory}}/wings.log {
    size 10M
    compress
    delaycompress
    dateext
    maxage 7
    missingok
    notifempty
    postrotate
        /usr/bin/systemctl kill -s HUP wings.service >/dev/null 2>&1 || true
    endscript
}`)
	if err != nil {
		return err
	}
	return errors.Wrap(t.Execute(f, _config.System), "config: failed to write logrotate to disk")
}

func (sc *SystemConfiguration) GetStatesPath() string {
	return path.Join(sc.RootDirectory, "/states.json")
}

func ConfigureTimezone() error {
	tz := os.Getenv("TZ")
	if _config.System.Timezone == "" && tz != "" {
		_config.System.Timezone = tz
	}
	if _config.System.Timezone == "" {
		b, err := os.ReadFile("/etc/timezone")
		if err != nil {
			if !os.IsNotExist(err) {
				return errors.WithMessage(err, "config: failed to open timezone file")
			}
			_config.System.Timezone = "UTC"
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			out, err := exec.CommandContext(ctx, "timedatectl").Output()
			if err != nil {
				log.WithField("error", err).Warn("failed to execute \"timedatectl\" to determine system timezone, falling back to UTC")
				return nil
			}
			r := regexp.MustCompile(`Time zone: ([\w/]+)`)
			matches := r.FindSubmatch(out)
			if len(matches) != 2 || string(matches[1]) == "" {
				log.Warn("failed to parse timezone from \"timedatectl\" output, falling back to UTC")
				return nil
			}
			_config.System.Timezone = string(matches[1])
		} else {
			_config.System.Timezone = string(b)
		}
	}
	_config.System.Timezone = regexp.MustCompile(`(?i)[^a-z_/]+`).ReplaceAllString(_config.System.Timezone, "")
	_, err := time.LoadLocation(_config.System.Timezone)
	return errors.WithMessage(err, fmt.Sprintf("the supplied timezone %s is invalid", _config.System.Timezone))
}

func getSystemName() (string, error) {
	release, err := osrelease.Read()
	if err != nil {
		return "", err
	}
	return release["ID"], nil
}

var (
	openat2    atomic.Bool
	openat2Set atomic.Bool
)

func UseOpenat2() bool {
	if openat2Set.Load() {
		return openat2.Load()
	}
	defer openat2Set.Store(true)
	c := Get()
	openatMode := c.System.OpenatMode
	switch openatMode {
	case "openat2":
		openat2.Store(true)
		return true
	case "openat":
		openat2.Store(false)
		return false
	default:
		fd, err := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{})
		if err != nil {
			log.WithError(err).Warn("error occurred while checking for openat2 support, falling back to openat")
			openat2.Store(false)
			return false
		}
		_ = unix.Close(fd)
		openat2.Store(true)
		return true
	}
}

func Expand(v string) (string, error) {
	v = os.ExpandEnv(v)
	const filePrefix = "file://"
	if strings.HasPrefix(v, filePrefix) {
		p := v[len(filePrefix):]
		b, err := os.ReadFile(p)
		if err != nil {
			return "", nil
		}
		v = string(bytes.TrimRight(bytes.TrimRight(b, "\r"), "\n"))
	}
	return v, nil
}