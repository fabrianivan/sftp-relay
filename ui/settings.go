package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	card                   fyne.CanvasObject
}

type settingsView struct {
	w       *Window
	content fyne.CanvasObject

	src, dst *endpointForm

	localOn                             *widget.Check
	localDir, localTarget, localArchive *widget.Entry
	localMode                           *widget.RadioGroup

	tabs    *segmented
	tabBody *fyne.Container

	backup, include, exclude, stable, parallel     *widget.Entry
	verify                                         *widget.Check
	poll, retries, reconnect, keepAlive, idleScans *widget.Entry
	strict, initial, notify, keychain              *widget.Check
	logDays                                        *widget.Entry
	schedOn                                        *widget.Check
	schedStart, schedEnd, schedTZ                  *widget.Entry
}

// What happens to a local file once it is on the target.
const (
	localKeepLabel    = "Keep it where it is, and upload it again whenever it changes"
	localArchiveLabel = "Move it to the archive subfolder named below"
)

// settingsCard is one tab of the settings screen: a heading, the fields
// (which scroll when the window is short) and an optional footer of actions.
func settingsCard(title, description string, fields, footer fyne.CanvasObject) fyne.CanvasObject {
	head := vstack(12, vstack(5, sectionTitle(title), subtitle(description)), hairline())
	var foot fyne.CanvasObject
	if footer != nil {
		foot = vstack(12, hairline(), footer)
	}
	body := container.NewVScroll(pad(6, 6, 0, 4, fields))
	return pad(gap, 0, 0, 0, panel(container.NewBorder(head, foot, nil, nil, body)))
}

func newSettingsView(w *Window) *settingsView {
	s := &settingsView{w: w}
	s.src = s.endpointForm(setup.Source, "Source server", "The SFTP server and folder that are watched for new files. Leave the host empty to only back up a local folder.")
	s.dst = s.endpointForm(setup.Target, "Target server", "Where files are delivered. Leave the host empty to only archive files.")

	s.backup = widget.NewEntry()
	s.include = widget.NewEntry()
	s.include.SetPlaceHolder("* (all files)")
	s.exclude = widget.NewEntry()
	s.exclude.SetPlaceHolder("*.tmp, .DS_Store (comma-separated)")
	s.stable = widget.NewEntry()
	s.parallel = widget.NewEntry()
	s.verify = widget.NewCheck("Verify file integrity (SHA-256) after transfer", nil)

	rulesCard := settingsCard("Relay rules", "Which files are picked up and how they are processed.", widget.NewForm(
		widget.NewFormItem("Archive folder (source)", s.backup),
		widget.NewFormItem("Include patterns", s.include),
		widget.NewFormItem("Exclude patterns", s.exclude),
		widget.NewFormItem("File stability delay (s)", s.stable),
		widget.NewFormItem("Max parallel transfers", s.parallel),
		widget.NewFormItem("", s.verify),
	), nil)

	s.schedOn = widget.NewCheck("Only relay during active hours", nil)
	s.schedStart, s.schedEnd, s.schedTZ = widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	s.schedStart.SetPlaceHolder("06:00 (HH:MM)")
	s.schedEnd.SetPlaceHolder("22:00 (HH:MM, earlier than start = overnight)")
	s.schedTZ.SetPlaceHolder("Local, UTC, or e.g. Asia/Jakarta")
	schedCard := settingsCard("Schedule", "Limit scanning to a daily time window.", widget.NewForm(
		widget.NewFormItem("", s.schedOn),
		widget.NewFormItem("Start time", s.schedStart),
		widget.NewFormItem("End time", s.schedEnd),
		widget.NewFormItem("Timezone", s.schedTZ),
	), nil)

	s.poll, s.retries, s.reconnect, s.keepAlive, s.idleScans, s.logDays =
		widget.NewEntry(), widget.NewEntry(), widget.NewEntry(), widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	s.strict = widget.NewCheck("Strict host key checking (reject unknown hosts)", nil)
	s.initial = widget.NewCheck("Scan immediately when the app starts", nil)
	s.notify = widget.NewCheck("Show desktop notifications", nil)
	s.keychain = widget.NewCheck("Store passwords in the system keychain (recommended)", nil)

	appCard := settingsCard("Engine", "Connection handling and application behaviour.", widget.NewForm(
		widget.NewFormItem("Scan interval (s)", s.poll),
		widget.NewFormItem("Max connection retries", s.retries),
		widget.NewFormItem("Retry backoff (s)", s.reconnect),
		widget.NewFormItem("SSH keep-alive (s)", s.keepAlive),
		widget.NewFormItem("Idle disconnect (scans)", s.idleScans),
		widget.NewFormItem("Log retention (days)", s.logDays),
		widget.NewFormItem("", s.keychain),
		widget.NewFormItem("", s.strict),
		widget.NewFormItem("", s.initial),
		widget.NewFormItem("", s.notify),
	), nil)

	cards := []fyne.CanvasObject{s.src.card, s.localCard(), s.dst.card, rulesCard, schedCard, appCard}
	s.tabBody = container.NewStack()
	s.tabs = newSegmented([]string{"Source server", "Local backup", "Target server", "Rules", "Schedule", "Engine"}, func(i int) {
		s.tabBody.Objects = []fyne.CanvasObject{cards[i]}
		s.tabBody.Refresh()
	})
	s.tabs.Select(0)

	save := widget.NewButtonWithIcon("Save & apply", theme.DocumentSaveIcon(), s.save)
	save.Importance = widget.HighImportance
	revert := widget.NewButtonWithIcon("Revert", theme.ContentUndoIcon(), s.load)

	about := mutedLabel(AppName + " " + versionLabel(w.version) + "  ·  config file: " + w.ctl.Path)
	s.content = page(pageHeader("Settings", about, revert, save),
		container.NewBorder(s.tabs.box, nil, nil, nil, s.tabBody))
	s.load()
	return s
}

// localCard builds the Local backup tab: a folder on this computer that is
// uploaded to the target, next to (or instead of) the source server.
func (s *settingsView) localCard() fyne.CanvasObject {
	s.localOn = widget.NewCheck("Back up a folder on this computer to the target server", nil)
	s.localDir = widget.NewEntry()
	s.localDir.SetPlaceHolder("Choose a folder, e.g. /Users/you/Exports")
	browse := widget.NewButtonWithIcon("Browse…", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
			if err == nil && u != nil {
				s.localDir.SetText(filepath.FromSlash(u.Path()))
			}
		}, s.w.win)
	})
	s.localTarget = widget.NewEntry()
	s.localTarget.SetPlaceHolder("Leave empty to use the target server's folder")
	s.localMode = widget.NewRadioGroup([]string{localKeepLabel, localArchiveLabel}, nil)
	s.localMode.Required = true
	s.localArchive = widget.NewEntry()
	s.localArchive.SetPlaceHolder("archive")

	note := widget.NewLabel("Local files go through the same rules, checks and schedule as the relay. " +
		"Only files directly in the folder are backed up, not its subfolders.")
	note.Importance = widget.LowImportance
	note.Wrapping = fyne.TextWrapWord

	check := widget.NewButtonWithIcon("Check folder", theme.SearchIcon(), s.testLocal)
	form := widget.NewForm(
		widget.NewFormItem("", s.localOn),
		widget.NewFormItem("Folder", container.NewBorder(nil, nil, nil, browse, s.localDir)),
		widget.NewFormItem("Target folder", s.localTarget),
		widget.NewFormItem("After upload", s.localMode),
		widget.NewFormItem("Archive subfolder", s.localArchive),
		widget.NewFormItem("", note),
	)
	return settingsCard("Local backup", "Send files from a folder on this computer to the target server.", form, container.NewHBox(check))
}

func (s *settingsView) testLocal() {
	cfg, err := s.read()
	if err != nil {
		dialog.ShowError(err, s.w.win)
		return
	}
	cfg.ApplyDefaults()
	res, err := setup.TestLocal(cfg)
	if err != nil {
		dialog.ShowError(fmt.Errorf("local folder: %w", err), s.w.win)
		return
	}
	msg := fmt.Sprintf("%d file(s) in the folder, %d of them match the include/exclude rules.", res.Files, res.Matching)
	if !cfg.LocalBackup.Keep() && !res.Writable {
		msg += "\n\nThe folder is read-only, so files cannot be moved to the archive subfolder after upload."
	}
	dialog.ShowInformation("Folder OK", msg, s.w.win)
}

func (s *settingsView) endpointForm(role, title, description string) *endpointForm {
	f := &endpointForm{role: role}
	f.host, f.port, f.user = widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	f.pass = widget.NewPasswordEntry()
	f.key, f.dir, f.fingerprint = widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	f.key.SetPlaceHolder("Optional, e.g. ~/.ssh/id_ed25519")
	f.fingerprint.SetPlaceHolder("Optional pin, e.g. SHA256:…")
	f.dir.SetPlaceHolder("/remote/path/to/folder")
	f.passHint = widget.NewLabel("")
	f.passHint.Importance = widget.LowImportance
	f.passHint.Wrapping = fyne.TextWrapWord

	browse := widget.NewButtonWithIcon("Browse…", theme.FolderOpenIcon(), func() {
		d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err == nil && r != nil {
				f.key.SetText(r.URI().Path())
				_ = r.Close()
			}
		}, s.w.win)
		d.Show()
	})
	test := widget.NewButtonWithIcon("Test connection", theme.LoginIcon(), func() { s.test(f) })

	form := widget.NewForm(
		widget.NewFormItem("Host", f.host),
		widget.NewFormItem("Port", f.port),
		widget.NewFormItem("Username", f.user),
		widget.NewFormItem("Password", f.pass),
		widget.NewFormItem("", f.passHint),
		widget.NewFormItem("Private key", container.NewBorder(nil, nil, nil, browse, f.key)),
		widget.NewFormItem("Remote folder", f.dir),
		widget.NewFormItem("Host key pin", f.fingerprint),
	)
	f.card = settingsCard(title, description, form, container.NewHBox(test))
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
	f.passHint.Importance = widget.LowImportance
	switch {
	case ep.Password != "":
		f.passHint.Importance = widget.WarningImportance
		f.passHint.SetText("Stored in plain text in config.json — save with the keychain option on to move it.")
	case s.w.ctl.HasStoredPassword(ep):
		f.pass.SetPlaceHolder("•••••••• saved in keychain")
		f.passHint.SetText("Saved in the system keychain. Leave blank to keep it.")
	default:
		f.pass.SetPlaceHolder("")
		f.passHint.SetText(fmt.Sprintf("Or set %s for servers/CI.", secrets.EnvVar(f.role, "password")))
	}
	f.passHint.Refresh()
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

	lb := cfg.LocalBackup
	s.localOn.SetChecked(lb.Enabled)
	s.localDir.SetText(lb.Directory)
	s.localTarget.SetText(lb.TargetDirectory)
	if lb.Keep() {
		s.localMode.SetSelected(localKeepLabel)
	} else {
		s.localMode.SetSelected(localArchiveLabel)
	}
	s.localArchive.SetText(lb.ArchiveFolder)
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
	mode := config.LocalKeep
	if s.localMode.Selected == localArchiveLabel {
		mode = config.LocalArchive
	}
	cfg.LocalBackup = config.LocalBackup{
		Enabled:         s.localOn.Checked,
		Directory:       strings.TrimSpace(s.localDir.Text),
		TargetDirectory: strings.TrimSpace(s.localTarget.Text),
		AfterUpload:     mode,
		ArchiveFolder:   strings.TrimSpace(s.localArchive.Text),
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
