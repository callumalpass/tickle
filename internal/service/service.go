package service

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/callumalpass/tickle/internal/paths"
)

const serviceName = "tickle"

func Install() (string, error) {
	if err := paths.EnsureBaseDirs(); err != nil {
		return "", err
	}
	bin, err := copySelfToStablePath()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		return installSystemd(bin)
	case "darwin":
		return installLaunchd(bin)
	case "windows":
		return installScheduledTask(bin)
	default:
		return "", fmt.Errorf("service install is not supported on %s; use `tickle daemon`", runtime.GOOS)
	}
}

func Start() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return run("systemctl", "--user", "start", "tickle.service")
	case "darwin":
		return run("launchctl", "start", "dev.tickle.daemon")
	case "windows":
		return run("schtasks", "/Run", "/TN", "Tickle")
	default:
		return "", fmt.Errorf("service start is not supported on %s", runtime.GOOS)
	}
}

func Stop() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return run("systemctl", "--user", "stop", "tickle.service")
	case "darwin":
		return run("launchctl", "stop", "dev.tickle.daemon")
	case "windows":
		return run("schtasks", "/End", "/TN", "Tickle")
	default:
		return "", fmt.Errorf("service stop is not supported on %s", runtime.GOOS)
	}
}

func Restart() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return run("systemctl", "--user", "restart", "tickle.service")
	case "darwin":
		stopOut, _ := Stop()
		startOut, err := Start()
		return stopOut + startOut, err
	case "windows":
		stopOut, _ := Stop()
		startOut, err := Start()
		return stopOut + startOut, err
	default:
		return "", fmt.Errorf("service restart is not supported on %s", runtime.GOOS)
	}
}

func Status() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return run("systemctl", "--user", "status", "--no-pager", "tickle.service")
	case "darwin":
		return run("launchctl", "print", "gui/"+fmt.Sprint(os.Getuid())+"/dev.tickle.daemon")
	case "windows":
		return run("schtasks", "/Query", "/TN", "Tickle", "/FO", "LIST", "/V")
	default:
		return "", fmt.Errorf("service status is not supported on %s", runtime.GOOS)
	}
}

func Logs() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return run("journalctl", "--user", "-u", "tickle.service", "-n", "100", "--no-pager")
	case "darwin":
		logs, err := paths.LogsDir()
		if err != nil {
			return "", err
		}
		return run("tail", "-n", "100", filepath.Join(logs, "daemon.out.log"), filepath.Join(logs, "daemon.err.log"))
	case "windows":
		return Status()
	default:
		return "", fmt.Errorf("service logs are not supported on %s", runtime.GOOS)
	}
}

func Uninstall() (string, error) {
	switch runtime.GOOS {
	case "linux":
		var out strings.Builder
		appendCommandOutput(&out, "systemctl", "--user", "disable", "--now", "tickle.service")
		file, err := systemdServicePath()
		if err != nil {
			return out.String(), err
		}
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return out.String(), err
		}
		appendCommandOutput(&out, "systemctl", "--user", "daemon-reload")
		return out.String(), nil
	case "darwin":
		var out strings.Builder
		plist, err := launchAgentPath()
		if err != nil {
			return "", err
		}
		appendCommandOutput(&out, "launchctl", "bootout", "gui/"+fmt.Sprint(os.Getuid()), plist)
		if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
			return out.String(), err
		}
		return out.String(), nil
	case "windows":
		return run("schtasks", "/Delete", "/TN", "Tickle", "/F")
	default:
		return "", fmt.Errorf("service uninstall is not supported on %s", runtime.GOOS)
	}
}

func installSystemd(bin string) (string, error) {
	file, err := systemdServicePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	content := fmt.Sprintf(`[Unit]
Description=Tickle local job daemon

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, bin)
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		return "", err
	}
	var out strings.Builder
	appendCommandOutput(&out, "systemctl", "--user", "daemon-reload")
	appendCommandOutput(&out, "systemctl", "--user", "enable", "tickle.service")
	out.WriteString("installed systemd user service at " + file + "\n")
	return out.String(), nil
}

func installLaunchd(bin string) (string, error) {
	plist, err := launchAgentPath()
	if err != nil {
		return "", err
	}
	logs, err := paths.LogsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return "", err
	}
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>dev.tickle.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, bin, filepath.Join(logs, "daemon.out.log"), filepath.Join(logs, "daemon.err.log"))
	if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
		return "", err
	}
	var out strings.Builder
	appendCommandOutput(&out, "launchctl", "bootstrap", "gui/"+fmt.Sprint(os.Getuid()), plist)
	appendCommandOutput(&out, "launchctl", "enable", "gui/"+fmt.Sprint(os.Getuid())+"/dev.tickle.daemon")
	out.WriteString("installed launchd agent at " + plist + "\n")
	return out.String(), nil
}

func installScheduledTask(bin string) (string, error) {
	task := fmt.Sprintf(`"%s" daemon`, bin)
	out, err := run("schtasks", "/Create", "/F", "/TN", "Tickle", "/SC", "ONLOGON", "/TR", task)
	if err != nil {
		return out, err
	}
	return out + "installed Windows scheduled task Tickle\n", nil
}

func copySelfToStablePath() (string, error) {
	src, err := os.Executable()
	if err != nil {
		return "", err
	}
	dst, err := paths.StableBinaryPath()
	if err != nil {
		return "", err
	}
	if sameFile(src, dst) {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return dst, nil
}

func sameFile(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	return aa == bb
}

func systemdServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", "tickle.service"), nil
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "dev.tickle.daemon.plist"), nil
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func appendCommandOutput(out *strings.Builder, name string, args ...string) {
	text, err := run(name, args...)
	writeCommandOutput(out, text, err)
}

func writeCommandOutput(out *strings.Builder, text string, err error) {
	if text != "" {
		out.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			out.WriteString("\n")
		}
	}
	if err != nil {
		out.WriteString(err.Error())
		out.WriteString("\n")
	}
}
