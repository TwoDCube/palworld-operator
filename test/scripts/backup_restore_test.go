/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// backup.sh and restore.sh reach S3 exclusively through rclone, and how they
// configure it is product behaviour: rclone's default bucket probe issues a
// CreateBucket that a per-bucket S3 identity rejects, which silently turned every
// S3 backup into a failure (spec 07). These tests stub the rclone binary and
// assert on the configuration the scripts hand it.

const (
	backupScript  = "../../build/palworld-server/backup.sh"
	restoreScript = "../../build/palworld-server/restore.sh"
)

// rcloneStub puts a fake rclone first on PATH. It appends its argv and every
// RCLONE_CONFIG_* variable it was given to a log file, so a test can assert on
// what the script actually exported rather than on the script's source text.
type rcloneStub struct {
	dir     string // prepended to PATH
	logPath string
	payload string // file streamed for "rclone cat" (restore path)
}

func newRcloneStub(t *testing.T) *rcloneStub {
	t.Helper()
	dir := t.TempDir()
	s := &rcloneStub{
		dir:     dir,
		logPath: filepath.Join(dir, "rclone.log"),
	}
	stub := `#!/usr/bin/env bash
{
  echo "ARGS: $*"
  env | grep '^RCLONE_CONFIG_' | sort
} >> "${STUB_LOG}"
case "$1" in
  rcat) cat > /dev/null ;;                      # swallow the streamed tar
  size) echo '{"bytes":42}' ;;
  copyto) : ;;
  cat)  cat "${STUB_PAYLOAD}" ;;                # stream the archive to restore
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "rclone"), []byte(stub), 0o755); err != nil {
		t.Fatalf("writing rclone stub: %v", err)
	}
	return s
}

// run executes one of the scripts with the stub first on PATH.
func (s *rcloneStub) run(t *testing.T, script string, args []string, env map[string]string) string {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+s.dir+":"+os.Getenv("PATH"),
		"STUB_LOG="+s.logPath,
		"STUB_PAYLOAD="+s.payload,
	)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", script, err, out)
	}
	return string(out)
}

func (s *rcloneStub) log(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(s.logPath)
	if err != nil {
		t.Fatalf("reading stub log (was rclone invoked?): %v", err)
	}
	return string(b)
}

// s3Env is the minimal S3 configuration both scripts require.
func s3ScriptEnv(dataDir string) map[string]string {
	return map[string]string{
		"DATA_DIR":              dataDir,
		"S3_BUCKET":             "backups",
		"S3_KEY":                "world.tar.gz",
		"AWS_ACCESS_KEY_ID":     "id",
		"AWS_SECRET_ACCESS_KEY": "secret",
	}
}

// The bucket always pre-exists here, so rclone must not probe for it: an ODF
// ObjectBucketClaim user carries a 1-bucket quota and answers the implied
// CreateBucket with TooManyBuckets, failing an upload into a writable bucket.
func TestBackupS3DisablesRcloneBucketCheck(t *testing.T) {
	stub := newRcloneStub(t)
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "Level.sav"), []byte("world"), 0o644); err != nil {
		t.Fatalf("seeding data dir: %v", err)
	}

	stub.run(t, backupScript, []string{"s3"}, s3ScriptEnv(dataDir))

	logged := stub.log(t)
	if !strings.Contains(logged, "RCLONE_CONFIG_S3_NO_CHECK_BUCKET=true") {
		t.Errorf("backup.sh did not disable the rclone bucket check; rclone saw:\n%s", logged)
	}
	if !strings.Contains(logged, "ARGS: rcat s3:backups/world.tar.gz") {
		t.Errorf("unexpected rclone invocation:\n%s", logged)
	}
}

func TestRestoreS3DisablesRcloneBucketCheck(t *testing.T) {
	stub := newRcloneStub(t)
	stub.payload = writeTarGz(t, map[string]string{"Level.sav": "restored"})
	dataDir := filepath.Join(t.TempDir(), "Saved")

	stub.run(t, restoreScript, []string{"s3"}, s3ScriptEnv(dataDir))

	logged := stub.log(t)
	if !strings.Contains(logged, "RCLONE_CONFIG_S3_NO_CHECK_BUCKET=true") {
		t.Errorf("restore.sh did not disable the rclone bucket check; rclone saw:\n%s", logged)
	}

	// Also confirm the stubbed stream really landed in DATA_DIR, so the assertion
	// above is about a restore that worked rather than one that no-oped.
	got, err := os.ReadFile(filepath.Join(dataDir, "Level.sav"))
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if string(got) != "restored" {
		t.Errorf("restored content = %q, want %q", got, "restored")
	}
}

// writeTarGz builds a gzipped tar of name->content and returns its path, matching
// the shape backup.sh produces (entries relative to the save dir).
func writeTarGz(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: "./" + name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	path := filepath.Join(t.TempDir(), "payload.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing payload: %v", err)
	}
	return path
}
