// Package unzip safely extracts ZIP archives, including nested ZIP files.
package unzip

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ExistingDirPolicy determines how an already existing extraction directory is handled.
type ExistingDirPolicy string

const (
	// Overwrite extracts into the existing directory and replaces files with matching names.
	Overwrite ExistingDirPolicy = "overwrite"
	// Skip leaves an existing extraction directory untouched.
	Skip ExistingDirPolicy = "skip"
	// Rename creates a distinct directory by appending " (2)", " (3)", and so on.
	Rename ExistingDirPolicy = "rename"
)

// Options controls the extraction process.
type Options struct {
	DeleteZIP bool
	Policy    ExistingDirPolicy
	Verbose   bool
	Logger    *log.Logger
}

// Result is the outcome for one root archive supplied by a caller.
type Result struct {
	Archive        string
	Destination    string
	ExtractedZIPs  int
	ExtractedFiles int
	Skipped        bool
}

// BatchResult is the aggregate outcome of processing requested files.
type BatchResult struct {
	Results []Result
	Success int
	Failed  int
}

// ProcessFiles recursively extracts ZIP files from the supplied paths. Non-ZIP paths are ignored.
// Processing continues after a failed root archive.
func ProcessFiles(paths []string, options Options) BatchResult {
	options = normalizedOptions(options)
	batch := BatchResult{}

	for _, path := range paths {
		if !isZIPPath(path) {
			continue
		}

		result, err := processArchive(path, options)
		if err != nil {
			options.Logger.Printf("[ERROR] %s: %v", path, err)
			batch.Failed++
			continue
		}

		batch.Results = append(batch.Results, result)
		batch.Success++
	}
	return batch
}

func normalizedOptions(options Options) Options {
	if options.Policy == "" {
		options.Policy = Overwrite
	}
	if options.Logger == nil {
		options.Logger = log.New(io.Discard, "", 0)
	}
	return options
}

func processArchive(archivePath string, options Options) (Result, error) {
	archivePath = filepath.Clean(archivePath)
	if !isZIPPath(archivePath) {
		return Result{}, fmt.Errorf("ZIPファイルではありません")
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		return Result{}, err
	}
	if info.IsDir() {
		return Result{}, fmt.Errorf("ZIPファイルではありません")
	}

	options.Logger.Printf("[START] %s", archivePath)
	destination, skipped, err := destinationFor(archivePath, options.Policy)
	if err != nil {
		return Result{}, err
	}
	result := Result{Archive: archivePath, Destination: destination, Skipped: skipped}
	if skipped {
		options.Logger.Printf("[SKIP] %s -> %s (展開先フォルダが既に存在します)", filepath.Base(archivePath), destination)
		options.Logger.Printf("[DONE] %s", archivePath)
		return result, nil
	}

	if err := extractOne(archivePath, destination, options); err != nil {
		return Result{}, err
	}
	result.ExtractedZIPs++

	if options.DeleteZIP {
		if err := os.Remove(archivePath); err != nil {
			return Result{}, fmt.Errorf("展開後のZIP削除に失敗しました: %w", err)
		}
		options.Logger.Printf("[DELETE] %s", archivePath)
	}

	if err := walkAndExtractNested(destination, &result, options); err != nil {
		return Result{}, err
	}
	files, err := countRegularFiles(destination)
	if err != nil {
		return Result{}, err
	}
	result.ExtractedFiles = files
	options.Logger.Printf("[DONE] %s", archivePath)
	return result, nil
}

func destinationFor(archivePath string, policy ExistingDirPolicy) (string, bool, error) {
	base := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
	if base == "" || base == "." {
		return "", false, fmt.Errorf("展開先フォルダ名を決定できません")
	}
	candidate := filepath.Join(filepath.Dir(archivePath), base)

	info, err := os.Stat(candidate)
	if err == nil {
		if !info.IsDir() {
			return "", false, fmt.Errorf("展開先パスがファイルとして存在します: %s", candidate)
		}
		switch policy {
		case Overwrite:
			return candidate, false, nil
		case Skip:
			return candidate, true, nil
		case Rename:
			for n := 2; ; n++ {
				renamed := fmt.Sprintf("%s (%d)", candidate, n)
				if _, statErr := os.Stat(renamed); errors.Is(statErr, os.ErrNotExist) {
					return renamed, false, nil
				} else if statErr != nil {
					return "", false, statErr
				}
			}
		default:
			return "", false, fmt.Errorf("不正な既存フォルダ処理モードです: %s", policy)
		}
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	return candidate, false, nil
}

func walkAndExtractNested(root string, result *Result, options Options) error {
	processed := make(map[string]struct{})
	var extractionErrors []error

	for {
		archives, err := findNestedZIPs(root)
		if err != nil {
			return err
		}
		processedInPass := 0

		for _, archivePath := range archives {
			key, err := filepath.Abs(archivePath)
			if err != nil {
				return err
			}
			if _, alreadyProcessed := processed[key]; alreadyProcessed {
				continue
			}
			processed[key] = struct{}{}
			processedInPass++

			destination, _, err := destinationFor(archivePath, Overwrite)
			if err != nil {
				extractionErrors = append(extractionErrors, fmt.Errorf("内部ZIP %s の展開先を作成できません: %w", archivePath, err))
				continue
			}
			if err := extractOne(archivePath, destination, options); err != nil {
				extractionErrors = append(extractionErrors, fmt.Errorf("内部ZIP %s の展開に失敗しました: %w", archivePath, err))
				continue
			}
			result.ExtractedZIPs++
			if options.DeleteZIP {
				if err := os.Remove(archivePath); err != nil {
					extractionErrors = append(extractionErrors, fmt.Errorf("内部ZIP %s の削除に失敗しました: %w", archivePath, err))
					continue
				}
				options.Logger.Printf("[DELETE] %s", archivePath)
			}
		}

		if processedInPass == 0 {
			break
		}
	}
	if len(extractionErrors) > 0 {
		return fmt.Errorf("内部ZIPの処理中に%d件のエラー: %w", len(extractionErrors), errors.Join(extractionErrors...))
	}
	return nil
}

func findNestedZIPs(root string) ([]string, error) {
	archives := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && isZIPPath(path) {
			archives = append(archives, path)
		}
		return nil
	})
	return archives, err
}

func extractOne(archivePath, destination string, options Options) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	if err := ensureSafeDirectory(destination, destination); err != nil {
		return fmt.Errorf("安全でない展開先フォルダ: %w", err)
	}
	options.Logger.Printf("[EXTRACT] %s -> %s", filepath.Base(archivePath), destination)

	for _, file := range reader.File {
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("シンボリックリンクのZIPエントリは許可されません: %q", file.Name)
		}
		outputPath, err := secureOutputPath(destination, file.Name)
		if err != nil {
			return fmt.Errorf("危険なZIPエントリを拒否しました %q: %w", file.Name, err)
		}

		if file.FileInfo().IsDir() {
			if err := ensureSafeDirectory(destination, outputPath); err != nil {
				return err
			}
			continue
		}

		if err := ensureSafeDirectory(destination, filepath.Dir(outputPath)); err != nil {
			return err
		}
		if info, err := os.Lstat(outputPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("シンボリックリンクへの上書きは許可されません: %s", outputPath)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := extractFile(file, outputPath); err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}
		if options.Verbose {
			options.Logger.Printf("[FILE] %s", outputPath)
		}
	}
	return nil
}

// ensureSafeDirectory creates directory if needed and rejects a symbolic link in any
// path component below root. This prevents a ZIP entry from escaping through an
// attacker-controlled preexisting link in an overwrite destination.
func ensureSafeDirectory(root, directory string) error {
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("展開先フォルダ外のディレクトリは許可されません")
	}

	current := root
	components := []string{}
	if relative != "." {
		components = strings.Split(relative, string(os.PathSeparator))
	}
	for index := -1; index < len(components); index++ {
		if index >= 0 {
			current = filepath.Join(current, components[index])
		}
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("シンボリックリンクは許可されません: %s", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("ディレクトリではないパスが存在します: %s", current)
		}
	}
	return nil
}

func extractFile(file *zip.File, outputPath string) error {
	input, err := file.Open()
	if err != nil {
		return err
	}
	defer input.Close()

	mode := file.Mode()
	if mode == 0 {
		mode = 0o644
	}
	output, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// secureOutputPath rejects absolute paths and paths that would escape destination (Zip Slip).
func secureOutputPath(destination, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("空のエントリ名です")
	}
	// ZIP stores paths with '/', but reject both separator styles to be safe on Windows.
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || filepath.IsAbs(name) {
		return "", fmt.Errorf("絶対パスは許可されません")
	}
	if len(name) >= 2 && name[1] == ':' {
		return "", fmt.Errorf("ドライブ指定は許可されません")
	}

	cleanName := filepath.Clean(filepath.FromSlash(name))
	if cleanName == "." || cleanName == string(os.PathSeparator) {
		return "", fmt.Errorf("不正なエントリ名です")
	}
	candidate := filepath.Join(destination, cleanName)
	relative, err := filepath.Rel(destination, candidate)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("展開先フォルダ外への書き込みは許可されません")
	}
	return candidate, nil
}

func countRegularFiles(root string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && !isZIPPath(entry.Name()) {
			count++
		}
		return nil
	})
	return count, err
}

func isZIPPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".zip")
}
