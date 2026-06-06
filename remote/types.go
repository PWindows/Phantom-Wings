package remote

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/apex/log"
	"github.com/goccy/go-json"

	"github.com/pwindows/phantom-wings/parser"
)

const (
	SftpAuthPassword  = SftpAuthRequestType("password")
	SftpAuthPublicKey = SftpAuthRequestType("public_key")
)

type d map[string]interface{}
type q map[string]string
type ClientOption func(c *client)

type Pagination struct {
	CurrentPage uint `json:"current_page"`
	From        uint `json:"from"`
	LastPage    uint `json:"last_page"`
	PerPage     uint `json:"per_page"`
	To          uint `json:"to"`
	Total       uint `json:"total"`
}

type ServerConfigurationResponse struct {
	Settings             json.RawMessage       `json:"settings"`
	ProcessConfiguration *ProcessConfiguration `json:"process_configuration"`
}

type InstallationScript struct {
	ContainerImage string `json:"container_image"`
	Entrypoint     string `json:"entrypoint"`
	Script         string `json:"script"`
}

type RawServerData struct {
	Uuid                 string          `json:"uuid"`
	Settings             json.RawMessage `json:"settings"`
	ProcessConfiguration json.RawMessage `json:"process_configuration"`
}

type SftpAuthRequestType string

type SftpAuthRequest struct {
	Type          SftpAuthRequestType `json:"type"`
	User          string              `json:"username"`
	Pass          string              `json:"password"`
	IP            string              `json:"ip"`
	SessionID     []byte              `json:"session_id"`
	ClientVersion []byte              `json:"client_version"`
}

type SftpAuthResponse struct {
	Server      string   `json:"server"`
	User        string   `json:"user"`
	Permissions []string `json:"permissions"`
}

type OutputLineMatcher struct {
	raw []byte
	reg *regexp.Regexp
}

func (olm *OutputLineMatcher) Matches(s []byte) bool {
	if olm.reg == nil {
		return bytes.Contains(s, olm.raw)
	}
	return olm.reg.Match(s)
}

func (olm *OutputLineMatcher) String() string {
	return string(olm.raw)
}

func (olm *OutputLineMatcher) UnmarshalJSON(data []byte) error {
	var r string
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}

	olm.raw = []byte(r)
	if bytes.HasPrefix(olm.raw, []byte("regex:")) && len(olm.raw) > 6 {
		r, err := regexp.Compile(strings.TrimPrefix(string(olm.raw), "regex:"))
		if err != nil {
			log.WithField("error", err).WithField("raw", string(olm.raw)).Warn("failed to compile output line marked as being regex")
		}
		olm.reg = r
	}

	return nil
}

type ProcessStopConfiguration struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type ProcessConfiguration struct {
	Startup struct {
		Done            []*OutputLineMatcher `json:"done"`
		UserInteraction []string             `json:"user_interaction"`
		StripAnsi       bool                 `json:"strip_ansi"`
	} `json:"startup"`
	Stop               ProcessStopConfiguration   `json:"stop"`
	ConfigurationFiles []parser.ConfigurationFile `json:"configs"`
}

type BackupRemoteUploadResponse struct {
	Parts    []string `json:"parts"`
	PartSize int64    `json:"part_size"`
}

type BackupPart struct {
	ETag       string `json:"etag"`
	PartNumber int    `json:"part_number"`
}

type BackupRequest struct {
	Checksum     string       `json:"checksum"`
	ChecksumType string       `json:"checksum_type"`
	Size         int64        `json:"size"`
	Successful   bool         `json:"successful"`
	Parts        []BackupPart `json:"parts"`
}

type InstallStatusRequest struct {
	Successful bool `json:"successful"`
	Reinstall  bool `json:"reinstall"`
}

type ServerStateChange struct {
	PrevState string `json:"previous_state"`
	NewState  string `json:"new_state"`
}