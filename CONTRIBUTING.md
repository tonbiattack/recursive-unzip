# Contributing

Issue、改善提案、Pull Requestを歓迎します。やり取りとドキュメントは日本語または英語で記載してください。

## 開発環境

- Go 1.22以降
- Windows 11（Explorerメニューを確認する場合）

依存関係はGo標準ライブラリのみです。変更前後に、リポジトリのルートで次を実行してください。

```powershell
go test ./... -count=1
go build -trimpath -ldflags="-s -w" -o .\recursive-unzip.exe .\cmd\recursive-unzip
```

`recursive-unzip.exe` はGit管理対象外です。Explorerメニューを確認する場合は、ビルド後に `scripts\Install-ExplorerMenu.ps1` を実行し、テスト用ZIPで最上位ZIPが残り、展開済みの内部ZIPだけが削除されることを確認してください。

## Pull Request

- 目的と利用者に見える動作の変更を説明してください。
- パス処理、Zip Slip対策、シンボリックリンク対策を弱めないでください。
- ZIPを削除する変更では、失敗時に元データが残ることをテストしてください。
- 生成物、個人情報、認証情報をコミットしないでください。

脆弱性の報告は、通常のIssueではなく [SECURITY.md](SECURITY.md) を参照してください。
