package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/engine"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	catNamespace string
	catDest      string
	catSecretKey string
	catOutFile   string
)

var catCmd = &cobra.Command{
	Use:   "cat <target> <file-path>",
	Short: "Stream a single file from a remote archive without full extraction",
	Long: `Streams the remote archive down in memory, locates the specified file,
writes its decrypted bytes directly to standard output (or an output file),
and terminates the connection immediately once transferred.`,
	Example: `  castor cat myproject README.md
  castor cat myproject config.toml -o config.toml
  castor cat myproject .env -k AGE-SECRET-KEY-1...`,
	Args: cobra.ExactArgs(2),
	RunE: runCat,
}

func init() {
	catCmd.Flags().StringVarP(&catNamespace, "namespace", "s", "", "Namespace to inspect")
	catCmd.Flags().StringVarP(&catDest, "dest", "d", "", "Specific destination to read from")
	catCmd.Flags().StringVarP(&catSecretKey, "key", "k", "", "Age private secret key or export CASTOR_AGE_KEY")
	catCmd.Flags().StringVarP(&catOutFile, "out", "o", "", "Write output to file path instead of stdout")
}

func runCat(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	targetName := args[0]
	filePath := args[1]

	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	targetNamespace := cfg.Namespace
	if catNamespace != "" {
		targetNamespace = catNamespace
	}

	if len(cfg.Destinations) == 0 {
		return fmt.Errorf("no cloud destinations configured")
	}

	dest := cfg.Destinations[0]
	if catDest != "" {
		found := false
		for _, d := range cfg.Destinations {
			if d.Name == catDest {
				dest = d
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("destination '%s' not found in configuration", catDest)
		}
	}

	prov, err := storage.NewProviderFromConfig(ctx, dest)
	if err != nil {
		return fmt.Errorf("failed to connect to provider '%s': %w", dest.Name, err)
	}
	defer prov.Close()

	// Resolve secret key
	secretKey := catSecretKey
	if secretKey == "" {
		secretKey = os.Getenv("CASTOR_AGE_KEY")
	}
	if secretKey == "" && cfg.Security.AgePrivateKeyFile != "" {
		if data, err := os.ReadFile(cfg.Security.AgePrivateKeyFile); err == nil {
			secretKey = strings.TrimSpace(string(data))
		}
	}
	if secretKey == "" && cfg.Security.Encrypt {
		if catOutFile == "" {
			fmt.Fprint(os.Stderr, "🔑 Enter Age Secret Key (AGE-SECRET-KEY-1...): ")
		} else {
			fmt.Print("🔑 Enter Age Secret Key (AGE-SECRET-KEY-1...): ")
		}
		keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if catOutFile == "" {
			fmt.Fprintln(os.Stderr)
		} else {
			fmt.Println()
		}
		if err != nil {
			return fmt.Errorf("failed to read secret key: %w", err)
		}
		secretKey = strings.TrimSpace(string(keyBytes))
	}

	canonicalKey := config.ResolveCanonicalKey(targetName, targetNamespace, cfg.Targets)

	var destWriter io.Writer = os.Stdout
	if catOutFile != "" {
		f, err := os.Create(catOutFile)
		if err != nil {
			return fmt.Errorf("failed to create output file '%s': %w", catOutFile, err)
		}
		defer f.Close()
		destWriter = f
	}

	header, err := engine.CatFileFromArchive(ctx, prov, canonicalKey, filePath, secretKey, destWriter)
	if err != nil {
		return err
	}

	if catOutFile != "" {
		fmt.Printf("✔ Extracted '%s' (%s) to %s\n", header.Name, tui.FormatBytes(header.Size), catOutFile)
	}

	return nil
}
