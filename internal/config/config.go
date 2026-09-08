package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/ostamand/castor/internal/sysinfo"
	"github.com/pelletier/go-toml/v2"
)

// Master configuration struct
type Config struct {
	Namespace    string              `toml:"namespace"`
	ShowTips     *bool               `toml:"show_tips,omitempty"`
	Performance  PerformanceConfig   `toml:"performance"`
	Safety       SafetyConfig        `toml:"safety"`
	Security     SecurityConfig      `toml:"security"`
	Destinations []DestinationConfig `toml:"destinations,omitempty"`
	Targets      []TargetConfig      `toml:"targets,omitempty"`
	Rules        RulesConfig         `toml:"rules"`
}

type PerformanceConfig struct {
	MaxWorkers       int `toml:"max_workers"`
	CompressionLevel int `toml:"compression_level"`
}

type SafetyConfig struct {
	MaxArchiveSizeGB float64 `toml:"max_archive_size_gb"`
}

type SecurityConfig struct {
	Encrypt          bool     `toml:"encrypt"`
	EncryptionMethod string   `toml:"encryption_method"`
	AgePublicKeys    []string `toml:"age_public_keys"`
	AgePrivateKeyFile string  `toml:"age_private_key_file,omitempty"`
}

type DestinationConfig struct {
	Name     string `toml:"name"`
	Provider string `toml:"provider"` // "gcs", "gdrive", "s3"
	Bucket   string `toml:"bucket,omitempty"`
	Location string `toml:"location,omitempty"`
	Prefix   string `toml:"prefix,omitempty"`
	Folder   string `toml:"folder,omitempty"` // For Google Drive e.g. "CastorLodge"
	Path            string `toml:"path,omitempty"`   // For local directory provider e.g. "/mnt/vault"
	CredentialsFile string `toml:"credentials_file,omitempty"` // For service account key file (e.g. GCS)
	Disabled        bool   `toml:"disabled,omitempty"`
}

type TargetConfig struct {
	Name            string   `toml:"name,omitempty"`
	Path            string   `toml:"path"`
	Type            string   `toml:"type"` // "git", "documents", "media", "generic"
	Compression     string   `toml:"compression,omitempty"` // "zstd", "xz"
	CreateGitBundle bool     `toml:"create_git_bundle,omitempty"`
	Namespace       string   `toml:"namespace,omitempty"` // Sub-namespace override
	MaxSizeGB       float64  `toml:"max_size_gb,omitempty"`
	Destinations    []string `toml:"destinations,omitempty"`
}

type RulesConfig struct {
	Git       RuleConfig `toml:"git"`
	Documents RuleConfig `toml:"documents"`
	Media     RuleConfig `toml:"media"`
	Generic   RuleConfig `toml:"generic"`
}

type RuleConfig struct {
	Excludes []string `toml:"excludes"`
}

// DefaultConfigPath returns ~/.config/castor/config.toml
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "castor", "config.toml")
}

// DefaultStatePath returns ~/.config/castor/state.json
func DefaultStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "castor", "state.json")
}

// StatePathForConfig returns state.json located alongside the given config.toml
func StatePathForConfig(configPath string) string {
	if configPath == "" {
		return DefaultStatePath()
	}
	return filepath.Join(filepath.Dir(configPath), "state.json")
}

// DefaultCredentialsPath returns ~/.config/castor/credentials.json
func DefaultCredentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "castor", "credentials.json")
}

// DefaultDropboxCredentialsPath returns ~/.config/castor/dropbox_credentials.json
func DefaultDropboxCredentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "castor", "dropbox_credentials.json")
}

// DefaultConfig initializes a recommended configuration
func DefaultConfig() *Config {
	host := sysinfo.GetHostInfo()
	numCPU := runtime.NumCPU()
	if numCPU < 2 {
		numCPU = 2
	} else if numCPU > 4 {
		numCPU = 4
	}

	showTips := true
	return &Config{
		Namespace: host.DefaultNamespace,
		ShowTips:  &showTips,
		Performance: PerformanceConfig{
			MaxWorkers:       numCPU,
			CompressionLevel: 19, // Optimal zstd cold storage compression
		},
		Safety: SafetyConfig{
			MaxArchiveSizeGB: 5.0,
		},
		Security: SecurityConfig{
			Encrypt:          true,
			EncryptionMethod: "age",
			AgePublicKeys:    []string{},
		},
		Destinations: []DestinationConfig{},
		Targets:      []TargetConfig{},
		Rules: RulesConfig{
			Git: RuleConfig{
				Excludes: []string{
					".git", "node_modules", ".npm", ".yarn", ".pnpm-store",
					".next", ".nuxt", ".turbo", "dist", "build", "out",
					"__pycache__", "*.pyc", ".venv", "venv", "env",
					"target", "bin", "obj", "*.log", ".DS_Store",
				},
			},
			Documents: RuleConfig{
				Excludes: []string{"*.tmp", "temp", "tmp", "~$*", ".DS_Store", "Thumbs.db"},
			},
			Media: RuleConfig{
				Excludes: []string{".cache", "Thumbs.db", ".DS_Store"},
			},
			Generic: RuleConfig{
				Excludes: []string{".cache", "tmp", "temp", "*.tmp", ".DS_Store", "Thumbs.db"},
			},
		},
	}
}

// AreTipsEnabled returns whether educational tips should be shown
func (c *Config) AreTipsEnabled() bool {
	if c == nil || c.ShowTips == nil {
		return true
	}
	return *c.ShowTips
}

// LoadConfig reads and parses the TOML config file
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file '%s': %w", path, err)
	}

	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse TOML config '%s': %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate checks configuration sanity
func (c *Config) Validate() error {
	c.Namespace = strings.TrimSpace(c.Namespace)
	if c.Namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}

	if c.Performance.MaxWorkers <= 0 {
		c.Performance.MaxWorkers = 2
	}
	if c.Performance.CompressionLevel <= 0 || c.Performance.CompressionLevel > 22 {
		c.Performance.CompressionLevel = 19
	}
	if c.Safety.MaxArchiveSizeGB <= 0 {
		c.Safety.MaxArchiveSizeGB = 5.0
	}

	// Validate Security
	if c.Security.Encrypt && len(c.Security.AgePublicKeys) == 0 {
		return fmt.Errorf("security.encrypt is true but no age_public_keys are specified")
	}

	// Validate Destinations
	destNames := make(map[string]bool)
	for _, d := range c.Destinations {
		if d.Name == "" {
			return fmt.Errorf("destination missing name")
		}
		if destNames[d.Name] {
			return fmt.Errorf("duplicate destination name: '%s'", d.Name)
		}
		destNames[d.Name] = true

		switch d.Provider {
		case "gcs":
			if d.Bucket == "" {
				return fmt.Errorf("gcs destination '%s' requires a bucket", d.Name)
			}
		case "gdrive":
			if d.Folder == "" {
				return fmt.Errorf("gdrive destination '%s' requires a folder", d.Name)
			}
		case "local", "file", "fs":
			if d.Path == "" {
				return fmt.Errorf("local destination '%s' requires a path", d.Name)
			}
		case "dropbox", "dbx":
			// Folder is optional; defaults to /CastorLodge or root of App folder
		case "memory":
			// in-memory provider for testing
		default:
			return fmt.Errorf("destination '%s' has unsupported provider '%s'", d.Name, d.Provider)
		}
	}

	// Validate Targets
	targetPaths := make(map[string]bool)
	targetNames := make(map[string]bool)
	for i := range c.Targets {
		t := &c.Targets[i]
		cleanPath := filepath.Clean(sysinfo.ExpandHome(t.Path))
		if cleanPath == "" || cleanPath == "." {
			return fmt.Errorf("target has invalid path '%s'", t.Path)
		}
		if targetPaths[cleanPath] {
			return fmt.Errorf("duplicate target path '%s'", t.Path)
		}
		targetPaths[cleanPath] = true

		// Auto-derive name from path basename if not set
		if t.Name == "" {
			t.Name = NormalizeTargetName(filepath.Base(cleanPath))
		}

		if targetNames[t.Name] {
			return fmt.Errorf("duplicate target name '%s'", t.Name)
		}
		targetNames[t.Name] = true

		if t.Type == "" {
			t.Type = "generic"
		}
		if t.Compression == "" {
			t.Compression = "zstd"
		}
	}

	return nil
}

// SaveConfig serializes the config to TOML file
func SaveConfig(path string, cfg *Config) error {
	if path == "" {
		path = DefaultConfigPath()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to encode config to TOML: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// NormalizeTargetName converts a raw directory name into kebab-case.
// Forward slashes are preserved for hierarchical names (e.g. "work/MyProject" → "work/my-project").
// Examples: "MyProject" → "my-project", "my_project" → "my-project",
// "Generated Visions Workflows" → "generated-visions-workflows"
func NormalizeTargetName(name string) string {
	// Split on slashes, normalize each segment, rejoin
	segments := strings.Split(name, "/")
	var normalized []string
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		normalized = append(normalized, normalizeSegment(seg))
	}
	return strings.Join(normalized, "/")
}

// normalizeSegment converts a single name segment to kebab-case
func normalizeSegment(name string) string {
	// Insert hyphens at CamelCase boundaries: "MyProject" → "My-Project"
	var parts []rune
	runes := []rune(name)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				parts = append(parts, '-')
			} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				parts = append(parts, '-')
			}
		}
		parts = append(parts, r)
	}

	s := string(parts)
	s = strings.ToLower(s)

	// Replace common separators with hyphens
	s = strings.NewReplacer(
		"_", "-",
		" ", "-",
		".", "-",
	).Replace(s)

	// Strip anything that isn't alphanumeric or hyphen
	var clean []rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			clean = append(clean, r)
		}
	}
	s = string(clean)

	// Collapse multiple hyphens and trim
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")

	return s
}

// ActiveDestinations returns only destinations that are not disabled.
func (c *Config) ActiveDestinations() []DestinationConfig {
	var active []DestinationConfig
	for _, d := range c.Destinations {
		if !d.Disabled {
			active = append(active, d)
		}
	}
	return active
}

// FindDestination returns the destination matching name (case-insensitively), its index, and true if found.
func (c *Config) FindDestination(name string) (DestinationConfig, int, bool) {
	nameLower := strings.ToLower(strings.TrimSpace(name))
	for i, d := range c.Destinations {
		if strings.ToLower(d.Name) == nameLower {
			return d, i, true
		}
	}
	return DestinationConfig{}, -1, false
}
