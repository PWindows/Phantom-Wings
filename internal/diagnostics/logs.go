package diagnostics

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/pwindows/phantom-wings/config"
	"github.com/pwindows/phantom-wings/system"
)

func GenerateDiagnosticsReport(includeEndpoints bool, includeLogs bool, logLines int) (string, error) {
	output := &strings.Builder{}

	fmt.Fprintln(output, "Phantom Wings - Diagnostics Report")
	printHeader(output, "Versions")
	fmt.Fprintln(output, "               Wings:", system.Version)
	appendVersionInfo(output)

	printHeader(output, "Phantom Wings Configuration")
	if err := config.FromFile(config.DefaultLocation); err != nil {
	}
	cfg := config.Get()
	fmt.Fprintln(output, "      Panel Location:", redactField(cfg.PanelLocation, includeEndpoints))
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "  Internal Webserver:", redactField(cfg.Api.Host, includeEndpoints), ":", cfg.Api.Port)
	fmt.Fprintln(output, "         SSL Enabled:", cfg.Api.Ssl.Enabled)
	fmt.Fprintln(output, "     SSL Certificate:", redactField(cfg.Api.Ssl.CertificateFile, includeEndpoints))
	fmt.Fprintln(output, "             SSL Key:", redactField(cfg.Api.Ssl.KeyFile, includeEndpoints))
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "         SFTP Server:", redactField(cfg.System.Sftp.Address, includeEndpoints), ":", cfg.System.Sftp.Port)
	fmt.Fprintln(output, "      SFTP Read-Only:", cfg.System.Sftp.ReadOnly)
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "      Root Directory:", cfg.System.RootDirectory)
	fmt.Fprintln(output, "      Logs Directory:", cfg.System.LogDirectory)
	fmt.Fprintln(output, "      Data Directory:", cfg.System.Data)
	fmt.Fprintln(output, "   Archive Directory:", cfg.System.ArchiveDirectory)
	fmt.Fprintln(output, "    Backup Directory:", cfg.System.BackupDirectory)
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "            Username:", cfg.System.Username)
	fmt.Fprintln(output, "         Server Time:", time.Now().Format(time.RFC1123Z))
	fmt.Fprintln(output, "          Debug Mode:", cfg.Debug)

	appendDockerInfo(output)

	printHeader(output, "Latest Phantom Wings Logs")
	if includeLogs {
		p := path.Join(cfg.System.LogDirectory, "wings.log")
		if c, err := readLastLines(p, logLines); err == nil {
			fmt.Fprintf(output, "%s\n", c)
		} else {
			fmt.Fprintln(output, "No logs found or an error occurred.")
		}
	} else {
		fmt.Fprintln(output, "Logs redacted.")
	}

	return output.String(), nil
}

func redactField(s string, include bool) string {
	if !include {
		return "{redacted}"
	}
	return s
}

func printHeader(output *strings.Builder, title string) {
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "-----", title, "-----")
}
