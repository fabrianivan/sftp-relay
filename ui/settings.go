package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/secrets"
	"github.com/fabrianivan/sftp-relay/internal/setup"
)

type endpointForm struct {
	role                   string
	host, port, user, pass *widget.Entry
	key, dir, fingerprint  *widget.Entry
	passHint               *widget.Label
	card                   *widget.Card
}

type settingsView struct {
	w       *Window
	content fyne.CanvasObject

	src, dst *endpointForm

	backup, include, exclude, stable, parallel     *widget.Entry
	verify                                         *widget.Check
	poll, retries, reconnect, keepAlive, idleScans *widget.Entry
	strict, initial, notify, keychain              *widget.Check
	logDays                                        *widget.Entry
	schedOn                                        *widget.Check
	schedStart, schedEnd, schedTZ                  *widget.Entry
}

func newSettingsView(w *Window) *settingsView {
	s := &settingsView{w: w}
	s.src = s.endpointForm(setup.Source, "Source server", "The folder that is watched for new files.")
	s.dst = s.endpointForm(setup.Target, "Target server", "Where files are delivered. Leave the host empty to only archive files.")

	s.backup = widget.NewEntry()
	s.include = widget.NewEntry()
	s.include.SetPlaceHolder("* (everything)")
	s.exclude = widget.NewEntry()
	s.stable = widget.NewEntry()
	s.parallel = widget.NewEntry()
	s.verify = widget.NewCheck("Verify SHA-256 after upload", nil)
	rules := widget.NewCard("Transfer rules", "", widget.NewForm(
		widget.NewFormItem("Archive folder (source)", s.backup),
		widget.NewFormItem("Include patterns", s.include),
		widget.NewFormItem("Exclude patterns", s.exclude),
		widget.NewFormItem("Wait until unchanged (s)", s.stable),
		widget.NewFormItem("Parallel transfers", s.parallel),
		widget.NewFormItem("", s.verify),
	))

	s.schedOn = widget.NewCheck("Only scan automatically during these hours", nil)
	s.schedStart, s.schedEnd, s.schedTZ = widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	s.schedStart.SetPlaceHolder("06:00")
	s.schedEnd.SetPlaceHolder("22:00 (earlier than start = overnight)")
	s.schedTZ.SetPlaceHolder("Local or e.g. Asia/Jakarta")
	sched := widget.NewCard("Active hours", "", widget.NewForm(
		widget.NewFormItem("", s.schedOn),
		widget.NewFormItem("Start (HH:MM)", s.schedStart),
		widget.NewFormItem("End (HH:MM)", s.schedEnd),
		widget.NewFormItem("Time zone", s.schedTZ),
	))

	s.poll, s.retries, s.reconnect, s.keepAlive, s.idleScans, s.logDays =
		widget.NewEntry(), widget.NewEntry(), widget.NewEntry(), widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	s.strict = widget.NewCheck("Strict host key checking (never trust new hosts automatically)", nil)
	s.initial = widget.NewCheck("Scan immediately when started", nil)
	s.notify = widget.NewCheck("Desktop notifications", nil)
	s.keychain = widget.NewCheck("Store passwords in the system keychain (recommended)", nil)
	app := widget.NewCard("Connection & app", "", widget.NewForm(
		widget.NewFormItem("Scan every (s)", s.poll),
		widget.NewFormItem("Retries", s.retries),
		widget.NewFormItem("Retry delay (s)", s.reconnect),
		widget.NewFormItem("Keep-alive (s)", s.keepAlive),
		widget.NewFormItem("Close idle sessions after N empty scans", s.idleScans),
		widget.NewFormItem("Keep logs (days)", s.logDays),
		widget.NewFormItem("", s.keychain),
		widget.NewFormItem("", s.strict),
		widget.NewFormItem("", s.initial),
		widget.NewFormItem("", s.notify),
	))

	save := widget.NewButtonWithIcon("Save & apply", theme.DocumentSaveIcon(), s.save)
	save.Importance = widget.HighImportance
	revert := widget.NewButtonWithIcon("Revert", theme.ContentUndoIcon(), s.load)
	path := widget.NewLabel("Config file: " + w.ctl.Path)
	path.Importance = widget.LowImportance
	path.Truncation = fyne.TextTruncateEllipsis

	grid := container.NewVBox(s.src.card, s.dst.card, rules, sched, app)
	footer := container.NewBorder(nil, nil, nil, container.NewHBox(revert, save), path)
	s.content = container.NewBorder(nil, container.NewPadded(footer), nil, nil, container.NewVScroll(container.NewPadded(grid)))
	s.load()
	return s
}

func (s *settingsView) endpointForm(role, title, subtitle string) *endpointForm {
	f := &endpointForm{role: role}
	f.host, f.port, f.user = widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	f.pass = widget.NewPasswordEntry()
	f.key, f.dir, f.fingerprint = widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	f.key.SetPlaceHolder("optional, e.g. ~/.ssh/id_ed25519")
	f.fingerprint.SetPlaceHolder("optional pin, e.g. SHA256:…")
	f.passHint = widget.NewLabel("")
	f.passHint.Importance = widget.LowImportance
	f.passHint.Wrapping = fyne.TextWrapWord

	browse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err == nil && r != nil {
				f.key.SetText(r.URI().Path())
				_ = r.Close()
			}
		}, s.w.win)
		d.Show()
	})
	test := widget.NewButtonWithIcon("Test connection", theme.ConfirmIcon(), func() { s.test(f) })

	form := widget.NewForm(
		widget.NewFormItem("Host", f.host),
		widget.NewFormItem("Port", f.port),
		widget.NewFormItem("Username", f.user),
		widget.NewFormItem("Password", f.pass),
		widget.NewFormItem("", f.passHint),
		widget.NewFormItem("Private key", container.NewBorder(nil, nil, nil, browse, f.key)),
		widget.NewFormItem("Folder", f.dir),
		widget.NewFormItem("Host key pin", f.fingerprint),
	)
	f.card = widget.NewCard(title, subtitle, container.NewVBox(form, container.NewHBox(test)))
	return f
}

func (s *settingsView) loadEndpoint(f *endpointForm, ep config.Endpoint) {
	f.host.SetText(ep.Host)
	f.port.SetText(strconv.Itoa(ep.Port))
	f.user.SetText(ep.Username)
	f.pass.SetText(ep.Password)
	f.key.SetText(ep.PrivateKeyPath)
	f.dir.SetText(ep.Directory)
	f.fingerprint.SetText(ep.ExpectedFingerprint)
	switch {
	case ep.Password != "":
		f.passHint.SetText("⚠ Stored in plain text in config.json — save with the keychain option on to move it.")
	case s.w.ctl.HasStoredPassword(ep):
		f.pass.SetPlaceHolder("•••••••• saved in keychain")
		f.passHint.SetText("Saved in the system keychain. Leave blank to keep it.")
	default:
		f.pass.SetPlaceHolder("")
		f.passHint.SetText(fmt.Sprintf("Or set %s for servers/CI.", secrets.EnvVar(f.role, "password")))
	}
}

func (s *settingsView) load() {
	cfg := s.w.ctl.Config()
	s.loadEndpoint(s.src, cfg.Source)
	s.loadEndpoint(s.dst, cfg.Target)
	s.backup.SetText(cfg.BackupDirectory)
	s.include.SetText(strings.Join(cfg.Include, ", "))
	s.exclude.SetText(strings.Join(cfg.Exclude, ", "))
	s.stable.SetText(strconv.Itoa(cfg.StableSeconds))
	s.parallel.SetText(strconv.Itoa(cfg.MaxParallel))
	s.verify.SetChecked(cfg.VerifyChecksumEnabled())
	s.poll.SetText(strconv.Itoa(cfg.PollInterval))
	s.retries.SetText(strconv.Itoa(cfg.ReconnectRetries))
	s.reconnect.SetText(strconv.Itoa(cfg.ReconnectInterval))
	s.keepAlive.SetText(strconv.Itoa(cfg.KeepAliveDuration))
	s.idleScans.SetText(strconv.Itoa(cfg.MaxIdleScans))
	s.logDays.SetText(strconv.Itoa(cfg.LogRetentionDays))
	s.strict.SetChecked(cfg.StrictHostKeyChecking)
	s.initial.SetChecked(cfg.InitialSyncEnabled())
	s.notify.SetChecked(cfg.NotificationsEnabled())
	s.keychain.SetChecked(cfg.Source.Password == "" && cfg.Target.Password == "")
	s.schedOn.SetChecked(cfg.ActiveSchedule.Enabled)
	s.schedStart.SetText(cfg.ActiveSchedule.Start)
	s.schedEnd.SetText(cfg.ActiveSchedule.End)
	s.schedTZ.SetText(cfg.ActiveSchedule.Timezone)
}

func patterns(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoi(field string, e *widget.Entry, errs *[]error) int {
	v, err := strconv.Atoi(strings.TrimSpace(e.Text))
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a whole number", field))
	}
	return v
}

func readEndpoint(f *endpointForm, errs *[]error) config.Endpoint {
	ep := config.Endpoint{
		Host:                strings.TrimSpace(f.host.Text),
		Username:            strings.TrimSpace(f.user.Text),
		Password:            f.pass.Text,
		PrivateKeyPath:      strings.TrimSpace(f.key.Text),
		Directory:           strings.TrimSpace(f.dir.Text),
		ExpectedFingerprint: strings.TrimSpace(f.fingerprint.Text),
	}
	if ep.Host != "" || strings.TrimSpace(f.port.Text) != "" {
		ep.Port = atoi(f.role+" port", f.port, errs)
	}
	return ep
}

// read builds a config from the form. Passwords typed into the form are kept
// in the returned config; save() moves them to the keychain if requested.
func (s *settingsView) read() (*config.Config, error) {
	var errs []error
	cfg := s.w.ctl.Config()
	cfg.Source = readEndpoint(s.src, &errs)
	cfg.Target = readEndpoint(s.dst, &errs)
	cfg.BackupDirectory = strings.TrimSpace(s.backup.Text)
	cfg.Include = patterns(s.include.Text)
	cfg.Exclude = patterns(s.exclude.Text)
	cfg.StableSeconds = atoi("Wait until unchanged", s.stable, &errs)
	cfg.MaxParallel = atoi("Parallel transfers", s.parallel, &errs)
	verify, initial, notify := s.verify.Checked, s.initial.Checked, s.notify.Checked
	cfg.VerifyChecksum, cfg.EnableInitialSync, cfg.Notifications = &verify, &initial, &notify
	cfg.PollInterval = atoi("Scan every", s.poll, &errs)
	cfg.ReconnectRetries = atoi("Retries", s.retries, &errs)
	cfg.ReconnectInterval = atoi("Retry delay", s.reconnect, &errs)
	cfg.KeepAliveDuration = atoi("Keep-alive", s.keepAlive, &errs)
	cfg.MaxIdleScans = atoi("Idle scans", s.idleScans, &errs)
	cfg.LogRetentionDays = atoi("Keep logs", s.logDays, &errs)
	cfg.StrictHostKeyChecking = s.strict.Checked
	cfg.ActiveSchedule = config.Schedule{
		Enabled:  s.schedOn.Checked,
		Start:    strings.TrimSpace(s.schedStart.Text),
		End:      strings.TrimSpace(s.schedEnd.Text),
		Timezone: strings.TrimSpace(s.schedTZ.Text),
	}
	if cfg.Exclude == nil {
		cfg.Exclude = []string{}
	}
	return cfg, errors.Join(errs...)
}

func (s *settingsView) save() {
	cfg, err := s.read()
	if err == nil {
		cfg.ApplyDefaults()
		err = cfg.Validate()
	}
	if err != nil {
		dialog.ShowError(err, s.w.win)
		return
	}
	if s.keychain.Checked {
		for _, ep := range []*config.Endpoint{&cfg.Source, &cfg.Target} {
			if ep.Password == "" || !ep.Enabled() {
				continue
			}
			if err := s.w.ctl.SavePassword(*ep, ep.Password); err != nil {
				dialog.ShowError(fmt.Errorf("could not save the %s password to the keychain (%v). "+
					"Untick the keychain option to keep it in config.json, or use an environment variable", ep.Host, err), s.w.win)
				return
			}
			ep.Password = ""
		}
	} else if s.w.ctl.Store != nil {
		// Keychain entries take precedence over config.json, so remove stale ones.
		for _, ep := range []config.Endpoint{cfg.Source, cfg.Target} {
			if ep.Password != "" && ep.Enabled() {
				_ = s.w.ctl.Store.Delete(secrets.Account(ep.Username, ep.Host, ep.Port))
			}
		}
	}

	progress := dialog.NewCustomWithoutButtons("Applying settings", widget.NewProgressBarInfinite(), s.w.win)
	progress.Show()
	go func() {
		err := s.w.ctl.Apply(cfg) // waits for running transfers
		fyne.Do(func() {
			progress.Hide()
			if err != nil {
				dialog.ShowError(err, s.w.win)
				return
			}
			s.load()
			dialog.ShowInformation("Settings saved", "The new settings are active.", s.w.win)
		})
	}()
}

func (s *settingsView) test(f *endpointForm) {
	cfg, err := s.read()
	if err != nil {
		dialog.ShowError(err, s.w.win)
		return
	}
	cfg.ApplyDefaults()
	progress := dialog.NewCustomWithoutButtons("Testing "+f.role+" connection…", widget.NewProgressBarInfinite(), s.w.win)
	progress.Show()
	go func() {
		res, err := setup.Test(context.Background(), s.w.ctl.Path, cfg, f.role, s.w.ctl.Store)
		fyne.Do(func() {
			progress.Hide()
			if err != nil && res.Unknown {
				msg := fmt.Sprintf("This server is not in known_hosts yet.\n\n%s key fingerprint:\n%s\n\nOnly trust it if it matches what the server administrator gave you.", res.KeyType, res.Fingerprint)
				dialog.ShowConfirm("Trust this server?", msg, func(ok bool) {
					if ok {
						if err := setup.TrustTested(s.w.ctl.Path, cfg, res); err != nil {
							dialog.ShowError(err, s.w.win)
							return
						}
						s.test(f)
					}
				}, s.w.win)
				return
			}
			if err != nil {
				dialog.ShowError(fmt.Errorf("%s connection failed: %w", f.role, err), s.w.win)
				return
			}
			write := "yes"
			if !res.Writable {
				write = "NO — check folder permissions"
			}
			what := "Folder"
			if f.role == setup.Source {
				what = "Archive folder"
			}
			dialog.ShowInformation("Connection OK", fmt.Sprintf(
				"Connected in %s.\n\nHost key (%s): %s\nEntries in folder: %d\n%s writable: %s",
				res.Elapsed.Round(time.Millisecond), res.KeyType, res.Fingerprint, res.Files, what, write), s.w.win)
		})
	}()
}
