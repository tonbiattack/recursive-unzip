// Package unzip は、ネストされたZIPを含めて安全に展開する処理を提供する。
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

// ExistingDirPolicy は、展開先フォルダが既に存在する場合の扱いを表す。
type ExistingDirPolicy string

const (
	// Overwrite は既存フォルダを使い、同名ファイルをZIPの内容で置き換える。
	Overwrite ExistingDirPolicy = "overwrite"
	// Skip は既存フォルダに触れず、その最上位ZIPの処理を成功扱いで省略する。
	Skip ExistingDirPolicy = "skip"
	// Rename は " (2)"、" (3)" を付けた未使用の別名フォルダを作る。
	Rename ExistingDirPolicy = "rename"
)

// Options は展開処理の安全設定と出力方法をまとめる。
type Options struct {
	// DeleteZIP は、展開に成功したZIPを削除する明示的な指定である。
	DeleteZIP bool
	// Policy は既存の展開先フォルダに対する扱いである。
	Policy ExistingDirPolicy
	// Verbose は、展開された各ファイルのパスもログ出力するかを示す。
	Verbose bool
	// Logger は進捗とエラーを出力する先であり、nilの場合は出力を捨てる。
	Logger *log.Logger
}

// Result は、呼び出し元が指定した最上位ZIP 1件の処理結果を表す。
type Result struct {
	// Archive は処理した最上位ZIPのパスである。
	Archive string
	// Destination は最上位ZIPの展開先フォルダである。
	Destination string
	// ExtractedZIPs は最上位ZIPを含め、実際に展開できたZIPの数である。
	ExtractedZIPs int
	// ExtractedFiles は展開先に残る通常ファイルの数であり、ZIP自体は数えない。
	ExtractedFiles int
	// Skipped は既存フォルダのスキップ設定により展開を行わなかったことを示す。
	Skipped bool
}

// BatchResult は、複数の最上位ZIPを処理した全体の集計結果を表す。
type BatchResult struct {
	// Results には成功またはスキップした最上位ZIPの詳細を格納する。
	Results []Result
	// Success は成功またはスキップとして完了した最上位ZIPの数である。
	Success int
	// Failed は展開先の決定や展開に失敗した最上位ZIPの数である。
	Failed int
}

// ProcessFiles は指定パスからZIPだけを選び、1件失敗しても後続の最上位ZIPを続けて処理する。
func ProcessFiles(paths []string, options Options) BatchResult {
	// 呼び出し元が省略した設定を安全な既定値で補う。
	options = normalizedOptions(options)
	batch := BatchResult{}

	for _, path := range paths {
		// ExplorerからZIP以外が渡ってもエラーにせず、対象外として無視する。
		if !isZIPPath(path) {
			continue
		}

		result, err := processArchive(path, options)
		if err != nil {
			// 最上位ZIP単位で失敗を隔離し、残りのZIPの処理を妨げない。
			options.Logger.Printf("[ERROR] %s: %v", path, err)
			batch.Failed++
			continue
		}

		batch.Results = append(batch.Results, result)
		batch.Success++
	}
	return batch
}

// normalizedOptions は、公開APIに渡された任意のOptionsを処理可能な状態へ正規化する。
func normalizedOptions(options Options) Options {
	if options.Policy == "" {
		// 意図しない削除や別名作成を避けるため、仕様どおり既定は上書きとする。
		options.Policy = Overwrite
	}
	if options.Logger == nil {
		// ライブラリ利用時にログ出力を強制しないため、nilは破棄用Loggerに置き換える。
		options.Logger = log.New(io.Discard, "", 0)
	}
	return options
}

// processArchive は1個の最上位ZIPを展開し、内部ZIPの再帰展開まで完了させる。
func processArchive(archivePath string, options Options) (Result, error) {
	// 相対パスに含まれる冗長な区切りを取り除き、ログと展開先の基準を統一する。
	archivePath = filepath.Clean(archivePath)
	if !isZIPPath(archivePath) {
		return Result{}, fmt.Errorf("ZIPファイルではありません")
	}

	// 存在しないパスやディレクトリ指定を、ZIPとして開く前に弾く。
	info, err := os.Stat(archivePath)
	if err != nil {
		return Result{}, err
	}
	if info.IsDir() {
		return Result{}, fmt.Errorf("ZIPファイルではありません")
	}

	options.Logger.Printf("[START] %s", archivePath)
	// 最上位ZIPにだけ、CLIで選択した既存フォルダポリシーを適用する。
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
	// 最上位ZIP自身も展開したZIPとして集計する。
	result.ExtractedZIPs++

	if options.DeleteZIP {
		// 削除は展開成功後に限る。展開に失敗した元データは必ず残す。
		if err := os.Remove(archivePath); err != nil {
			return Result{}, fmt.Errorf("展開後のZIP削除に失敗しました: %w", err)
		}
		options.Logger.Printf("[DELETE] %s", archivePath)
	}

	// 展開で新しく現れるZIPを見逃さないよう、反復探索で内部ZIPを処理する。
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

// destinationFor は、ZIPと同じ親フォルダに作る展開先と、既存フォルダ時の扱いを決定する。
func destinationFor(archivePath string, policy ExistingDirPolicy) (string, bool, error) {
	// "images.zip" は "images" へ展開する。大文字拡張子もfilepath.Extで扱える。
	base := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
	if base == "" || base == "." {
		return "", false, fmt.Errorf("展開先フォルダ名を決定できません")
	}
	candidate := filepath.Join(filepath.Dir(archivePath), base)

	info, err := os.Stat(candidate)
	if err == nil {
		// 同名の通常ファイルをフォルダとして扱うと破壊的なので、明示的に失敗させる。
		if !info.IsDir() {
			return "", false, fmt.Errorf("展開先パスがファイルとして存在します: %s", candidate)
		}
		switch policy {
		case Overwrite:
			return candidate, false, nil
		case Skip:
			return candidate, true, nil
		case Rename:
			// 既存の名前を壊さず、最初に空いている連番フォルダを選ぶ。
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
	// 存在しない以外のStat失敗（権限不足など）は、空きパスと誤認しない。
	if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	return candidate, false, nil
}

// walkAndExtractNested は、展開先に現れた未処理のZIPがなくなるまで探索と展開を繰り返す。
func walkAndExtractNested(root string, result *Result, options Options) error {
	// 同じ内部ZIPを次の探索周回で再び展開しないため、絶対パスで記録する。
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

			// 内部ZIPは親の展開結果の一部なので、常に既定の上書き展開とする。
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
				// 内部ZIPも、展開成功したものだけを削除対象にする。
				if err := os.Remove(archivePath); err != nil {
					extractionErrors = append(extractionErrors, fmt.Errorf("内部ZIP %s の削除に失敗しました: %w", archivePath, err))
					continue
				}
				options.Logger.Printf("[DELETE] %s", archivePath)
			}
		}

		// 新規ZIPを1件も処理しなかった時点で、再帰探索は収束したと判断する。
		if processedInPass == 0 {
			break
		}
	}
	if len(extractionErrors) > 0 {
		// すべての内部ZIPを試行した後に、複数の失敗原因をまとめて最上位へ返す。
		return fmt.Errorf("内部ZIPの処理中に%d件のエラー: %w", len(extractionErrors), errors.Join(extractionErrors...))
	}
	return nil
}

// findNestedZIPs は、現在の展開先ツリーから通常ファイルとして存在するZIPだけを列挙する。
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

// extractOne は1個のZIPを展開先へストリームコピーし、各エントリの安全性を確認する。
func extractOne(archivePath, destination string, options Options) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	// 展開先自体が既存のシンボリックリンクでないことを最初に確認する。
	if err := ensureSafeDirectory(destination, destination); err != nil {
		return fmt.Errorf("安全でない展開先フォルダ: %w", err)
	}
	options.Logger.Printf("[EXTRACT] %s -> %s", filepath.Base(archivePath), destination)

	for _, file := range reader.File {
		// ZIP内のリンクは、展開後に外部のパスを指し得るため作成しない。
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("シンボリックリンクのZIPエントリは許可されません: %q", file.Name)
		}
		outputPath, err := secureOutputPath(destination, file.Name)
		if err != nil {
			return fmt.Errorf("危険なZIPエントリを拒否しました %q: %w", file.Name, err)
		}

		if file.FileInfo().IsDir() {
			// ディレクトリエントリも、途中にリンクがないことを確認してから作成する。
			if err := ensureSafeDirectory(destination, outputPath); err != nil {
				return err
			}
			continue
		}

		// 親ディレクトリを安全に用意し、既存ファイルがリンクなら上書きを拒否する。
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

// ensureSafeDirectory は必要なディレクトリを作り、root配下の途中成分にシンボリックリンクがないことを保証する。
func ensureSafeDirectory(root, directory string) error {
	relative, err := filepath.Rel(root, directory)
	if err != nil {
		return err
	}
	// root外のディレクトリを作る呼び出しは、内部バグまたは経路逸脱として拒否する。
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
		// Lstatを使うことで、リンク先ではなくリンクそのもののモードを検査する。
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

// extractFile はZIPエントリの内容を全メモリへ読み込まず、出力ファイルへストリームコピーする。
func extractFile(file *zip.File, outputPath string) error {
	input, err := file.Open()
	if err != nil {
		return err
	}
	defer input.Close()

	// ZIPに権限情報がない場合は、通常ファイルとして安全な既定の0644を使う。
	mode := file.Mode()
	if mode == 0 {
		mode = 0o644
	}
	// O_TRUNCは上書きモードの同名ファイルをZIPの内容で置き換えるために必要である。
	output, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	// Closeの失敗も書き込み失敗として扱うため、Copyの後でも必ず検査する。
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// secureOutputPath は、Zip Slipを防ぐために絶対パスと展開先外へ出る相対パスを拒否する。
func secureOutputPath(destination, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("空のエントリ名です")
	}
	// ZIPの標準区切りは'/'だが、Windows向けには'\\'を含む不正な経路も同じ基準で検査する。
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || filepath.IsAbs(name) {
		return "", fmt.Errorf("絶対パスは許可されません")
	}
	// "C:" のようなドライブ指定は、Windowsで展開先を無視する可能性があるため明示的に拒否する。
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
	// Relの結果が".."から始まる場合は、正規化後も展開先の外へ出ている。
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("展開先フォルダ外への書き込みは許可されません")
	}
	return candidate, nil
}

// countRegularFiles は展開結果の表示用に、ZIP以外の通常ファイル数を数える。
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

// isZIPPath は拡張子の大文字小文字に関係なく、ZIPファイル名かを判定する。
func isZIPPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".zip")
}
