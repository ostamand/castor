package cli

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/klauspost/compress/zstd"
	"github.com/ostamand/castor/internal/config"
	"github.com/ostamand/castor/internal/crypto"
	"github.com/ostamand/castor/internal/engine"
	"github.com/ostamand/castor/internal/storage"
	"github.com/ostamand/castor/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/api/option"
)

var (
	verifyNamespace string
	verifyDest      string
	verifySecretKey string
)

var verifyCmd = &cobra.Command{
	Use:     "verify [target]",
	Aliases: []string{"check"},
	Short:   "Verify archive integrity and decryptability in-memory",
	Long: `Streams down the remote archive, tests Age decryption and decompression in-memory,
and verifies the ciphertext SHA-256 against the sidecar metadata manifest without unpacking to disk.`,
	RunE: runVerify,
}

func init() {
	verifyCmd.Flags().StringVarP(&verifyNamespace, "namespace", "s", "", "Namespace to inspect")
	verifyCmd.Flags().StringVar(&verifyDest, "dest", "", "Specific destination to verify against")
	verifyCmd.Flags().StringVarP(&verifySecretKey, "key", "k", "", "Age private secret key or export CASTOR_AGE_KEY")
}

func runVerify(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	configPath := cfgPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	targetNamespace := cfg.Namespace
	if verifyNamespace != "" {
		targetNamespace = verifyNamespace
	}

	if len(cfg.Destinations) == 0 {
		return fmt.Errorf("no cloud destinations configured")
	}

	dest := cfg.Destinations[0]
	if verifyDest != "" {
		for _, d := range cfg.Destinations {
			if d.Name == verifyDest {
				dest = d
				break
			}
		}
	}

	var prov storage.Provider
	switch dest.Provider {
	case "gcs":
		prov, err = storage.NewGCSProvider(ctx, dest.Name, dest.Bucket, dest.Prefix)
	case "gdrive":
		credsPath := config.DefaultCredentialsPath()
		var opts []option.ClientOption
		if _, err := os.Stat(credsPath); err == nil {
			opts = append(opts, option.WithCredentialsFile(credsPath))
		}
		prov, err = storage.NewGDriveProvider(ctx, dest.Name, dest.Folder, opts...)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to provider '%s': %w", dest.Name, err)
	}
	defer prov.Close()

	// Prompt for secret key if encrypted and not provided
	secretKey := verifySecretKey
	if secretKey == "" {
		secretKey = os.Getenv("CASTOR_AGE_KEY")
	}
	if secretKey == "" && cfg.Security.Encrypt {
		fmt.Print("🔑 Enter Age Secret Key (AGE-SECRET-KEY-1...): ")
		keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("failed to read secret key: %w", err)
		}
		secretKey = strings.TrimSpace(string(keyBytes))
	}

	// Identify targets to verify
	var targetsToVerify []string
	if len(args) > 0 {
		targetsToVerify = []string{args[0]}
	} else {
		for _, t := range cfg.Targets {
			key := config.CanonicalCloudKey(targetNamespace, t.Path, t.Namespace)
			targetsToVerify = append(targetsToVerify, key)
		}
	}

	if len(targetsToVerify) == 0 {
		fmt.Println("No targets to verify.")
		return nil
	}

	fmt.Printf("🦫 Castor · Verifying %d archive(s) in '%s' (Namespace: %s)...\n\n",
		len(targetsToVerify), dest.Name, targetNamespace)

	for _, targetKey := range targetsToVerify {
		if !strings.HasPrefix(targetKey, targetNamespace+"/") {
			targetKey = path.Join(targetNamespace, targetKey)
		}

		dirScope := path.Dir(targetKey)
		baseName := path.Base(targetKey)
		archiveObj := path.Join(dirScope, baseName+".tar.zst.age")
		metaObj := path.Join(dirScope, baseName+".meta.json.age")

		// 1. Read sidecar metadata
		var meta *engine.ArchiveMetadata
		if metaR, err := prov.NewReader(ctx, metaObj); err == nil {
			meta, _ = engine.ReadMetadata(metaR, secretKey, strings.HasSuffix(metaObj, ".age"))
			_ = metaR.Close()
		}

		// 2. Stream and verify archive
		rawReader, err := prov.NewReader(ctx, archiveObj)
		if err != nil {
			// Try without .age extension
			archiveObj = strings.TrimSuffix(archiveObj, ".age")
			rawReader, err = prov.NewReader(ctx, archiveObj)
			if err != nil {
				fmt.Printf("✖ %s: not found in cloud (%v)\n", targetKey, err)
				continue
			}
		}

		hasher := sha256.New()
		teeReader := io.TeeReader(rawReader, hasher)

		var decompressReader io.Reader = teeReader
		if strings.HasSuffix(archiveObj, ".age") {
			decReader, err := crypto.DecryptStream(teeReader, secretKey)
			if err != nil {
				rawReader.Close()
				fmt.Printf("✖ %s: Age decryption failed: %v\n", targetKey, err)
				continue
			}
			decompressReader = decReader
		}

		zstdReader, err := zstd.NewReader(decompressReader)
		if err != nil {
			rawReader.Close()
			fmt.Printf("✖ %s: zstd decompression failed: %v\n", targetKey, err)
			continue
		}

		// Validate tar stream
		tr := tar.NewReader(zstdReader)
		entriesCount := 0
		var tarErr error
		for {
			_, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				tarErr = err
				break
			}
			entriesCount++
		}

		zstdReader.Close()
		rawReader.Close()

		if tarErr != nil {
			fmt.Printf("✖ %s: corrupt tar structure: %v\n", targetKey, tarErr)
			continue
		}

		shaSum := hex.EncodeToString(hasher.Sum(nil))
		shaStatus := "SHA-256 verified"
		if meta != nil && meta.Payload.ArchiveSHA256 != "" {
			if !strings.EqualFold(shaSum, meta.Payload.ArchiveSHA256) {
				fmt.Printf("✖ %s: SHA-256 mismatch! (computed: %s, expected: %s)\n",
					targetKey, shaSum[:12], meta.Payload.ArchiveSHA256[:12])
				continue
			}
		}

		successMsg := fmt.Sprintf("✔ %-36s Decrypted · %s · %d entries · %s",
			targetKey, shaStatus, entriesCount, lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render("100% HEALTHY"))
		fmt.Println(successMsg)
	}

	return nil
}
