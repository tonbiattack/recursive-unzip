# recursive-unzip

**recursive-unzip** は、Windows 11 のExplorerで選択したZIPファイルを展開し、展開後に見つかった内部ZIPもなくなるまで再帰的に展開する、外部依存のないGo製CLIツールです。元のZIPファイルは既定では削除しません。

> **設計方針:** ZIPを選択して右クリックし、`ZIPを再帰展開` を実行するだけで、ネストされたZIPを意識せず展開を完了できます。ZIP以外の選択項目は無視します。

|項目|内容|
|---|---|
|対象環境|Windows 11（x86-64）|
|配布形式|単一の `recursive-unzip.exe`|
|主な入力|1個以上の `.zip` ファイルパス|
|標準の出力先|各ZIPと同じフォルダ内の、拡張子を除いたフォルダ|
|既定の競合動作|既存フォルダに上書き展開|
|外部サービス・通信|不要|
|元ZIP|既定では保持。`--delete-zip` 指定時のみ、展開成功後に削除|

## 機能

このツールは最初に指定されたZIPだけを処理対象とし、ZIP内にあるZIPは同じ規則で展開します。たとえば `images.zip` の内部に `sub.zip`、さらにその内部に `inner.zip` があるとき、最終的に `images/sub/inner/` まで作成します。最上位ZIPと内部ZIPはいずれも既定では残ります。

|機能|実装内容|
|---|---|
|再帰展開|ネスト階層に固定上限を設けず、未処理の内部ZIPがなくなるまで反復処理します。|
|複数選択|CLIに渡された複数ZIPを順に処理します。あるZIPに失敗しても、後続の最上位ZIPを処理します。|
|ZIP以外|`.zip` 以外の引数は無視します。ZIPが0件なら日本語メッセージを表示して終了します。|
|Zip Slip対策|絶対パス、ドライブ指定、展開先から外へ出る `..` を含む危険なエントリを拒否します。|
|日本語・空白パス|GoのUnicode文字列と標準ライブラリのパス処理で扱います。|
|既存フォルダ|既定の上書きに加え、スキップまたは別名フォルダ作成を選べます。|
|完了後の表示|Windows上では、1件成功時は展開先、複数成功時は元ZIPの親フォルダをExplorerで開きます。|

## 利用方法

### CLI

PowerShellまたはコマンドプロンプトから、ZIPのフルパスまたは相対パスを指定します。空白を含むパスは必ず引用符で囲んでください。

```powershell
.\recursive-unzip.exe "C:\Users\your-name\Downloads\images.zip"
```

複数のZIPを処理する場合は、並べて指定します。テキストファイルなどが混ざっていても無視されます。

```powershell
.\recursive-unzip.exe a.zip b.zip c.txt
```

実行ログは標準出力へ出力されます。

```text
[START] C:\Users\your-name\Downloads\images.zip
[EXTRACT] images.zip -> C:\Users\your-name\Downloads\images
[EXTRACT] sub.zip -> C:\Users\your-name\Downloads\images\sub
[EXTRACT] inner.zip -> C:\Users\your-name\Downloads\images\sub\inner
[DONE] C:\Users\your-name\Downloads\images.zip
完了
成功: 1
失敗: 0
```

### オプション

|オプション|説明|
|---|---|
|`--delete-zip`|各ZIPの展開に成功した後、そのZIPを削除します。既定では削除しません。|
|`--overwrite`|既存の展開先フォルダへ上書きします。既定の動作です。|
|`--skip-existing`|展開先フォルダが既にある場合、その最上位ZIPをスキップします。|
|`--rename-existing`|展開先フォルダが既にある場合、`name (2)`、`name (3)` のような別名フォルダへ展開します。|
|`--verbose`|展開する個々のファイルパスもログに表示します。|
|`--no-open`|処理完了後にExplorerを開きません。自動処理・テスト時に便利です。|
|`--help`|使用方法を表示します。|

`--overwrite`、`--skip-existing`、`--rename-existing` は排他的です。複数同時に指定すると、使用方法を表示して終了します。

## Explorer右クリックメニューへの登録

Windows Shellでは、ファイル種別の `shell` 配下に静的なverbとcommandを登録することで、Explorerの右クリックメニューへコマンドを追加できます。[1] 本プロジェクトでは、管理者権限を要求せず現在のユーザーに限定するため、`HKCU\Software\Classes` に登録します。

まず、`recursive-unzip.exe` と `scripts\Install-ExplorerMenu.ps1` を同じ配布フォルダに置きます。次にPowerShellを開き、次を実行してください。

```powershell
Set-ExecutionPolicy -Scope Process Bypass -Force
.\scripts\Install-ExplorerMenu.ps1 -ExecutablePath "C:\Tools\recursive-unzip\recursive-unzip.exe"
```

登録後はExplorerで1個以上のZIPを選び、右クリックして **ZIPを再帰展開** を選択します。Windows 11では、レジストリベースの静的verbが **その他のオプションを表示** のサブメニューに現れる場合があります。コマンド文字列では実行ファイルと`%1`を引用符で囲んでおり、空白を含むパスにも対応します。Windows Shellのcommand文字列では、スペースを含む可能性がある実行ファイル・引数を引用符で囲むことが推奨されています。[2]

> 複数選択には、複数項目を扱うverb用の `MultiSelectModel=Player` を登録します。[3] レジストリベースの静的verbはWindows Shellの既定上限により、1回に100項目を超える選択では表示されない場合があります。[3] その場合はCLIから複数ZIPを直接指定してください。

登録を解除するには、次を実行します。

```powershell
.\scripts\Uninstall-ExplorerMenu.ps1
```

## ビルド

Go 1.22以降を用意して、プロジェクトのルートで実行します。

```powershell
# Windows上
 go build -trimpath -ldflags="-s -w" -o recursive-unzip.exe .\cmd\recursive-unzip

# Linux/macOSからWindows x86-64向けにクロスビルド
$env:GOOS="windows"; $env:GOARCH="amd64"
go build -trimpath -ldflags="-s -w" -o recursive-unzip.exe .\cmd\recursive-unzip
```

Windows以外の環境でも展開処理自体はテストできますが、Explorerの起動はWindows上でのみ実行されます。

## テスト

自動テストは外部ファイルやネットワークを使用しません。プロジェクトルートで次を実行します。

```powershell
go test ./... -count=1
```

|検証項目|対象テスト|
|---|---|
|通常のZIP|ファイル・サブフォルダの展開|
|ZIP内ZIP|2階層・3階層の再帰展開|
|複数入力|複数ZIPを処理し、ZIP以外を無視|
|障害継続|壊れたZIPがあっても後続ZIPを処理|
|既存フォルダ|上書き・スキップ・別名フォルダ|
|安全性|`../`、Windowsドライブ指定、バックスラッシュ経路のZip Slip、ZIP内・既存展開先のシンボリックリンクを拒否|
|パス互換性|日本語名、空白を含むZIP名・フォルダ名|
|境界条件|空ZIP、`--delete-zip`|

## セキュリティと制約

ZIPのエントリパスは展開先の相対パスとして正規化し、`filepath.Rel` によって展開先フォルダの外へ出ないことを確認してから書き込みます。ZIP内のシンボリックリンク、および既に存在する展開先内のシンボリックリンクを経由する書き込みも拒否します。危険なエントリを1件でも含むZIPは失敗として扱い、外部への書き込みは行いません。

この初期版は標準ライブラリのZIP形式のみを対象とします。パスワード付きZIP、7z、RAR、GUI、進捗バー、ドラッグ&ドロップは実装対象外です。また、ZIP bombのような過大な展開結果を抑止するサイズ上限は設けていません。信頼できない配布元の大容量ZIPは、十分な空き容量がある隔離された場所で扱ってください。

## ライセンス

現時点ではライセンスは未指定です。配布・公開前に利用方針に合うライセンスを追加してください。

## 参考資料

[1]: https://learn.microsoft.com/en-us/windows/win32/shell/context-menu-handlers "Creating Shortcut Menu Handlers | Microsoft Learn"
[2]: https://learn.microsoft.com/en-us/windows/win32/shell/context "Extending Shortcut Menus | Microsoft Learn"
[3]: https://learn.microsoft.com/en-us/windows/win32/shell/how-to-employ-the-verb-selection-model "How to Employ the Verb Selection Model | Microsoft Learn"
