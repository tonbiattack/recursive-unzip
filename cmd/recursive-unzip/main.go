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

// usageText は、Explorer連携ではなくCLIから直接実行する場合の使用方法を示す。
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
	// ZIP削除はデータ消失につながるため、明示指定された場合だけ有効にする。
	deleteZIP := flag.Bool("delete-zip", false, "展開に成功したZIPを削除する")
	// 既存フォルダの扱いは相互排他的な選択肢として後で検証する。
	overwrite := flag.Bool("overwrite", false, "既存の展開先に上書きする（既定）")
	skipExisting := flag.Bool("skip-existing", false, "既存の展開先をスキップする")
	renameExisting := flag.Bool("rename-existing", false, "既存の展開先があれば別名フォルダを作る")
	verbose := flag.Bool("verbose", false, "詳細ログを表示する")
	// Explorerを開かないモードは、バッチ処理や自動テストで使う。
	noOpen := flag.Bool("no-open", false, "完了後にExplorerを開かない")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usageText)
	}
	flag.Parse()

	// 複数の既存フォルダ処理モードが指定された場合は、曖昧な動作を避けて終了する。
	policy, err := selectedPolicy(*overwrite, *skipExisting, *renameExisting)
	if err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		flag.Usage()
		os.Exit(2)
	}

	paths := flag.Args()
	// ZIP以外は中核処理で無視するが、ZIPが0件なら操作ミスとして分かりやすく通知する。
	if countZIPPaths(paths) == 0 {
		fmt.Println("ZIPファイルが選択されていません。")
		os.Exit(1)
	}

	// プレフィックスや時刻を付けず、中核処理が定義する [START] 等の形式をそのまま表示する。
	logger := log.New(os.Stdout, "", 0)
	batch := unzip.ProcessFiles(paths, unzip.Options{
		DeleteZIP: *deleteZIP,
		Policy:    policy,
		Verbose:   *verbose,
		Logger:    logger,
	})

	// 個別ログに加えて、呼び出し元が成否を一目で判断できる集計を最後に出力する。
	fmt.Println("完了")
	fmt.Printf("成功: %d\n", batch.Success)
	fmt.Printf("失敗: %d\n", batch.Failed)

	// 少なくとも1件成功した場合だけ、利用者が結果を確認しやすいようExplorerを開く。
	if batch.Success > 0 && !*noOpen {
		if err := openResult(batch.Results); err != nil {
			// 展開結果そのものは成功しているため、Explorer起動失敗は警告として扱う。
			fmt.Fprintln(os.Stderr, "Explorerを開けませんでした:", err)
		}
	}
	// 1件でも失敗があれば、スクリプトやCIから検出できるよう終了コードを非0にする。
	if batch.Failed > 0 {
		os.Exit(1)
	}
}

// selectedPolicy は既存フォルダに対する3つのCLI指定を、内部パッケージの列挙値へ変換する。
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
	// --overwrite を省略しても、安全仕様として定めた既定の上書きモードを返す。
	return unzip.Overwrite, nil
}

// countZIPPaths は拡張子を大文字小文字を区別せず数え、Explorerから混在して渡った項目を判定する。
func countZIPPaths(paths []string) int {
	count := 0
	for _, path := range paths {
		if strings.EqualFold(filepath.Ext(path), ".zip") {
			count++
		}
	}
	return count
}

// openResult はWindowsでだけExplorerを起動する。1件なら展開先、複数なら共通の親フォルダを開く。
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
