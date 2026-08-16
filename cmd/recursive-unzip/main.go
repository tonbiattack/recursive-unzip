package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tonbiattack/recursive-unzip/internal/unzip"
)

const usageText = `使用方法:
  recursive-unzip.exe [options] <zip...>

選択したZIPを展開し、内部のZIPも見つからなくなるまで再帰的に展開します。
ZIP以外の引数は無視します。

オプション:
  --delete-zip              展開に成功したZIPを削除します（既定: 残す）
  --overwrite               既存の展開先に上書きします（既定）
  --skip-existing           既存の展開先があれば、そのZIPを処理しません
  --rename-existing         既存の展開先があれば " (2)" 等を付けた別名フォルダを作ります
  --verbose                 展開する各ファイルを表示します
  --no-open                 完了後にExplorerを開きません
  --help                    このヘルプを表示します
`

func main() {
	deleteZIP := flag.Bool("delete-zip", false, "展開に成功したZIPを削除する")
	overwrite := flag.Bool("overwrite", false, "既存の展開先に上書きする（既定）")
	skipExisting := flag.Bool("skip-existing", false, "既存の展開先をスキップする")
	renameExisting := flag.Bool("rename-existing", false, "既存の展開先があれば別名フォルダを作る")
	verbose := flag.Bool("verbose", false, "詳細ログを表示する")
	noOpen := flag.Bool("no-open", false, "完了後にExplorerを開かない")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usageText)
	}
	flag.Parse()

	policy, err := selectedPolicy(*overwrite, *skipExisting, *renameExisting)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		flag.Usage()
		os.Exit(2)
	}

	paths := flag.Args()
	if countZIPPaths(paths) == 0 {
		fmt.Println("ZIPファイルが選択されていません。")
		os.Exit(1)
	}

	logger := log.New(os.Stdout, "", 0)
	batch := unzip.ProcessFiles(paths, unzip.Options{
		DeleteZIP: *deleteZIP,
		Policy:    policy,
		Verbose:   *verbose,
		Logger:    logger,
	})

	fmt.Println("完了")
	fmt.Printf("成功: %d\n", batch.Success)
	fmt.Printf("失敗: %d\n", batch.Failed)

	if batch.Success > 0 && !*noOpen {
		if err := openResult(batch.Results); err != nil {
			fmt.Fprintln(os.Stderr, "Explorerを開けませんでした:", err)
		}
	}
	if batch.Failed > 0 {
		os.Exit(1)
	}
}

func selectedPolicy(overwrite, skipExisting, renameExisting bool) (unzip.ExistingDirPolicy, error) {
	selected := 0
	if overwrite {
		selected++
	}
	if skipExisting {
		selected++
	}
	if renameExisting {
		selected++
	}
	if selected > 1 {
		return "", fmt.Errorf("--overwrite、--skip-existing、--rename-existing は同時に指定できません")
	}
	if skipExisting {
		return unzip.Skip, nil
	}
	if renameExisting {
		return unzip.Rename, nil
	}
	return unzip.Overwrite, nil
}

func countZIPPaths(paths []string) int {
	count := 0
	for _, path := range paths {
		if strings.EqualFold(filepath.Ext(path), ".zip") {
			count++
		}
	}
	return count
}

func openResult(results []unzip.Result) error {
	if len(results) == 0 || runtime.GOOS != "windows" {
		return nil
	}
	toOpen := results[0].Destination
	if len(results) > 1 {
		toOpen = filepath.Dir(results[0].Archive)
	}
	return exec.Command("explorer.exe", toOpen).Start()
}
