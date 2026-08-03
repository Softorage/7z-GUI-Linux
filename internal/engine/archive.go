package engine

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
	"github.com/Softorage/7z-GUI-Linux/internal/sys"
)

// ExtractArchive is the centralized extraction function for any archive format.
// For double-compressed tarballs (e.g., .tar.gz), it pipelines two 7-Zip subprocesses in memory
// using an io.Pipe: uncompressing the outer wrapper to stdout, and streaming it directly into the inner tar extractor.
// Attaches an empty Reader to Stdin to prevent processes from hanging if an archive requires input.
func ExtractArchive(archivePath, destDir, password string, targets ...string) error {
	if sys.IsTarballExtension(archivePath) {
		// Decompress outer stream (e.g., gzip, bzip2, xz) to stdout (-so)
		args1 := []string{"x", archivePath, "-so", "-bso0", "-bsp0"}
		if password != "" {
			args1 = append(args1, "-p"+password)
		}
		cmd1 := exec.Command(Root7zCmd, args1...)
		cmd1.Stdin = strings.NewReader("")

		// Decompress TAR stream read from stdin (-si) into destDir
		args2 := append([]string{"x", "-si", "-ttar", "-o" + destDir, "-y"}, targets...)
		cmd2 := exec.Command(Root7zCmd, args2...)

		// io.Pipe connects writer (cmd1.Stdout) to reader (cmd2.Stdin) in memory without disk I/O
		pr, pw := io.Pipe()
		cmd1.Stdout = pw
		cmd2.Stdin = pr

		if err := cmd1.Start(); err != nil {
			pr.Close()
			pw.Close()
			return err
		}
		if err := cmd2.Start(); err != nil {
			pr.Close()
			pw.Close()
			return err
		}

		// Asynchronously wait for outer process completion and close pipe writer to send EOF to cmd2
		go func() {
			_ = cmd1.Wait()
			_ = pw.Close()
		}()

		err := cmd2.Wait()
		_ = pr.Close()
		return err
	}

	// Standard single-stage extraction for standard formats (.zip, .7z, .rar, etc.)
	args := append([]string{"x", archivePath, "-o" + destDir, "-y"}, targets...)
	if password != "" {
		args = append(args, "-p"+password)
	}
	cmd := exec.Command(Root7zCmd, args...)
	cmd.Stdin = strings.NewReader("")
	return cmd.Run()
}

// ExtractArchiveItems extracts clipboard entries situated inside virtual archives into temporary disk staging locations.
// Returns a mapping of original virtual paths to their temporary local disk paths, the temporary directory path itself, and any error encountered.
func ExtractArchiveItems(items []domain.ClipboardItem) (map[string]string, string, error) {
	hasArchiveItems := false
	for _, item := range items {
		if item.IsArchive {
			hasArchiveItems = true
			break
		}
	}
	if !hasArchiveItems {
		return nil, "", nil
	}

	tempDir, err := os.MkdirTemp("", "7gl-extract-*")
	if err != nil {
		return nil, "", err
	}

	// Group items by archive path and record passwords so we can batch extract with a single 7-Zip process per archive
	passwords := make(map[string]string)
	groups := make(map[string][]string)
	for _, item := range items {
		if item.IsArchive {
			groups[item.ArchivePath] = append(groups[item.ArchivePath], item.Path)
			if item.Password != "" {
				passwords[item.ArchivePath] = item.Password
			}
		}
	}

	for archivePath, paths := range groups {
		if err := ExtractArchive(archivePath, tempDir, passwords[archivePath], paths...); err != nil {
			os.RemoveAll(tempDir)
			return nil, "", fmt.Errorf("failed to extract from archive %s: %w", filepath.Base(archivePath), err)
		}
	}

	pathMap := make(map[string]string)
	for _, item := range items {
		if item.IsArchive {
			pathMap[item.Path] = filepath.Join(tempDir, filepath.FromSlash(item.Path))
		}
	}

	return pathMap, tempDir, nil
}

// ListArchive retrieves detailed metadata from an archive using 7-Zip's SLT flag `-slt`.
// Returns error explicitly if tarball pipeline decompression fails instead of falling back to outer Gzip header.
func ListArchive(archivePath, password string) ([]domain.ArchiveItem, bool, error) {
	var out []byte
	var err error

	if sys.IsTarballExtension(archivePath) {
		args1 := []string{"x", archivePath, "-so", "-bso0", "-bsp0"}
		if password != "" {
			args1 = append(args1, "-p"+password)
		}
		cmd1 := exec.Command(Root7zCmd, args1...)
		cmd1.Stdin = strings.NewReader("")
		cmd2 := exec.Command(Root7zCmd, "l", "-si", "-ttar", "-slt")

		pr, pw := io.Pipe()
		cmd1.Stdout = pw
		cmd2.Stdin = pr

		var outBuf strings.Builder
		cmd2.Stdout = &outBuf

		if err1 := cmd1.Start(); err1 == nil && cmd2.Start() == nil {
			go func() {
				_ = cmd1.Wait()
				_ = pw.Close()
			}()
			err = cmd2.Wait()
			_ = pr.Close()
			if err != nil {
				return nil, false, fmt.Errorf("failed to list tarball archive contents: %w", err)
			}
			return ParseSLTOutput(outBuf.String(), archivePath)
		} else {
			_ = pr.Close()
			_ = pw.Close()
			return nil, false, fmt.Errorf("failed to start tarball decompression pipeline")
		}
	}

	args := []string{"l", "-slt", archivePath}
	if password != "" {
		args = append(args, "-p"+password)
	}
	cmd := exec.Command(Root7zCmd, args...)
	cmd.Stdin = strings.NewReader("")
	out, err = cmd.Output()
	if err != nil {
		return nil, false, err
	}

	return ParseSLTOutput(string(out), archivePath)
}

// IsPasswordProtected tests if the archive requires a password for extraction.
func IsPasswordProtected(archive string) bool {
	// Execute '7z l' (List) with a dummy password. This is fast and will reveal
	// if the file is encrypted without extracting anything.
	cmd := exec.Command(Root7zCmd, "l", "-slt", archive, "-pDummyPassword_123456789")
	out, err := cmd.CombinedOutput()

	outStr := string(out)
	lowerOut := strings.ToLower(outStr)

	if err != nil {
		// If the header itself is encrypted, 7-zip will fail to list files
		// and output an error mentioning "wrong password" or "encrypted".
		if strings.Contains(lowerOut, "wrong password") ||
			strings.Contains(lowerOut, "encrypted archive") ||
			strings.Contains(lowerOut, "error in encrypted file") {
			return true
		}
	}

	// For archives where headers are NOT encrypted but the files inside are,
	// 7-zip will successfully list the contents. We check for the 'Encrypted = +' flag.
	if strings.Contains(outStr, "\nEncrypted = +") {
		return true
	}

	return false
}