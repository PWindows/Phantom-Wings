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