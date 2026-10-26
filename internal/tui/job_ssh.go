package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/jobconn"
)

// Step indexes of the job wizard for server engines (mysql/postgres/mongodb).
const (
	stepSSH      = 2
	stepDatabase = 3
)

const connStepDescription = "Connection parameters for the database server. " +
	"With an SSH tunnel (next step), the host is as seen from the SSH server."

func sshStep(vals map[string]string) WizardStep {
	port := vals["ssh_port"]
	if port == "" {
		port = "22"
	}
	return WizardStep{
		Title: "SSH Tunnel (optional)",
		Description: "Reach a database that only listens on its server or sits behind a firewall. " +
			"Leave SSH Host blank to connect directly.",
		Fields: []WizardField{
			{Key: "ssh_host", Label: "SSH Host", Placeholder: "blank = connect directly", Value: vals["ssh_host"], Type: FieldText},
			{Key: "ssh_port", Label: "SSH Port", Placeholder: "22", Value: port, Type: FieldText},
			{Key: "ssh_user", Label: "SSH User", Placeholder: "deploy", Value: vals["ssh_user"], Type: FieldText},
			{Key: "ssh_key", Label: "Private Key File (copied into vault)", Placeholder: keyPlaceholder(vals), Value: vals["ssh_key"], Type: FieldText},
			{Key: "ssh_password", Label: "SSH Password / Key Passphrase (Vault)", Placeholder: "optional", Value: vals["ssh_password"], Type: FieldText, IsSecret: true},
		},
	}
}

func keyPlaceholder(vals map[string]string) string {
	if vals["ssh_key_in_vault"] == "true" {
		return "stored in vault (blank = keep it)"
	}
	return "~/.ssh/id_ed25519 (blank = password or ssh-agent)"
}

func validateSSHValues(vals map[string]string) error {
	if strings.TrimSpace(vals["ssh_host"]) == "" {
		return nil
	}
	if strings.TrimSpace(vals["ssh_user"]) == "" {
		return errors.New("SSH user is required when SSH host is set")
	}
	if p := strings.TrimSpace(vals["ssh_port"]); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n <= 0 || n > 65535 {
			return fmt.Errorf("SSH port %q is not a valid port", p)
		}
	}
	if k := strings.TrimSpace(vals["ssh_key"]); k != "" {
		if _, err := os.Stat(config.ExpandPath(k)); err != nil {
			return fmt.Errorf("private key file: %v", err)
		}
	}
	return nil
}

// sshConfigFromValues returns the SSH block for the wizard values, or nil when
// SSH host is blank. prev keeps the pinned host key and password ref when the
// server is unchanged.
func sshConfigFromValues(vals map[string]string, prev *config.SSH) *config.SSH {
	host := strings.TrimSpace(vals["ssh_host"])
	if host == "" {
		return nil
	}
	port, _ := strconv.Atoi(strings.TrimSpace(vals["ssh_port"]))
	if port <= 0 {
		port = 22
	}
	c := &config.SSH{
		Host:    host,
		Port:    port,
		User:    strings.TrimSpace(vals["ssh_user"]),
		KeyPath: strings.TrimSpace(vals["ssh_key"]),
	}
	if prev != nil && prev.Host == c.Host && sshPort(prev) == c.Port {
		c.HostKey = prev.HostKey
		c.PasswordRef = prev.PasswordRef
	}
	return c
}

func sshPort(c *config.SSH) int {
	if c.Port <= 0 {
		return 22
	}
	return c.Port
}

// wizardTunnel builds the SSH tunnel from the wizard values, pinning the
// server's host key on first use. Returns nil when SSH host is blank.
func (s *JobsScreen) wizardTunnel(ctx context.Context, vals map[string]string) (*db.SSHTunnel, error) {
	var prev *config.SSH
	name := s.editingJobName
	if s.isEditing {
		if j, ok := s.svc.Cfg.Job(name); ok {
			prev = j.Conn.SSH
		}
	}
	sc := sshConfigFromValues(vals, prev)
	if sc == nil {
		return nil, nil
	}
	t, err := s.svc.ResolveJobSSH(config.Job{Name: name, Conn: config.Conn{SSH: sc}})
	if err != nil {
		return nil, err
	}
	if pw := vals["ssh_password"]; pw != "" {
		t.Password = pw
	}
	if t.HostKey == "" {
		fp, err := db.FetchSSHHostKey(ctx, *t)
		if err != nil {
			return nil, err
		}
		t.HostKey = fp
	}
	return t, nil
}

// saveJobSSH stores the SSH password in the vault and pins the server's host
// key on first use. It returns a note for the status line ("" if none).
func (s *JobsScreen) saveJobSSH(job *config.Job, oldName string, vals map[string]string) string {
	v := s.svc.Vault
	if job.Conn.SSH == nil {
		if v != nil {
			v.Delete(jobconn.SSHPasswordVaultKey(job.Name))
			v.Delete(jobconn.SSHKeyVaultKey(job.Name))
			_ = v.Save()
		}
		return ""
	}
	if v == nil {
		_ = s.svc.CreateVault()
		v = s.svc.Vault
	}
	var notes []string
	if v != nil {
		if oldName != "" && oldName != job.Name {
			if k, ok := v.Get(jobconn.SSHKeyVaultKey(oldName)); ok {
				v.Set(jobconn.SSHKeyVaultKey(job.Name), k)
				v.Delete(jobconn.SSHKeyVaultKey(oldName))
			}
			v.Delete(jobconn.SSHPasswordVaultKey(oldName))
		}
		if pw := vals["ssh_password"]; pw != "" {
			v.Set(jobconn.SSHPasswordVaultKey(job.Name), pw)
		}
		// Import the key file into the vault so scheduled runs only need
		// dbk.db, and the key travels with settings backups.
		if kp := job.Conn.SSH.KeyPath; kp != "" {
			if b, err := os.ReadFile(config.ExpandPath(kp)); err == nil {
				v.Set(jobconn.SSHKeyVaultKey(job.Name), strings.TrimRight(string(b), "\r\n"))
				job.Conn.SSH.KeyPath = ""
				notes = append(notes, "SSH key stored in the vault")
			} else {
				notes = append(notes, "could not import SSH key ("+err.Error()+"), using the file")
			}
		}
		_ = v.Save()
	}

	if job.Conn.SSH.HostKey == "" {
		t, err := s.svc.ResolveJobSSH(*job)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			var fp string
			fp, err = db.FetchSSHHostKey(ctx, *t)
			cancel()
			if err == nil {
				job.Conn.SSH.HostKey = fp
				notes = append(notes, "pinned SSH host key "+fp)
			}
		}
		if err != nil {
			notes = append(notes, "could not read SSH host key yet ("+err.Error()+")")
		}
	}
	hasVaultKey := false
	if v != nil {
		_, hasVaultKey = v.Get(jobconn.SSHKeyVaultKey(job.Name))
	}
	if job.Conn.SSH.KeyPath == "" && !hasVaultKey && vals["ssh_password"] == "" && job.Conn.SSH.PasswordRef == "" {
		notes = append(notes, "SSH uses ssh-agent, which scheduled runs don't have; set a key file for schedules")
	}
	return strings.Join(notes, "; ")
}

// toolWarning explains which client tools engine is missing here, or "".
func (s *JobsScreen) toolWarning(engine string) string {
	if err := s.svc.ToolsMissing(engine); err != nil {
		return err.Error() + ". You can still save the job; backups fail until they are installed."
	}
	return ""
}

// toolSummary shows which engines' client tools are installed here.
func (s *JobsScreen) toolSummary(engines []string) string {
	parts := make([]string, 0, len(engines))
	for _, e := range engines {
		mark := "✔"
		if s.svc.ToolsMissing(e) != nil {
			mark = "✖ tools missing"
		}
		parts = append(parts, e+" "+mark)
	}
	return "Client tools on this computer: " + strings.Join(parts, " · ")
}
