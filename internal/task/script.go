package task

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/rana-remote/rana-remote/internal/config"
)

type scriptData struct {
	TempDir      string
	Paths        string
	RcloneRemote string
	RcloneFlags  string
}

var backupScriptTmpl = template.Must(template.New("backup-script").Parse(`#!/bin/bash
set -euo pipefail

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
HOSTNAME=$(hostname)
BACKUP_NAME="${HOSTNAME}_${TIMESTAMP}.tar.gz"
TEMP_DIR={{.TempDir}}
BACKUP_PATH="${TEMP_DIR}/${BACKUP_NAME}"

cleanup() {
  rm -f "${BACKUP_PATH:-}"
}
trap cleanup EXIT INT TERM

echo "[Preflight] Checking dependencies..."
command -v tar >/dev/null 2>&1 || { echo "tar not found"; exit 10; }
command -v rclone >/dev/null 2>&1 || { echo "rclone not found"; exit 11; }

mkdir -p "${TEMP_DIR}"
touch "${TEMP_DIR}/.rana-write-test" && rm -f "${TEMP_DIR}/.rana-write-test"

EST_KB=$(du -sk {{.Paths}} | awk '{sum+=$1} END{print sum}')
FREE_KB=$(df -Pk "${TEMP_DIR}" | awk 'NR==2 {print $4}')
if [ "${FREE_KB}" -lt "${EST_KB}" ]; then
  echo "insufficient temp disk space: need=${EST_KB}KB free=${FREE_KB}KB"
  exit 12
fi

echo "[Step 1/3] Creating archive..."
tar -czf "${BACKUP_PATH}" {{.Paths}}
echo "Archive size: $(du -h "${BACKUP_PATH}" | cut -f1)"

echo "[Step 2/3] Uploading via rclone..."
rclone copy "${BACKUP_PATH}" {{.RcloneRemote}}{{if .RcloneFlags}} {{.RcloneFlags}}{{end}} --progress

echo "[Step 3/3] Cleaning up..."
rm -f "${BACKUP_PATH}"

echo "Backup completed successfully: ${BACKUP_NAME}"
`))

// GenerateScript renders bash script with shell-escaped args.
func GenerateScript(tempDir string, srv config.Server) (string, error) {
	if strings.TrimSpace(tempDir) == "" {
		return "", fmt.Errorf("temp dir is required")
	}
	if len(srv.Paths) == 0 {
		return "", fmt.Errorf("server paths are required")
	}
	if strings.TrimSpace(srv.Rclone.Remote) == "" {
		return "", fmt.Errorf("rclone remote is required")
	}
	d := scriptData{
		TempDir:      shellQuote(tempDir),
		Paths:        quoteSlice(srv.Paths),
		RcloneRemote: shellQuote(srv.Rclone.Remote),
		RcloneFlags:  quoteSlice(srv.Rclone.Flags),
	}
	if strings.TrimSpace(d.Paths) == "" {
		return "", fmt.Errorf("rendered paths are empty")
	}

	var b bytes.Buffer
	if err := backupScriptTmpl.Execute(&b, d); err != nil {
		return "", fmt.Errorf("execute script template: %w", err)
	}
	return b.String(), nil
}

func quoteSlice(values []string) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, shellQuote(v))
	}
	return strings.Join(out, " ")
}

func shellQuote(v string) string {
	if v == "" {
		return "''"
	}
	if !strings.ContainsAny(v, " \t\n\r'\"$`\\!&|;()<>{}[]*?") {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'"
}
