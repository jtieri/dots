// Package config loads the settings for dots from its configuration file and
// the environment.
//
// Each setting comes from, in increasing order of precedence, its default,
// the configuration file, and its environment variable. The configuration
// file is $XDG_CONFIG_HOME/dots/config.toml unless DOTS_CONFIG or --config
// names another. Settings are checked strictly: an unknown setting, a value
// of the wrong type or a value out of range is an error, so a mistyped
// setting can never be silently ignored.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Config holds the settings for dots. Paths in the home directory are
// relative to it, with "." for the home directory itself.
type Config struct {
	Branch          string           `mapstructure:"branch"`
	Capture         Capture          `mapstructure:"capture"`
	FetchTimeout    time.Duration    `mapstructure:"fetch_timeout"`
	Leftovers       Leftovers        `mapstructure:"leftovers"`
	ModifyTemplates []ModifyTemplate `mapstructure:"modify_templates"`
	OverwriteBackup OverwriteBackup  `mapstructure:"overwrite_backup"`
	Push            Push             `mapstructure:"push"`
	RestartNotices  []RestartNotice  `mapstructure:"restart_notices"`
}

// Capture holds the settings for dots capture.
type Capture struct {
	// Protected are patterns, as path.Match takes them, for files that are
	// never captured. A pattern that matches a folder protects everything
	// in it.
	Protected []string `mapstructure:"protected"`
}

// Leftovers holds the settings for the leftovers check.
type Leftovers struct {
	Skip []string `mapstructure:"skip"`
}

// OverwriteBackup holds the settings for the copies dots keeps of files it is
// told to overwrite.
type OverwriteBackup struct {
	Enabled bool `mapstructure:"enabled"`
	// KeepDays is how long copies are kept; 0 keeps them forever.
	KeepDays int `mapstructure:"keep_days"`
}

// Push holds the settings for dots push.
type Push struct {
	// PackageLists is the folder, relative to the top of the repo, that
	// holds each machine's package lists. Empty leaves them alone.
	PackageLists string `mapstructure:"package_lists"`
}

// A ModifyTemplate is a file the repo keeps as a chezmoi modify template, so
// that one part of it stays as each machine has it.
type ModifyTemplate struct {
	File string `mapstructure:"file"`
	// Keep matches the part of the file each machine keeps for itself.
	Keep *regexp.Regexp `mapstructure:"keep"`
}

// A RestartNotice asks dots to say when a program should be restarted: when
// applying changes one of Files while the program is running.
type RestartNotice struct {
	// Program is the program's process name, as ps shows it.
	Program string   `mapstructure:"program"`
	Files   []string `mapstructure:"files"`
}

// A Setting describes one setting, for loading it and for dots help.
type Setting struct {
	// Key is the setting's name in the configuration file, with a dot
	// between a table and the setting in it.
	Key string
	// Env is the environment variable that overrides the file, if any.
	Env string
	// Default is the value used when neither sets it, written as it
	// would be in the file.
	Default any
	Help    string
}

// DefaultText returns the setting's default written as it would be in the
// configuration file, such as "main" or ["~", "~/.config"].
func (s Setting) DefaultText() string {
	switch v := s.Default.(type) {
	case string:
		return strconv.Quote(v)
	case []string:
		quoted := make([]string, len(v))
		for i, item := range v {
			quoted[i] = strconv.Quote(item)
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	}
	// The lists of tables, which are empty by default.
	if rv := reflect.ValueOf(s.Default); rv.Kind() == reflect.Slice && rv.Len() == 0 {
		return "[]"
	}
	return fmt.Sprint(s.Default)
}

// Settings lists every setting.
var Settings = []Setting{
	{
		Key:     "branch",
		Env:     "DOTS_BRANCH",
		Default: "main",
		Help:    "The branch every machine should be on.",
	},
	{
		Key:     "capture.protected",
		Env:     "DOTS_CAPTURE_PROTECTED",
		Default: []string{"~/.ssh/id_*", "~/.gnupg"},
		Help: "Files dots capture never copies into the repo, such as private keys. A * matches " +
			"any part of a name, and a folder protects everything in it. Setting it replaces " +
			"the default list.",
	},
	{
		Key:     "fetch_timeout",
		Env:     "DOTS_FETCH_TIMEOUT",
		Default: "10s",
		Help:    "How long to wait for the remote when fetching, such as \"10s\" or \"1m\".",
	},
	{
		Key:     "leftovers.skip",
		Env:     "DOTS_LEFTOVERS_SKIP",
		Default: []string{"~", "~/.config", "~/.ssh"},
		Help: "Folders the leftovers check does not look in, because they hold personal files " +
			"beside tracked ones. Setting it replaces the default list.",
	},
	{
		Key:     "modify_templates",
		Default: []ModifyTemplate{},
		Help: "Files the repo keeps as chezmoi modify templates, as a list of tables with file and " +
			"keep, a regular expression matching the part of the file each machine keeps for " +
			"itself. dots capture rewrites the rest of the template from the file, and dots warns " +
			"when one is replaced by a plain copy.",
	},
	{
		Key:     "overwrite_backup.enabled",
		Env:     "DOTS_OVERWRITE_BACKUP_ENABLED",
		Default: true,
		Help: "Whether to copy files changed on this machine before overwriting them, to " +
			"$XDG_STATE_HOME/dots/overwritten/.",
	},
	{
		Key:     "overwrite_backup.keep_days",
		Env:     "DOTS_OVERWRITE_BACKUP_KEEP_DAYS",
		Default: 30,
		Help:    "How many days to keep those copies. 0 keeps them forever.",
	},
	{
		Key:     "push.package_lists",
		Env:     "DOTS_PUSH_PACKAGE_LISTS",
		Default: "scripts/pkglists",
		Help: "The folder in the repo, relative to its top, where dots push keeps a folder of " +
			"package lists for each machine, named after it. Empty turns the lists off.",
	},
	{
		Key:     "restart_notices",
		Default: []RestartNotice{},
		Help: "Programs to restart after dots apply changes their files, as a list of tables " +
			"with program (the process name) and files. dots says so when one is running.",
	},
}

// EnvConfig names the environment variable that selects another
// configuration file.
const EnvConfig = "DOTS_CONFIG"

// DefaultPath returns the configuration file used when no other is named.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dots", "config.toml"), nil
}

// Load loads the settings. If file is empty, Load reads the file named by
// DOTS_CONFIG, or the default file if that is unset; only the default file may
// be missing.
func Load(file string) (*Config, error) {
	if file == "" {
		file = os.Getenv(EnvConfig)
	}
	explicit := file != ""
	if !explicit {
		var err error
		if file, err = DefaultPath(); err != nil {
			return nil, err
		}
	}

	v := viper.New()
	for _, s := range Settings {
		v.SetDefault(s.Key, s.Default)
		if s.Env == "" {
			continue
		}
		if err := v.BindEnv(s.Key, s.Env); err != nil {
			return nil, err
		}
	}
	v.SetConfigFile(file)
	v.SetConfigType("toml")
	err := v.ReadInConfig()
	switch {
	case errors.Is(err, fs.ErrNotExist) && !explicit:
	case err != nil:
		return nil, fmt.Errorf("config file %s: %w", file, err)
	default:
		if err := checkKeys(v); err != nil {
			return nil, fmt.Errorf("config file %s: %w", file, err)
		}
	}

	var c Config
	strict := func(dc *mapstructure.DecoderConfig) {
		dc.WeaklyTypedInput = false
		// Values from the environment are strings; these turn them into
		// the setting's type.
		dc.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			durationHook,
			wholeNumberHook,
			mapstructure.StringToSliceHookFunc(","),
			mapstructure.StringToBoolHookFunc(),
			mapstructure.StringToIntHookFunc(),
			regexpHook,
		)
	}
	if err := v.UnmarshalExact(&c, strict); err != nil {
		return nil, fmt.Errorf("config: %s", decodeMessage(err))
	}
	if err := c.check(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

// durationHook decodes a duration from a string such as "10s". It rejects a
// bare number, which would otherwise be taken as nanoseconds.
func durationHook(_, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeFor[time.Duration]() {
		return data, nil
	}
	str, ok := data.(string)
	if !ok {
		return nil, fmt.Errorf("%v is not a duration; write it as a string such as \"10s\"", data)
	}
	return time.ParseDuration(str)
}

// regexpHook compiles a regular expression from a string.
func regexpHook(_, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeFor[*regexp.Regexp]() {
		return data, nil
	}
	str, ok := data.(string)
	if !ok {
		return nil, fmt.Errorf("%v is not a regular expression; write it as a string", data)
	}
	return regexp.Compile(str)
}

// wholeNumberHook rejects a number with a fractional part for a whole-number
// setting, which mapstructure would otherwise truncate.
func wholeNumberHook(from, to reflect.Type, data any) (any, error) {
	isFloat := from.Kind() == reflect.Float32 || from.Kind() == reflect.Float64
	if to.Kind() == reflect.Int && isFloat {
		return nil, fmt.Errorf("%v is not a whole number", data)
	}
	return data, nil
}

// decodeMessage returns the problems in a decoding error on one line,
// without the heading mapstructure puts above them.
func decodeMessage(err error) string {
	_, problems, ok := strings.Cut(err.Error(), "error(s):")
	if !ok {
		return err.Error()
	}
	var lines []string
	for line := range strings.Lines(problems) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; ")
}

// checkKeys reports a key in the file that is not a setting. UnmarshalExact
// would catch it too, but with a less helpful message.
func checkKeys(v *viper.Viper) error {
	for _, key := range v.AllKeys() {
		known := slices.ContainsFunc(Settings, func(s Setting) bool { return s.Key == key })
		if !known && v.InConfig(key) {
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	return nil
}

// check checks the values and turns home paths into paths relative to the
// home directory.
func (c *Config) check() error {
	if c.Branch == "" || strings.ContainsAny(c.Branch, " \t\n") {
		return fmt.Errorf("branch %q is not a branch name", c.Branch)
	}
	if c.FetchTimeout <= 0 {
		return fmt.Errorf("fetch_timeout %v must be more than zero", c.FetchTimeout)
	}
	if c.OverwriteBackup.KeepDays < 0 {
		return fmt.Errorf("overwrite_backup.keep_days %d must not be negative", c.OverwriteBackup.KeepDays)
	}
	for _, p := range c.Capture.Protected {
		if _, err := path.Match(p, ""); err != nil {
			return fmt.Errorf("capture.protected: %q is not a valid pattern", p)
		}
	}
	var err error
	if c.Capture.Protected, err = homePaths("capture.protected", c.Capture.Protected); err != nil {
		return err
	}
	if c.Leftovers.Skip, err = homePaths("leftovers.skip", c.Leftovers.Skip); err != nil {
		return err
	}
	for i := range c.ModifyTemplates {
		m := &c.ModifyTemplates[i]
		key := fmt.Sprintf("modify_templates[%d]", i)
		if m.Keep == nil {
			return fmt.Errorf("%s: keep is missing", key)
		}
		files, err := homePaths(key+".file", []string{m.File})
		if err != nil {
			return err
		}
		m.File = files[0]
	}
	if p := c.Push.PackageLists; p != "" && (!filepath.IsLocal(p) || filepath.Clean(p) != p) {
		return fmt.Errorf("push.package_lists %q must be a folder inside the repo, such as scripts/pkglists", p)
	}
	for i := range c.RestartNotices {
		n := &c.RestartNotices[i]
		key := fmt.Sprintf("restart_notices[%d]", i)
		if n.Program == "" || strings.Contains(n.Program, "/") {
			return fmt.Errorf("%s: program %q must be a process name, such as zed-editor", key, n.Program)
		}
		if len(n.Files) == 0 {
			return fmt.Errorf("%s: files is empty", key)
		}
		if n.Files, err = homePaths(key+".files", n.Files); err != nil {
			return err
		}
	}
	return nil
}

// homePaths turns paths written as "~" or "~/path" into paths relative to the
// home directory.
func homePaths(key string, paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, ok := strings.CutPrefix(p, "~/")
		if p == "~" {
			rel, ok = ".", true
		}
		rel = path.Clean(rel)
		if !ok || rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
			return nil, fmt.Errorf("%s: %q must be ~ or start with ~/ and stay inside the home directory", key, p)
		}
		out = append(out, rel)
	}
	return out, nil
}
